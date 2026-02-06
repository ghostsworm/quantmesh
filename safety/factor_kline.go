package safety

import (
	"context"
	"quantmesh/config"
	"sync"
)

// KlineAnomalyFactor K 线异常因子
type KlineAnomalyFactor struct {
	cfg          *config.Config
	riskMonitor  *RiskMonitor
	weight       float64
	mu           sync.RWMutex
}

// NewKlineAnomalyFactor 创建 K 线异常因子
func NewKlineAnomalyFactor(cfg *config.Config, riskMonitor *RiskMonitor) *KlineAnomalyFactor {
	w := 0.15
	if cfg != nil && cfg.CompositeRisk.Factors.Kline.Weight > 0 {
		w = cfg.CompositeRisk.Factors.Kline.Weight
	}
	return &KlineAnomalyFactor{
		cfg:         cfg,
		riskMonitor: riskMonitor,
		weight:      w,
	}
}

// Name 实现 RiskFactor
func (f *KlineAnomalyFactor) Name() string { return "kline" }

// Weight 实现 RiskFactor
func (f *KlineAnomalyFactor) Weight() float64 {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.weight
}

// Evaluate 实现 RiskFactor
func (f *KlineAnomalyFactor) Evaluate(ctx context.Context) FactorResult {
	f.mu.RLock()
	rm := f.riskMonitor
	f.mu.RUnlock()

	if rm == nil {
		return FactorResult{
			Score:      0,
			Confidence: 0,
			Reason:     "K 线风控未接入",
		}
	}

	score, reason := rm.GetKlineRiskScore()
	if reason == "" {
		reason = "K 线正常"
	}
	return FactorResult{
		Score:      score,
		Confidence: 0.85,
		Reason:     reason,
		Details:    map[string]interface{}{"score": score},
	}
}
