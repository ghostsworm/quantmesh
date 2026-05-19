package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"quantmesh/config"
	"quantmesh/logger"
	"quantmesh/polymarket"
)

// PolymarketSignalAnalysis 與 Web 前端 AIPolymarketSignal 對齊。
type PolymarketSignalAnalysis struct {
	Signal     string       `json:"signal"`
	Strength   float64      `json:"strength"`
	Confidence float64      `json:"confidence"`
	Reasoning  string       `json:"reasoning"`
	Signals    []SignalItem `json:"signals,omitempty"`
}

// SignalItem 單個市場的解讀。
type SignalItem struct {
	Question    string  `json:"question"`
	Signal      string  `json:"signal"`
	Probability float64 `json:"probability"`
	Strength    float64 `json:"strength"`
	Confidence  float64 `json:"confidence"`
	Reasoning   string  `json:"reasoning"`
	Relevance   string  `json:"relevance"`
}

// PolymarketSignalAnalyzer 拉取 Gamma 預測市場並用 LLM 綜合解讀「是否可能發生宏觀/加密大事」。
type PolymarketSignalAnalyzer struct {
	mu       sync.RWMutex
	cfg      *config.Config
	client   AIClient
	http     *http.Client
	last     *PolymarketSignalAnalysis
	lastTime time.Time
	running  atomic.Bool
}

// NewPolymarketSignalAnalyzer 創建分析器。
func NewPolymarketSignalAnalyzer(cfg *config.Config, client AIClient) *PolymarketSignalAnalyzer {
	if cfg == nil {
		return nil
	}
	return &PolymarketSignalAnalyzer{
		cfg:    cfg,
		client: client,
		http:   &http.Client{Timeout: 20 * time.Second},
	}
}

func (a *PolymarketSignalAnalyzer) gammaBaseURL() string {
	g := strings.TrimSpace(a.cfg.MacroEvent.GammaAPIURL)
	if g == "" {
		g = strings.TrimSpace(a.cfg.AI.Modules.PolymarketSignal.APIURL)
	}
	if g == "" {
		g = "https://gamma-api.polymarket.com"
	}
	return strings.TrimSuffix(g, "/")
}

func (a *PolymarketSignalAnalyzer) keywords() []string {
	return a.cfg.AI.Modules.PolymarketSignal.Markets.Keywords
}

// GetLastAnalysis 返回最近一次分析結果。
func (a *PolymarketSignalAnalyzer) GetLastAnalysis() interface{} {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.last == nil {
		return nil
	}
	cp := *a.last
	if a.last.Signals != nil {
		cp.Signals = append([]SignalItem(nil), a.last.Signals...)
	}
	return &cp
}

// GetLastAnalysisTime 獲取最近一次分析時間。
func (a *PolymarketSignalAnalyzer) GetLastAnalysisTime() time.Time {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.lastTime
}

// TriggerAnalysis 拉取市場並調用 LLM 生成綜合信號。
func (a *PolymarketSignalAnalyzer) TriggerAnalysis() error {
	if a == nil || a.client == nil || a.cfg == nil {
		return fmt.Errorf("polymarket analyzer 未初始化")
	}
	if !a.running.CompareAndSwap(false, true) {
		return fmt.Errorf("polymarket analyzer 已在运行")
	}
	defer a.running.Store(false)

	fetchCtx, fetchCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer fetchCancel()
	raw, err := polymarket.FetchActiveMarketsContext(fetchCtx, a.gammaBaseURL(), a.keywords(), a.http, 50)
	if err != nil {
		return err
	}
	filtered := a.filterMarkets(raw)
	if len(filtered) == 0 {
		a.mu.Lock()
		a.last = &PolymarketSignalAnalysis{
			Signal:     "neutral",
			Strength:   0,
			Confidence: 0.1,
			Reasoning:  "當前篩選條件下沒有可用的 Polymarket 市場數據，請放寬關鍵詞或流動性閾值。",
		}
		a.lastTime = time.Now()
		a.mu.Unlock()
		return nil
	}
	// 取前 25 條，按成交量排序
	sort.Slice(filtered, func(i, j int) bool { return filtered[i].Volume > filtered[j].Volume })
	if len(filtered) > 25 {
		filtered = filtered[:25]
	}
	prompt := buildPolymarketPrompt(filtered)
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"signal":     map[string]interface{}{"type": "string", "description": "bullish | bearish | neutral"},
			"strength":   map[string]interface{}{"type": "number"},
			"confidence": map[string]interface{}{"type": "number"},
			"reasoning":  map[string]interface{}{"type": "string"},
			"signals": map[string]interface{}{
				"type": "array",
				"items": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"question":    map[string]interface{}{"type": "string"},
						"signal":      map[string]interface{}{"type": "string"},
						"probability": map[string]interface{}{"type": "number"},
						"strength":    map[string]interface{}{"type": "number"},
						"confidence":  map[string]interface{}{"type": "number"},
						"reasoning":   map[string]interface{}{"type": "string"},
						"relevance":   map[string]interface{}{"type": "string"},
					},
				},
			},
		},
	}
	out, err := a.client.GenerateContent(ctx, prompt, schema, false)
	if err != nil {
		return err
	}
	analysis, err := parsePolymarketJSON(out)
	if err != nil {
		logger.Warn("Polymarket 信號 JSON 解析失敗，使用原始摘要: %v", err)
		analysis = &PolymarketSignalAnalysis{
			Signal:     "neutral",
			Strength:   0.3,
			Confidence: 0.3,
			Reasoning:  truncateStr(out, 2000),
		}
	}
	normalizePolymarketAnalysis(analysis)
	a.mu.Lock()
	a.last = analysis
	a.lastTime = time.Now()
	a.mu.Unlock()
	return nil
}

func truncateStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func (a *PolymarketSignalAnalyzer) filterMarkets(in []polymarket.MarketInfo) []polymarket.MarketInfo {
	m := a.cfg.AI.Modules.PolymarketSignal.Markets
	minL := m.MinLiquidity
	minV := m.MinVolume24h
	minDay := m.MinDaysToExpiry
	maxDay := m.MaxDaysToExpiry
	now := time.Now()
	var out []polymarket.MarketInfo
	for _, x := range in {
		if minL > 0 && x.Liquidity < minL {
			continue
		}
		if minV > 0 && x.Volume < minV {
			continue
		}
		if !x.EndDate.IsZero() {
			days := x.EndDate.Sub(now).Hours() / 24
			if minDay > 0 && days < float64(minDay) {
				continue
			}
			if maxDay > 0 && days > float64(maxDay) {
				continue
			}
		}
		out = append(out, x)
	}
	return out
}

func buildPolymarketPrompt(markets []polymarket.MarketInfo) string {
	var b strings.Builder
	b.WriteString("你是加密與宏觀預測市場分析師。以下數據來自 Polymarket（預測市場），用於感知「市場是否正在為某類重大事件定價」。\n")
	b.WriteString("請根據問題、標題、Yes 概率、流動性與成交量，輸出 JSON：\n")
	b.WriteString("- signal: 對加密資產短期風險偏好的綜合判斷（bullish / bearish / neutral）\n")
	b.WriteString("- strength: 0~1\n- confidence: 0~1\n- reasoning: 簡短中文，說明為何可能出現「大事」或為何相對平靜\n")
	b.WriteString("- signals: 每個市場一條，signal 用 bullish/bearish/neutral 表示該市場對風險的含義；probability 必須優先使用市場給出的 Yes 概率，無價格時才填 0.5\n\n")
	for i, m := range markets {
		title := m.EventTitle
		if title == "" {
			title = "(event)"
		}
		yesProbability := "未知"
		if len(m.OutcomePrices) > 0 {
			yesProbability = fmt.Sprintf("%.4f", m.YesProbability)
		}
		outcomes := strings.Join(m.Outcomes, "/")
		if outcomes == "" {
			outcomes = "未知"
		}
		b.WriteString(fmt.Sprintf("%d. [%s] %s\n   结果:%s Yes概率:%s 流動性:%.0f 成交:%.0f 到期:%s\n",
			i+1, title, m.Question, outcomes, yesProbability, m.Liquidity, m.Volume, m.EndDate.Format(time.RFC3339)))
	}
	return b.String()
}

func parsePolymarketJSON(s string) (*PolymarketSignalAnalysis, error) {
	s = strings.TrimSpace(s)
	if i := strings.Index(s, "{"); i >= 0 {
		s = s[i:]
	}
	if j := strings.LastIndex(s, "}"); j >= 0 {
		s = s[:j+1]
	}
	var out PolymarketSignalAnalysis
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func normalizePolymarketAnalysis(a *PolymarketSignalAnalysis) {
	if a == nil {
		return
	}
	a.Signal = normalizePolymarketSignal(a.Signal)
	a.Strength = clamp01(a.Strength)
	a.Confidence = clamp01(a.Confidence)
	for i := range a.Signals {
		a.Signals[i].Signal = normalizePolymarketSignal(a.Signals[i].Signal)
		a.Signals[i].Probability = clamp01(a.Signals[i].Probability)
		a.Signals[i].Strength = clamp01(a.Signals[i].Strength)
		a.Signals[i].Confidence = clamp01(a.Signals[i].Confidence)
	}
}

func normalizePolymarketSignal(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "bullish", "bearish", "neutral":
		return strings.ToLower(strings.TrimSpace(s))
	default:
		return "neutral"
	}
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}
