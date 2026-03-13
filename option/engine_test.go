package option

import (
	"testing"
	"time"
)

func TestEngine_ComputeCoverage(t *testing.T) {
	cfg := OptionHedgeConfig{
		Enabled:             true,
		TargetCoverageRatio: 0.25,
		MinCoverageRatio:    0.15,
		DTEWarningDays:      7,
	}
	eng := NewEngine(cfg)

	positions := []OptionHedgePosition{
		{
			Right:     "PUT",
			Strike:    90000,
			Qty:       0.1,
			Delta:     -0.3,
			Premium:   500,
			Expiry:    time.Now().Add(14 * 24 * time.Hour),
		},
	}

	cov := eng.ComputeCoverage("bot1", 100000, 1.0, positions)
	if cov == nil {
		t.Fatal("expected non-nil coverage")
	}
	if cov.GridNotional != 100000 {
		t.Errorf("grid notional: want 100000, got %f", cov.GridNotional)
	}
	// Option notional = 90000 * 0.1 = 9000, nominal = 9000/100000 = 0.09
	if cov.NominalCoverage < 0.08 || cov.NominalCoverage > 0.1 {
		t.Errorf("nominal coverage: want ~0.09, got %f", cov.NominalCoverage)
	}
	if cov.MinDTE < 13 || cov.MinDTE > 15 {
		t.Errorf("min DTE: want ~14, got %d", cov.MinDTE)
	}
}

func TestEngine_ComputeCoverage_EmptyPositions(t *testing.T) {
	eng := NewEngine(OptionHedgeConfig{Enabled: true, MinCoverageRatio: 0.15})
	cov := eng.ComputeCoverage("bot1", 50000, 0.5, nil)
	if cov == nil {
		t.Fatal("expected non-nil coverage")
	}
	if cov.NominalCoverage != 0 {
		t.Errorf("nominal coverage: want 0, got %f", cov.NominalCoverage)
	}
	if !cov.BelowMinCoverage {
		t.Error("expected below min coverage when no positions")
	}
}

func TestEngine_SuggestRolls(t *testing.T) {
	eng := NewEngine(OptionHedgeConfig{TargetCoverageRatio: 0.25})
	snap := &CoverageSnapshot{MinDTE: 5, BotID: "b1"}
	suggestions := eng.SuggestRolls(snap, 95000)
	if len(suggestions) != 3 {
		t.Errorf("want 3 suggestions, got %d", len(suggestions))
	}
	if suggestions[0].Rank != 1 || suggestions[0].Label != "conservative" {
		t.Errorf("first suggestion: want rank=1 conservative, got rank=%d label=%s", suggestions[0].Rank, suggestions[0].Label)
	}
}

func TestEngine_SuggestRolls_NilSnapshot(t *testing.T) {
	eng := NewEngine(OptionHedgeConfig{})
	suggestions := eng.SuggestRolls(nil, 100000)
	if suggestions != nil {
		t.Errorf("want nil for nil snapshot, got %v", suggestions)
	}
}
