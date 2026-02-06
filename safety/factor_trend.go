package safety

import (
	"context"
	"quantmesh/config"
	"sync"
)

// TrendProvider 趨勢提供者接口（由 strategy.TrendDetector 實現，避免 safety 依賴 strategy）
type TrendProvider interface {
	GetCurrentTrend() string
}

// TrendRiskFactor 均线趋势因子
type TrendRiskFactor struct {
	cfg           *config.Config
	trendDetector TrendProvider
	weight        float64
	useRSI        bool
	useMACD       bool
	mu            sync.RWMutex
}

// NewTrendRiskFactor 创建均线趋势因子
func NewTrendRiskFactor(cfg *config.Config, trendDetector TrendProvider) *TrendRiskFactor {
	w := 0.25
	useRSI := false
	useMACD := false
	if cfg != nil {
		if cfg.CompositeRisk.Factors.Trend.Weight > 0 {
			w = cfg.CompositeRisk.Factors.Trend.Weight
		}
		useRSI = cfg.CompositeRisk.Factors.Trend.UseRSI
		useMACD = cfg.CompositeRisk.Factors.Trend.UseMACD
	}
	return &TrendRiskFactor{
		cfg:           cfg,
		trendDetector: trendDetector,
		weight:        w,
		useRSI:        useRSI,
		useMACD:       useMACD,
	}
}

// Name 实现 RiskFactor
func (f *TrendRiskFactor) Name() string { return "trend" }

// Weight 实现 RiskFactor
func (f *TrendRiskFactor) Weight() float64 {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.weight
}

// Evaluate 實現 RiskFactor
func (f *TrendRiskFactor) Evaluate(ctx context.Context) FactorResult {
	f.mu.RLock()
	td := f.trendDetector
	f.mu.RUnlock()

	if td == nil {
		return FactorResult{
			Score:      0,
			Confidence: 0,
			Reason:     "趋势检测未接入",
		}
	}

	trend := td.GetCurrentTrend()
	var score float64
	var reason string
	switch trend {
	case "down":
		score = 70
		reason = "均线趋势: 下跌"
	case "side":
		score = 40
		reason = "均线趋势: 震荡"
	case "up":
		score = 20
		reason = "均线趋势: 上涨"
	default:
		score = 40
		reason = "均线趋势: 未知"
	}

	details := map[string]interface{}{
		"trend": trend,
	}
	if f.useRSI {
		details["use_rsi"] = true
		// RSI 需 K 线数据，此处仅标记；可后续扩展
	}
	if f.useMACD {
		details["use_macd"] = true
		// MACD 需 K 线数据，此处仅标记；可后续扩展
	}

	return FactorResult{
		Score:      score,
		Confidence: 0.85,
		Reason:     reason,
		Details:    details,
	}
}
