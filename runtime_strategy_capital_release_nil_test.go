package main

import (
	"testing"

	"quantmesh/strategy"
)

func TestRuntimeStrategyCapitalReleaseRejectsNilRegisteredStrategy(t *testing.T) {
	cases := map[string]strategy.Strategy{
		"nil":           nil,
		"grid":          (*strategy.GridStrategy)(nil),
		"dca":           (*strategy.DCAEnhancedStrategy)(nil),
		"martingale":    (*strategy.MartingaleStrategy)(nil),
		"trend":         (*strategy.TrendFollowingStrategy)(nil),
		"mean":          (*strategy.MeanReversionStrategy)(nil),
		"momentum":      (*strategy.MomentumStrategy)(nil),
		"spot_long":     (*strategy.SpotLongStrategy)(nil),
		"spot_short":    (*strategy.SpotShortStrategy)(nil),
		"futures_long":  (*strategy.FuturesLongStrategy)(nil),
		"futures_short": (*strategy.FuturesShortStrategy)(nil),
		"combo":         (*strategy.ComboStrategy)(nil),
	}
	for name, current := range cases {
		t.Run(name, func(t *testing.T) {
			rt, venue, _ := capitalReleaseRuntimeFixture(t)
			rt.StrategyManager.RegisterStrategy("broken", current, 1, 0)
			if err := verifyStandardSpotBotInventoryFlatContext(t.Context(), rt.SuperPositionManager, rt.StrategyManager, rt.capitalReleaseScope.Symbol); err == nil {
				t.Fatal("stop inventory check accepted missing strategy")
			}
			amounts, err := releaseRuntimeStrategyCapital(t.Context(), []*SymbolRuntime{rt}, "dca")
			if err == nil || amounts["dca"] != 0 || rt.StrategyManager.GetCapitalAllocator().GetUsed("dca") != 200 || venue.lastReadSymbol != "" {
				t.Fatalf("missing strategy did not preserve capital: %v %v", amounts, err)
			}
			// A corrected registration must recover through the same real path,
			// proving the failed check left no submission barrier behind.
			rt.StrategyManager.RegisterStrategy("broken", &capitalReleaseRuntimeStrategy{}, 1, 0)
			amounts, err = releaseRuntimeStrategyCapital(t.Context(), []*SymbolRuntime{rt}, "dca")
			if err != nil || amounts["dca"] != 200 {
				t.Fatalf("corrected strategy could not recover: %v %v", amounts, err)
			}
		})
	}
}
