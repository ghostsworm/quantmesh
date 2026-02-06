package safety

import (
	"context"
	"quantmesh/config"
	"sync"
)

// FundingRateFactor 资金费率因子
type FundingRateFactor struct {
	cfg                        *config.Config
	fundingMonitor             *FundingRateMonitor
	weight                      float64
	consecutiveNegativePeriods  int
	mu                          sync.RWMutex
}

// NewFundingRateFactor 创建资金费率因子
func NewFundingRateFactor(cfg *config.Config, fundingMonitor *FundingRateMonitor) *FundingRateFactor {
	w := 0.20
	consecutiveN := 3
	if cfg != nil {
		if cfg.CompositeRisk.Factors.FundingRate.Weight > 0 {
			w = cfg.CompositeRisk.Factors.FundingRate.Weight
		}
		if cfg.CompositeRisk.Factors.FundingRate.ConsecutiveNegativePeriods > 0 {
			consecutiveN = cfg.CompositeRisk.Factors.FundingRate.ConsecutiveNegativePeriods
		}
	}
	return &FundingRateFactor{
		cfg:                       cfg,
		fundingMonitor:            fundingMonitor,
		weight:                    w,
		consecutiveNegativePeriods: consecutiveN,
	}
}

// Name 实现 RiskFactor
func (f *FundingRateFactor) Name() string { return "funding_rate" }

// Weight 实现 RiskFactor
func (f *FundingRateFactor) Weight() float64 {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.weight
}

// Evaluate 实现 RiskFactor
func (f *FundingRateFactor) Evaluate(ctx context.Context) FactorResult {
	f.mu.RLock()
	fm := f.fundingMonitor
	consecutiveN := f.consecutiveNegativePeriods
	f.mu.RUnlock()

	if fm == nil {
		return FactorResult{
			Score:      0,
			Confidence: 0,
			Reason:     "资金费率监控未接入",
		}
	}

	rate := fm.GetCurrentRate()
	history := fm.GetRateHistory()

	var score float64
	var reason string

	// 极端负费率 (< -0.1%)：直接高风险
	if rate <= -0.001 {
		score = 70
		reason = "极端负费率"
	} else {
		// 连续 N 期负费率：递增风险
		negativeCount := 0
		for i := len(history) - 1; i >= 0; i-- {
			if history[i].Rate < 0 {
				negativeCount++
			} else {
				break
			}
		}
		if negativeCount >= consecutiveN {
			// 每期 +10，上限 80
			score = 30 + float64(negativeCount)*10
			if score > 80 {
				score = 80
			}
			reason = "连续负费率"
		} else if rate < 0 {
			// 单次负费率：适度风险
			score = 30
			reason = "单次负费率"
		} else if rate > 0.0015 {
			// 正费率过高 (> 0.15%)：多头过度拥挤，高风险
			score = 70
			reason = "正费率过高"
		} else if rate > 0.001 {
			// 0.1% ~ 0.15%：中等风险
			score = 50
			reason = "正费率偏高"
		} else {
			score = 0
			reason = "费率正常"
		}
	}

	if score > 100 {
		score = 100
	}
	if score < 0 {
		score = 0
	}

	return FactorResult{
		Score:      score,
		Confidence: 0.9,
		Reason:     reason,
		Details: map[string]interface{}{
			"current_rate": rate,
			"rate_pct":     rate * 100,
		},
	}
}
