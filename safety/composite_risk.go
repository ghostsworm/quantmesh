package safety

import (
	"context"
	"quantmesh/config"
	"quantmesh/logger"
	"quantmesh/monitor"
	"sync"
	"time"
)

// RiskFactor 风控因子接口
type RiskFactor interface {
	Name() string
	Evaluate(ctx context.Context) FactorResult
	Weight() float64
}

// FactorResult 因子评估结果
type FactorResult struct {
	Score      float64                `json:"score"`      // 0-100, 0=安全, 100=极度危险
	Confidence float64                `json:"confidence"` // 0-1, 数据可信度
	Reason     string                 `json:"reason"`     // 人类可读原因
	Details    map[string]interface{}  `json:"details"`    // 详细数据
}

// RiskLevel 风控级别
type RiskLevel string

const (
	RiskNormal         RiskLevel = "normal"          // < 25
	RiskCaution        RiskLevel = "caution"        // 25-45
	RiskReducePosition RiskLevel = "reduce_position" // 45-65
	RiskPauseBuying    RiskLevel = "pause_buying"    // 65-80
	RiskStopTrading    RiskLevel = "stop_trading"    // > 80
)

// CompositeRiskResult 复合风控结果
type CompositeRiskResult struct {
	CompositeScore float64             `json:"composite_score"`
	Level          RiskLevel           `json:"level"`
	BuyBias        float64             `json:"buy_bias"` // 0.0-1.2 买入系数
	FactorResults  map[string]FactorResult `json:"factor_results"`
	Timestamp      time.Time           `json:"timestamp"`
	Reasons        []string            `json:"reasons"`
}

// CompositeRiskController 复合风控引擎
type CompositeRiskController struct {
	cfg       *config.Config
	factors   []RiskFactor
	mu        sync.RWMutex
	lastResult CompositeRiskResult
	running   bool
	stopCh    chan struct{}
}

// NewCompositeRiskController 创建复合风控引擎
func NewCompositeRiskController(cfg *config.Config) *CompositeRiskController {
	if cfg != nil && cfg.CompositeRisk.Thresholds.Caution <= 0 {
		cfg.CompositeRisk.Thresholds.Caution = 25
		cfg.CompositeRisk.Thresholds.ReducePosition = 45
		cfg.CompositeRisk.Thresholds.PauseBuying = 65
		cfg.CompositeRisk.Thresholds.StopTrading = 80
	}
	return &CompositeRiskController{
		cfg:     cfg,
		factors: make([]RiskFactor, 0),
		lastResult: CompositeRiskResult{
			Level:         RiskNormal,
			BuyBias:       1.0,
			FactorResults: make(map[string]FactorResult),
			Reasons:       []string{},
		},
		stopCh: make(chan struct{}),
	}
}

// RegisterFactor 注册风控因子
func (c *CompositeRiskController) RegisterFactor(f RiskFactor) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.factors = append(c.factors, f)
}

// SetNewsMonitor 設置新聞監控器（由 main 在創建 NewsMonitor 後注入到新聞因子）
func (c *CompositeRiskController) SetNewsMonitor(nm *monitor.NewsMonitor) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, f := range c.factors {
		if nf, ok := f.(*NewsRiskFactor); ok {
			nf.SetNewsMonitor(nm)
			break
		}
	}
}

// SetMacroEventProvider 設置宏觀事件提供者（由 main 在創建 MacroEventFetcher 後注入到宏觀因子）
func (c *CompositeRiskController) SetMacroEventProvider(p MacroEventProvider) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, f := range c.factors {
		if mf, ok := f.(*MacroEventRiskFactor); ok {
			mf.SetProvider(p)
			break
		}
	}
}

// Evaluate 执行一次评估
func (c *CompositeRiskController) Evaluate(ctx context.Context) CompositeRiskResult {
	c.mu.RLock()
	factors := make([]RiskFactor, len(c.factors))
	copy(factors, c.factors)
	thresholds := config.CompositeRiskThresholds{Caution: 25, ReducePosition: 45, PauseBuying: 65, StopTrading: 80}
	if c.cfg != nil {
		thresholds = c.cfg.CompositeRisk.Thresholds
	}
	c.mu.RUnlock()

	if len(factors) == 0 {
		return c.lastResult
	}

	var totalWeight float64
	var weightedSum float64
	factorResults := make(map[string]FactorResult)
	var reasons []string

	for _, f := range factors {
		res := f.Evaluate(ctx)
		factorResults[f.Name()] = res
		w := f.Weight()
		if w <= 0 {
			continue
		}
		totalWeight += w
		weightedSum += w * res.Score * res.Confidence
		if res.Reason != "" {
			reasons = append(reasons, res.Reason)
		}
	}

	compositeScore := 0.0
	if totalWeight > 0 {
		compositeScore = weightedSum / totalWeight
	}
	if compositeScore > 100 {
		compositeScore = 100
	}
	if compositeScore < 0 {
		compositeScore = 0
	}

	level := scoreToLevel(compositeScore, thresholds)
	buyBias := levelToBuyBias(level)

	result := CompositeRiskResult{
		CompositeScore: compositeScore,
		Level:          level,
		BuyBias:        buyBias,
		FactorResults:  factorResults,
		Timestamp:      time.Now(),
		Reasons:        reasons,
	}

	c.mu.Lock()
	c.lastResult = result
	c.mu.Unlock()

	return result
}

func scoreToLevel(score float64, t config.CompositeRiskThresholds) RiskLevel {
	if t.StopTrading <= 0 {
		t.StopTrading = 80
	}
	if t.PauseBuying <= 0 {
		t.PauseBuying = 65
	}
	if t.ReducePosition <= 0 {
		t.ReducePosition = 45
	}
	if t.Caution <= 0 {
		t.Caution = 25
	}
	if score >= t.StopTrading {
		return RiskStopTrading
	}
	if score >= t.PauseBuying {
		return RiskPauseBuying
	}
	if score >= t.ReducePosition {
		return RiskReducePosition
	}
	if score >= t.Caution {
		return RiskCaution
	}
	return RiskNormal
}

func levelToBuyBias(level RiskLevel) float64 {
	switch level {
	case RiskStopTrading:
		return 0.0
	case RiskPauseBuying:
		return 0.0
	case RiskReducePosition:
		return 0.5
	case RiskCaution:
		return 0.8
	default:
		return 1.0
	}
}

// GetCurrentResult 获取最新评估结果
func (c *CompositeRiskController) GetCurrentResult() CompositeRiskResult {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.lastResult
}

// Start 启动定时评估
func (c *CompositeRiskController) Start(ctx context.Context) {
	if c.cfg == nil || !c.cfg.CompositeRisk.Enabled {
		logger.Info("⚠️ 复合风控未启用")
		return
	}
	c.mu.Lock()
	if c.running {
		c.mu.Unlock()
		return
	}
	c.running = true
	interval := time.Duration(c.cfg.CompositeRisk.EvaluateInterval) * time.Second
	if interval <= 0 {
		interval = 30 * time.Second
	}
	c.mu.Unlock()

	logger.Info("🛡️ 复合风控引擎已启动 (评估间隔: %v)", interval)

	// 立即评估一次
	c.Evaluate(ctx)

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				c.mu.Lock()
				c.running = false
				c.mu.Unlock()
				return
			case <-c.stopCh:
				return
			case <-ticker.C:
				c.Evaluate(ctx)
			}
		}
	}()
}

// Stop 停止评估
func (c *CompositeRiskController) Stop() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.running {
		return
	}
	c.running = false
	close(c.stopCh)
}
