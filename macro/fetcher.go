package macro

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"quantmesh/config"
	"quantmesh/logger"
)

const (
	defaultGammaAPIURL   = "https://gamma-api.polymarket.com"
	defaultFetchInterval = 300 // 秒
	maxGammaBodyBytes    = 8 << 20
	maxGammaErrorBytes   = 4096
)

// MacroEventFetcher 从 Polymarket Gamma API 拉取宏观事件
type MacroEventFetcher struct {
	cfg        *config.Config
	apiURL     string
	interval   time.Duration
	classifier *EventImpactClassifier

	mu          sync.RWMutex
	events      []MacroEvent
	lastFetched time.Time
	history     map[string]float64 // eventID -> 上次概率，用于计算 delta
	running     bool
	stopCh      chan struct{}
	httpClient  *http.Client
}

// NewMacroEventFetcher 创建拉取器
func NewMacroEventFetcher(cfg *config.Config, classifier *EventImpactClassifier) *MacroEventFetcher {
	apiURL := defaultGammaAPIURL
	interval := time.Duration(defaultFetchInterval) * time.Second
	if cfg != nil && cfg.MacroEvent.Enabled {
		if cfg.MacroEvent.GammaAPIURL != "" {
			apiURL = strings.TrimSuffix(cfg.MacroEvent.GammaAPIURL, "/")
		}
		if cfg.MacroEvent.FetchInterval > 0 {
			interval = time.Duration(cfg.MacroEvent.FetchInterval) * time.Second
		}
	}
	return &MacroEventFetcher{
		cfg:        cfg,
		apiURL:     apiURL,
		interval:   interval,
		classifier: classifier,
		events:     make([]MacroEvent, 0),
		history:    make(map[string]float64),
		stopCh:     make(chan struct{}),
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

// Start 启动定时拉取
func (f *MacroEventFetcher) Start(ctx context.Context) {
	if f.cfg == nil || !f.cfg.MacroEvent.Enabled {
		logger.Info("📊 宏观事件拉取未启用")
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	f.mu.Lock()
	if f.running {
		f.mu.Unlock()
		return
	}
	if f.interval <= 0 {
		f.interval = time.Duration(defaultFetchInterval) * time.Second
	}
	f.stopCh = make(chan struct{})
	stopCh := f.stopCh
	interval := f.interval
	f.running = true
	f.mu.Unlock()

	// 立即拉取一次
	if err := f.Fetch(ctx); err != nil {
		logger.Warn("⚠️ 宏观事件首次拉取失败: %v", err)
	}

	go func() {
		defer func() {
			f.mu.Lock()
			if f.stopCh == stopCh {
				f.running = false
				f.stopCh = make(chan struct{})
			}
			f.mu.Unlock()
		}()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-stopCh:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := f.Fetch(ctx); err != nil {
					logger.Warn("⚠️ 宏观事件拉取失败: %v", err)
				}
			}
		}
	}()
	logger.Info("📊 宏观事件拉取已启动，间隔 %v", interval)
}

// Stop 停止拉取
func (f *MacroEventFetcher) Stop() {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.running {
		return
	}
	stopCh := f.stopCh
	f.running = false
	f.stopCh = make(chan struct{})
	close(stopCh)
}

// Fetch 执行一次拉取
func (f *MacroEventFetcher) Fetch(ctx context.Context) error {
	events, err := f.fetchFromGamma(ctx)
	if err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()

	// 计算概率变化
	for i := range events {
		e := &events[i]
		if prev, ok := f.history[e.ID]; ok {
			e.ProbabilityDelta = e.Probability - prev
		}
		f.history[e.ID] = e.Probability
	}
	f.events = events
	f.lastFetched = time.Now()
	return nil
}

// GetEvents 获取当前缓存的宏观事件列表
func (f *MacroEventFetcher) GetEvents() []MacroEvent {
	f.mu.RLock()
	defer f.mu.RUnlock()
	out := make([]MacroEvent, len(f.events))
	copy(out, f.events)
	return out
}

// GetImpactSummary 获取宏观影响汇总（供风控因子和 API 使用）
func (f *MacroEventFetcher) GetImpactSummary() MacroImpactSummary {
	f.mu.RLock()
	events := make([]MacroEvent, len(f.events))
	copy(events, f.events)
	lastFetched := f.lastFetched
	f.mu.RUnlock()

	var assessments []ImpactAssessment
	var compositeScore float64
	highCount := 0
	if f.classifier != nil {
		for _, e := range events {
			a := f.classifier.Assess(e)
			assessments = append(assessments, a)
			compositeScore += a.RiskScore * a.Weight
			if a.RiskScore >= 50 {
				highCount++
			}
		}
		if len(assessments) > 0 {
			compositeScore /= float64(len(assessments))
			if compositeScore > 100 {
				compositeScore = 100
			}
		}
	}
	return MacroImpactSummary{
		CompositeRiskScore: compositeScore,
		EventCount:         len(events),
		HighImpactCount:    highCount,
		Assessments:        assessments,
		LastFetched:        lastFetched,
	}
}

// GetLastFetched 获取上次拉取时间
func (f *MacroEventFetcher) GetLastFetched() time.Time {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.lastFetched
}

// fetchFromGamma 从 Gamma API 拉取并过滤
func (f *MacroEventFetcher) fetchFromGamma(ctx context.Context) ([]MacroEvent, error) {
	baseURL := f.apiURL
	minLiq := 50000.0
	minVol := 10000.0
	activeOnly := true
	if f.cfg != nil && f.cfg.MacroEvent.Enabled {
		if f.cfg.MacroEvent.Filters.MinLiquidity > 0 {
			minLiq = f.cfg.MacroEvent.Filters.MinLiquidity
		}
		if f.cfg.MacroEvent.Filters.MinVolume24h > 0 {
			minVol = f.cfg.MacroEvent.Filters.MinVolume24h
		}
		activeOnly = f.cfg.MacroEvent.Filters.ActiveOnly
	}

	// GET /events?active=true&closed=false&limit=100&order=volume24hr&ascending=false
	url := fmt.Sprintf("%s/events?limit=100&order=volume24hr&ascending=false", baseURL)
	if activeOnly {
		url += "&active=true&closed=false"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := f.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxGammaErrorBytes))
		return nil, fmt.Errorf("Gamma API %d: %s", resp.StatusCode, string(body))
	}
	var rawEvents []GammaEvent
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxGammaBodyBytes)).Decode(&rawEvents); err != nil {
		return nil, err
	}

	var out []MacroEvent
	for _, re := range rawEvents {
		liq := re.Liquidity
		if liq == 0 && len(re.Markets) > 0 {
			for _, m := range re.Markets {
				liq += m.LiquidityNum
			}
		}
		vol24 := re.Volume24hr
		if vol24 == 0 && len(re.Markets) > 0 {
			for _, m := range re.Markets {
				vol24 += m.Volume24hr
			}
		}
		if liq < minLiq && vol24 < minVol {
			continue
		}
		evt, ok := f.convertEvent(re)
		if !ok {
			continue
		}
		out = append(out, evt)
	}
	return out, nil
}

