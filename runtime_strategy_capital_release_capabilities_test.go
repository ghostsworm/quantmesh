package main

import (
	"context"
	"testing"

	"quantmesh/strategy"
)

// This strategy exposes empty display/context inventory while its private
// recovery state is unresolved. It supplies no private-accounting proof.
type capitalMissingPrivateProof struct {
	strategy.Strategy
	pendingRecovery bool
}

func (*capitalMissingPrivateProof) GetPositions() []*strategy.Position { return nil }
func (*capitalMissingPrivateProof) GetOrders() []*strategy.Order       { return nil }
func (*capitalMissingPrivateProof) CapitalReleaseInventorySnapshot(context.Context) ([]*strategy.Position, []*strategy.Order, error) {
	return nil, nil, nil
}

func TestRuntimeStrategyCapitalReleaseRejectsMissingPrivateProof(t *testing.T) {
	rt, venue, _ := capitalReleaseRuntimeFixture(t)
	current := &capitalMissingPrivateProof{pendingRecovery: true}
	rt.StrategyManager.RegisterStrategy("extension", current, 1, 0)
	amounts, err := releaseRuntimeStrategyCapital(t.Context(), []*SymbolRuntime{rt}, "dca")
	if err == nil || amounts["dca"] != 0 || rt.StrategyManager.GetCapitalAllocator().GetUsed("dca") != 200 || venue.lastReadSymbol != "" || !current.pendingRecovery {
		t.Fatalf("unsupported private recovery proof authorized release: %v %v", amounts, err)
	}
	// Replacing the registration with a complete proof capability is necessary;
	// silently clearing the hidden state cannot manufacture that capability.
	current.pendingRecovery = false
	amounts, err = releaseRuntimeStrategyCapital(t.Context(), []*SymbolRuntime{rt}, "dca")
	if err == nil || amounts["dca"] != 0 {
		t.Fatal("missing proof capability inferred from empty memory")
	}
	rt.StrategyManager.RegisterStrategy("extension", &capitalReleaseRuntimeStrategy{}, 1, 0)
	amounts, err = releaseRuntimeStrategyCapital(t.Context(), []*SymbolRuntime{rt}, "dca")
	if err != nil || amounts["dca"] != 200 {
		t.Fatalf("complete replacement proof did not recover: %v %v", amounts, err)
	}
}

type capitalMissingInventoryProof struct{ strategy.Strategy }

func (*capitalMissingInventoryProof) VerifyCapitalReleaseState(ctx context.Context) error {
	return ctx.Err()
}
func (*capitalMissingInventoryProof) GetPositions() []*strategy.Position {
	panic("unsupported synchronous getter must not run")
}
func (*capitalMissingInventoryProof) GetOrders() []*strategy.Order {
	panic("unsupported synchronous getter must not run")
}

func TestRuntimeStrategyCapitalReleaseRejectsMissingContextInventory(t *testing.T) {
	rt, venue, _ := capitalReleaseRuntimeFixture(t)
	rt.StrategyManager.RegisterStrategy("extension", &capitalMissingInventoryProof{}, 1, 0)
	if err := verifyStandardSpotBotInventoryFlatContext(t.Context(), rt.SuperPositionManager, rt.StrategyManager, rt.capitalReleaseScope.Symbol); err == nil {
		t.Fatal("stop verification accepted missing context inventory capability")
	}
	amounts, err := releaseRuntimeStrategyCapital(t.Context(), []*SymbolRuntime{rt}, "dca")
	if err == nil || amounts["dca"] != 0 || rt.StrategyManager.GetCapitalAllocator().GetUsed("dca") != 200 || venue.lastReadSymbol != "" {
		t.Fatalf("missing context inventory released capital: %v %v", amounts, err)
	}
	rt.StrategyManager.RegisterStrategy("extension", &capitalReleaseRuntimeStrategy{}, 1, 0)
	amounts, err = releaseRuntimeStrategyCapital(t.Context(), []*SymbolRuntime{rt}, "dca")
	if err != nil || amounts["dca"] != 200 {
		t.Fatalf("complete context inventory did not recover: %v %v", amounts, err)
	}
}

func TestRuntimeStrategyCapitalReleaseBuiltinProofCapabilities(t *testing.T) {
	// DCA and enhanced DCA use the same concrete implementation. Grid's
	// independent slot ledger is checked by exact type and manager binding.
	for name, current := range map[string]strategy.Strategy{
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
	} {
		if _, ok := current.(strategy.CapitalReleaseInventoryReader); !ok {
			t.Errorf("builtin %s lacks context inventory", name)
		}
		if name != "grid" {
			if _, ok := current.(strategy.CapitalReleaseStateVerifier); !ok {
				t.Errorf("builtin %s lacks private recovery proof", name)
			}
		}
	}
}
