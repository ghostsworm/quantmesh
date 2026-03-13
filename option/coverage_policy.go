package option

import "quantmesh/config"

// CoveragePolicyResult 覆盖率风控策略输出
type CoveragePolicyResult struct {
	ShouldPauseOpening   bool
	PauseReason          string
	ReduceMaxLayers      int     // 0=不调整
	IncreaseOrderDist    float64 // 0=不调整
	SuggestedMaxLayers   int
	SuggestedOrderDist   float64
}

// ApplyCoveragePolicy 根据覆盖率快照应用风控策略
func ApplyCoveragePolicy(cov *CoverageSnapshot, cfg OptionHedgeConfig, baseRisk *config.BotRiskControl) CoveragePolicyResult {
	var r CoveragePolicyResult
	if cov == nil || !cfg.Enabled {
		return r
	}
	if cov.BelowMinCoverage {
		r.ShouldPauseOpening = true
		r.PauseReason = "option_coverage_below_min"
		if baseRisk != nil && baseRisk.MaxPositionLayers > 0 {
			r.ReduceMaxLayers = baseRisk.MaxPositionLayers / 2
			if r.ReduceMaxLayers < 1 {
				r.ReduceMaxLayers = 1
			}
			r.SuggestedMaxLayers = r.ReduceMaxLayers
		}
		if baseRisk != nil && baseRisk.OpenOrderDistance > 0 {
			r.IncreaseOrderDist = baseRisk.OpenOrderDistance * 1.5
			r.SuggestedOrderDist = r.IncreaseOrderDist
		}
	}
	if cov.DTEWarning && cov.MinDTE < 3 {
		r.ShouldPauseOpening = true
		if r.PauseReason == "" {
			r.PauseReason = "option_expiry_imminent"
		}
	}
	return r
}
