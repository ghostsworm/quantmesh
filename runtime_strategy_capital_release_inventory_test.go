package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"quantmesh/strategy"
)

type capitalReleaseContextInventoryStrategy struct {
	capitalReleaseRuntimeStrategy
	read func(context.Context) error
}

func (s *capitalReleaseContextInventoryStrategy) CapitalReleaseInventorySnapshot(ctx context.Context) ([]*strategy.Position, []*strategy.Order, error) {
	if s.read != nil {
		if err := s.read(ctx); err != nil {
			return nil, nil, err
		}
	}
	return s.positions, s.orders, nil
}

func (*capitalReleaseContextInventoryStrategy) GetPositions() []*strategy.Position {
	panic("capital proof must use cancellable inventory reader")
}

func (*capitalReleaseContextInventoryStrategy) GetOrders() []*strategy.Order {
	panic("capital proof must use cancellable inventory reader")
}

func TestRuntimeStrategyCapitalReleaseUsesContextInventoryProof(t *testing.T) {
	for _, scenario := range []string{"flat", "position", "order", "nil row", "read failure", "deadline"} {
		t.Run(scenario, func(t *testing.T) {
			rt, venue, _ := capitalReleaseRuntimeFixture(t)
			current := &capitalReleaseContextInventoryStrategy{}
			switch scenario {
			case "position":
				current.positions = []*strategy.Position{{Symbol: "BTCUSDT", Size: 1}}
			case "order":
				current.orders = []*strategy.Order{{Symbol: "BTCUSDT", Status: "NEW"}}
			case "nil row":
				current.positions = []*strategy.Position{nil}
			case "read failure":
				current.read = func(context.Context) error { return errors.New("inventory unavailable") }
			case "deadline":
				current.read = func(ctx context.Context) error { <-ctx.Done(); return ctx.Err() }
			}
			rt.StrategyManager.RegisterStrategy("dca", current, 1, 0)
			allocator := rt.StrategyManager.GetCapitalAllocator()
			allocator.Allocate()
			if !allocator.Reserve("dca", 200) {
				t.Fatal("fixture reserve failed")
			}
			ctx := t.Context()
			if scenario == "deadline" {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, 30*time.Millisecond)
				defer cancel()
			}
			released, err := releaseRuntimeStrategyCapital(ctx, []*SymbolRuntime{rt}, "dca")
			if scenario == "flat" {
				if err != nil || released["dca"] != 200 {
					t.Fatalf("valid inventory proof: %v, %v", released, err)
				}
				return
			}
			if err == nil || released["dca"] != 0 || allocator.GetUsed("dca") != 200 || venue.lastReadSymbol != "" {
				t.Fatalf("invalid inventory authorized release: %v, %v", released, err)
			}
			if scenario == "deadline" && !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("inventory deadline lost: %v", err)
			}
			current.read, current.positions, current.orders = nil, nil, nil
			released, err = releaseRuntimeStrategyCapital(t.Context(), []*SymbolRuntime{rt}, "dca")
			if err != nil || released["dca"] != 200 {
				t.Fatalf("legitimate retry failed: %v, %v", released, err)
			}
		})
	}
}
