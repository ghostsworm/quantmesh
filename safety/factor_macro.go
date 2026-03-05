package safety

import (
	"context"
	"quantmesh/config"
	"quantmesh/macro"
	"sync"
)

// MacroEventProvider 宏观事件数据提供者接口
type MacroEventProvider interface {
	GetImpactSummary() macro.MacroImpactSummary
}

// MacroEventRiskFactor 宏观事件风控因子
type MacroEventRiskFactor struct {
	cfg     *config.Config
	provider MacroEventProvider
	weight  float64
	mu      sync.RWMutex
}

// NewMacroEventRiskFactor 创建宏观事件因子
func NewMacroEventRiskFactor(cfg *config.Config, provider MacroEventProvider) *MacroEventRiskFactor {
	w := 0.20
	if cfg != nil && cfg.CompositeRisk.Factors.Macro.Weight > 0 {
		w = cfg.CompositeRisk.Factors.Macro.Weight
	}
	return &MacroEventRiskFactor{
		cfg:      cfg,
		provider: provider,
		weight:   w,
	}
}

// SetProvider 设置数据提供者（可由 main 在创建 MacroEventFetcher 后注入）
func (f *MacroEventRiskFactor) SetProvider(p MacroEventProvider) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.provider = p
}

// Name 实现 RiskFactor
func (f *MacroEventRiskFactor) Name() string { return "macro" }

// Weight 实现 RiskFactor
func (f *MacroEventRiskFactor) Weight() float64 {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.weight
}

// Evaluate 实现 RiskFactor
func (f *MacroEventRiskFactor) Evaluate(ctx context.Context) FactorResult {
	f.mu.RLock()
	provider := f.provider
	f.mu.RUnlock()

	if provider == nil {
		return FactorResult{
			Score:      0,
			Confidence: 0,
			Reason:     "宏观事件未接入",
		}
	}

	summary := provider.GetImpactSummary()
	if summary.EventCount == 0 {
		return FactorResult{
			Score:      0,
			Confidence: 0.3,
			Reason:     "暂无宏观事件数据",
		}
	}

	score := summary.CompositeRiskScore
	if score > 100 {
		score = 100
	}
	if score < 0 {
		score = 0
	}

	reason := "宏观事件正常"
	if summary.HighImpactCount > 0 {
		reason = "存在高影响宏观事件"
	}
	if summary.CompositeRiskScore >= 70 {
		reason = "宏观风险较高，建议谨慎"
	}
	if summary.CompositeRiskScore >= 50 {
		reason = "宏观事件需关注"
	}

	confidence := 0.8
	if summary.EventCount < 3 {
		confidence = 0.5
	}

	return FactorResult{
		Score:      score,
		Confidence: confidence,
		Reason:     reason,
		Details: map[string]interface{}{
			"event_count":          summary.EventCount,
			"high_impact_count":    summary.HighImpactCount,
			"composite_risk_score": summary.CompositeRiskScore,
			"last_fetched":         summary.LastFetched,
		},
	}
}
