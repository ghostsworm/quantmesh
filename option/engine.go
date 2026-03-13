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
func (e *Engine) ComputeCoverage(botID string, gridNotional, gridPositionQty float64, positions []OptionHedgePosition) *CoverageSnapshot {
	now := time.Now()
	snap := &CoverageSnapshot{
		BotID:           botID,
		GridNotional:    gridNotional,
		GridPositionQty: gridPositionQty,
		SnapshotAt:      now,
	}

	var optionNotional, optionDelta float64
	minDTE := 999
	for _, p := range positions {
		if p.Right != "PUT" || p.Qty <= 0 {
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
	if gridPositionQty > 0 && optionDelta > 0 {
		snap.DeltaCoverage = math.Min(1, optionDelta/gridPositionQty)
	}

	snap.BelowMinCoverage = snap.NominalCoverage < e.cfg.MinCoverageRatio
	snap.DTEWarning = snap.MinDTE >= 0 && snap.MinDTE < e.cfg.DTEWarningDays

	return snap
}

// SuggestRolls 生成展期建议（2-3 档）
func (e *Engine) SuggestRolls(snap *CoverageSnapshot, currentPrice float64) []RollSuggestion {
	if snap == nil || snap.MinDTE < 0 {
		return nil
	}
	var out []RollSuggestion
	// 保守：建议立即展期
	out = append(out, RollSuggestion{
		Rank:             1,
		Label:            "conservative",
		Strike:           currentPrice * 0.9,
		DTE:              14,
		ExpectedCoverage: e.cfg.TargetCoverageRatio,
		RiskIfSkip:       "protection_gap_if_expiry",
	})
	// 中性：7 天后到期
	out = append(out, RollSuggestion{
		Rank:             2,
		Label:            "neutral",
		Strike:           currentPrice * 0.92,
		DTE:              21,
		ExpectedCoverage: e.cfg.TargetCoverageRatio,
		RiskIfSkip:       "reduced_protection_window",
	})
	// 进攻：更远月
	out = append(out, RollSuggestion{
		Rank:             3,
		Label:            "aggressive",
		Strike:           currentPrice * 0.95,
		DTE:              30,
		ExpectedCoverage: e.cfg.TargetCoverageRatio,
		RiskIfSkip:       "higher_premium_cost",
	})
	return out
}
