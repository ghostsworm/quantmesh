package option

import (
	"math"
	"time"
)

// Engine 覆盖率计算与展期建议引擎
type Engine struct {
	cfg OptionHedgeConfig
}

// NewEngine 创建引擎
func NewEngine(cfg OptionHedgeConfig) *Engine {
	if cfg.TargetCoverageRatio <= 0 {
		cfg.TargetCoverageRatio = 0.25
	}
	if cfg.MinCoverageRatio <= 0 {
		cfg.MinCoverageRatio = 0.15
	}
	if cfg.DTEWarningDays <= 0 {
		cfg.DTEWarningDays = 7
	}
	return &Engine{cfg: cfg}
}

// ComputeCoverage 计算覆盖率快照
// hedgeType: PUT（做多网格）或 CALL（做空网格），用于过滤仓位并写入快照
// Put 多头：delta 为负，对冲做多网格；Call 多头：delta 为正，对冲做空网格；均用 |delta|*qty 计入对冲量
func (e *Engine) ComputeCoverage(botID string, gridNotional, gridPositionQty float64, positions []OptionHedgePosition, hedgeType string) *CoverageSnapshot {
	now := time.Now()
	if hedgeType == "" {
		hedgeType = "PUT"
	}
	snap := &CoverageSnapshot{
		BotID:           botID,
		HedgeType:       hedgeType,
		GridNotional:    gridNotional,
		GridPositionQty: gridPositionQty,
		SnapshotAt:      now,
	}

	var optionNotional, optionDelta float64
	minDTE := 999
	for _, p := range positions {
		if p.Right != hedgeType || p.Qty <= 0 {
			continue
		}
		notional := p.Strike * p.Qty
		optionNotional += notional
		optionDelta += math.Abs(p.Delta) * p.Qty
		snap.TotalPremium += p.Premium
		dte := int(time.Until(p.Expiry).Hours() / 24)
		if dte >= 0 && dte < minDTE {
			minDTE = dte
		}
	}
	snap.OptionNotional = optionNotional
	snap.OptionDeltaHedge = optionDelta
	snap.MinDTE = minDTE
	if minDTE == 999 {
		snap.MinDTE = -1
	}

	if gridNotional > 0 {
		snap.NominalCoverage = optionNotional / gridNotional
	}
	gridQtyAbs := math.Abs(gridPositionQty)
	if gridQtyAbs > 0 && optionDelta > 0 {
		snap.DeltaCoverage = math.Min(1, optionDelta/gridQtyAbs)
	}

	snap.BelowMinCoverage = snap.NominalCoverage < e.cfg.MinCoverageRatio
	snap.DTEWarning = snap.MinDTE >= 0 && snap.MinDTE < e.cfg.DTEWarningDays

	return snap
}

// SuggestRolls 生成展期建议（2-3 档）
// hedgeType PUT：做多网格，推荐 OTM Put（行权价低于现价）；CALL：做空网格，推荐 OTM Call（行权价高于现价）
func (e *Engine) SuggestRolls(snap *CoverageSnapshot, currentPrice float64) []RollSuggestion {
	if snap == nil || snap.MinDTE < 0 {
		return nil
	}
	var out []RollSuggestion
	isCall := snap.HedgeType == "CALL"
	if isCall {
		// 做空网格 + Call：行权价高于现价（OTM Call）
		out = append(out, RollSuggestion{
			Rank:             1,
			Label:            "conservative",
			Strike:           currentPrice * 1.05,
			DTE:              14,
			ExpectedCoverage: e.cfg.TargetCoverageRatio,
			RiskIfSkip:       "protection_gap_if_expiry",
		})
		out = append(out, RollSuggestion{
			Rank:             2,
			Label:            "neutral",
			Strike:           currentPrice * 1.08,
			DTE:              21,
			ExpectedCoverage: e.cfg.TargetCoverageRatio,
			RiskIfSkip:       "reduced_protection_window",
		})
		out = append(out, RollSuggestion{
			Rank:             3,
			Label:            "aggressive",
			Strike:           currentPrice * 1.10,
			DTE:              30,
			ExpectedCoverage: e.cfg.TargetCoverageRatio,
			RiskIfSkip:       "higher_premium_cost",
		})
	} else {
		// 做多网格 + Put：行权价低于现价（OTM Put）
		out = append(out, RollSuggestion{
			Rank:             1,
			Label:            "conservative",
			Strike:           currentPrice * 0.9,
			DTE:              14,
			ExpectedCoverage: e.cfg.TargetCoverageRatio,
			RiskIfSkip:       "protection_gap_if_expiry",
		})
		out = append(out, RollSuggestion{
			Rank:             2,
			Label:            "neutral",
			Strike:           currentPrice * 0.92,
			DTE:              21,
			ExpectedCoverage: e.cfg.TargetCoverageRatio,
			RiskIfSkip:       "reduced_protection_window",
		})
		out = append(out, RollSuggestion{
			Rank:             3,
			Label:            "aggressive",
			Strike:           currentPrice * 0.95,
			DTE:              30,
			ExpectedCoverage: e.cfg.TargetCoverageRatio,
			RiskIfSkip:       "higher_premium_cost",
		})
	}
	return out
}
