package strategy

import (
	"testing"

	"quantmesh/config"
)

func TestNewFundingCarryStrategy_ConfigParams(t *testing.T) {
	stratCfg := map[string]interface{}{
		"min_funding_rate":        0.001,
		"exit_funding_rate":       0.0005,
		"max_basis_pct":           0.3,
		"rebalance_interval_sec":  120.0,
	}
	s := NewFundingCarryStrategy("fc-test", nil, config.SymbolConfig{Symbol: "BTCUSDT"}, nil, nil, stratCfg)
	if s.minFundingRate != 0.001 {
		t.Errorf("minFundingRate = %v, want 0.001", s.minFundingRate)
	}
	if s.exitFundingRate != 0.0005 {
		t.Errorf("exitFundingRate = %v, want 0.0005", s.exitFundingRate)
	}
	if s.maxBasisPct != 0.3 {
		t.Errorf("maxBasisPct = %v, want 0.3", s.maxBasisPct)
	}
	if s.tickInterval.Seconds() != 120 {
		t.Errorf("tickInterval = %v, want 120s", s.tickInterval)
	}
}
