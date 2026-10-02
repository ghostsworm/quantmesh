package main

import (
	"testing"

	"quantmesh/config"
	"quantmesh/position"
	"quantmesh/strategy"
)

func TestRuntimeStrategyCapitalReleaseRejectsUnverifiedGridWrapper(t *testing.T) {
	rt, venue, _ := capitalReleaseRuntimeFixture(t)
	cfg := &config.Config{}
	cfg.Trading.BotID, cfg.Trading.Symbol = rt.capitalReleaseScope.Bot, rt.capitalReleaseScope.Symbol
	// A wrapper bound to its own not-yet-verified manager must not borrow the
	// runtime's separate empty proof just because its display snapshot is empty.
	grid := &position.SuperPositionManager{}
	current := strategy.NewGridStrategy("grid", cfg, nil, nil, grid)
	rt.StrategyManager.RegisterStrategy("grid", current, 1, 0)
	released, err := releaseRuntimeStrategyCapital(t.Context(), []*SymbolRuntime{rt}, "dca")
	if err == nil || released["dca"] != 0 || rt.StrategyManager.GetCapitalAllocator().GetUsed("dca") != 200 || venue.lastReadSymbol != "" {
		t.Fatalf("unverified wrapper authorized capital release: %v %v", released, err)
	}
	grid.MarkGridRuntimeVenueFlatVerified()
	released, err = releaseRuntimeStrategyCapital(t.Context(), []*SymbolRuntime{rt}, "dca")
	if err == nil || released["dca"] != 0 || rt.StrategyManager.GetCapitalAllocator().GetUsed("dca") != 200 {
		t.Fatalf("another empty manager borrowed runtime owner proof: %v %v", released, err)
	}
	rt.StrategyManager.RegisterStrategy("grid", strategy.NewGridStrategy("grid", cfg, nil, nil, rt.SuperPositionManager), 1, 0)
	released, err = releaseRuntimeStrategyCapital(t.Context(), []*SymbolRuntime{rt}, "dca")
	if err != nil || released["dca"] != 200 {
		t.Fatalf("correctly bound verified wrapper could not recover: %v %v", released, err)
	}
}