// convertEvent 将 GammaEvent 转为 MacroEvent，并做分类
func (f *MacroEventFetcher) convertEvent(re GammaEvent) (MacroEvent, bool) {
	prob := 0.0
	if len(re.Markets) > 0 {
		m := re.Markets[0]
		prices := parseOutcomePrices(m.OutcomePrices)
		if len(prices) > 0 {
			prob = prices[0] // YES 概率
		}
	}
	category, label := CategoryUnknown, ""
	if f.classifier != nil {
		category, label = f.classifier.Classify(re.Title + " " + re.Description)
	}
	if category == CategoryUnknown {
		return MacroEvent{}, false
	}
	liq := re.Liquidity
	vol := re.Volume
	vol24 := re.Volume24hr
	for _, m := range re.Markets {
		liq += m.LiquidityNum
		vol += m.VolumeNum
		vol24 += m.Volume24hr
	}
	return MacroEvent{
		ID:            re.ID,
		Title:         re.Title,
		Description:   re.Description,
		Category:      category,
		CategoryLabel: label,
		Probability:   prob,
		Volume:        vol,
		Volume24hr:    vol24,
		Liquidity:     liq,
		SourceURL:     "https://polymarket.com/event/" + re.Slug,
		EndDate:       re.EndDate,
		LastUpdated:   re.UpdatedAt,
		MarketCount:   len(re.Markets),
	}, true
}

func parseOutcomePrices(s string) []float64 {
	s = strings.TrimSpace(s)
	if s == "" || s == "[]" {
		return nil
	}
	var arr []string
	if err := json.Unmarshal([]byte(s), &arr); err != nil {
		return nil
	}
	out := make([]float64, 0, len(arr))
	for _, v := range arr {
		f, _ := strconv.ParseFloat(strings.TrimSpace(v), 64)
		out = append(out, f)
	}
	return out
}
