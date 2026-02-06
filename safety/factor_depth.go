package safety

import (
	"context"
	"quantmesh/config"
	"sync"
)

// DepthRiskFactor 市场深度因子
type DepthRiskFactor struct {
	cfg           *config.Config
	depthMonitor  *DepthMonitor
	symbol        string
	weight        float64
	mu            sync.RWMutex
}

// NewDepthRiskFactor 创建市场深度因子
func NewDepthRiskFactor(cfg *config.Config, depthMonitor *DepthMonitor, symbol string) *DepthRiskFactor {
	w := 0.10
	if cfg != nil && cfg.CompositeRisk.Factors.Depth.Weight > 0 {
		w = cfg.CompositeRisk.Factors.Depth.Weight
	}
	return &DepthRiskFactor{
		cfg:          cfg,
		depthMonitor: depthMonitor,
		symbol:       symbol,
		weight:       w,
	}
}

// Name 实现 RiskFactor
func (f *DepthRiskFactor) Name() string { return "depth" }

// Weight 实现 RiskFactor
func (f *DepthRiskFactor) Weight() float64 {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.weight
}

// Evaluate 实现 RiskFactor
func (f *DepthRiskFactor) Evaluate(ctx context.Context) FactorResult {
	f.mu.RLock()
	dm := f.depthMonitor
	symbol := f.symbol
	f.mu.RUnlock()

	if dm == nil {
		return FactorResult{
			Score:      0,
			Confidence: 0,
			Reason:     "深度监控未接入",
		}
	}

	if symbol == "" {
		symbols := dm.getMonitorSymbols()
		if len(symbols) > 0 {
			symbol = symbols[0]
		}
	}
	if symbol == "" {
		return FactorResult{
			Score:      0,
			Confidence: 0,
			Reason:     "无监控交易对",
		}
	}

	score, reason := dm.GetDepthRiskScore(symbol)
	return FactorResult{
		Score:      score,
		Confidence: 0.85,
		Reason:     reason,
		Details: map[string]interface{}{
			"symbol": symbol,
		},
	}
}
