package strategy

import (
	"context"
	"errors"
	"testing"

	"quantmesh/config"
	"quantmesh/position"
)

type capitalReleaseGridNoPrice struct{ position.IExchange }

func (*capitalReleaseGridNoPrice) GetLatestPrice(context.Context, string) (float64, error) {
	panic("capital flatness proof must not query display mark/PnL")
}

func TestCapitalReleaseGridInventoryRequiresOwnProof(t *testing.T) {
	manager := &position.SuperPositionManager{}
	grid := NewGridStrategy("grid", &config.Config{}, nil, &capitalReleaseGridNoPrice{}, manager)
	positions, orders, err := grid.CapitalReleaseInventorySnapshot(t.Context())
	if err == nil || positions != nil || orders != nil {
		t.Fatal("unverified display-empty manager returned proof")
	}
	manager.MarkGridRuntimeVenueFlatVerified()
	if err := grid.VerifyCapitalReleaseManagerBinding(manager); err != nil {
		t.Fatal(err)
	}
	if err := grid.VerifyCapitalReleaseManagerBinding(&position.SuperPositionManager{}); err == nil {
		t.Fatal("foreign manager treated as startup binding")
	}
	positions, orders, err = grid.CapitalReleaseInventorySnapshot(t.Context())
	if err != nil || len(positions) != 0 || len(orders) != 0 {
		t.Fatalf("legitimate Grid proof failed: %v %v %v", positions, orders, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if positions, orders, err := grid.CapitalReleaseInventorySnapshot(ctx); positions != nil || orders != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled Grid proof: %v %v %v", positions, orders, err)
	}
	if _, _, err := grid.CapitalReleaseInventorySnapshot(nil); err == nil {
		t.Fatal("nil context accepted")
	}
	for _, missing := range []*GridStrategy{nil, {}} {
		if _, _, err := missing.CapitalReleaseInventorySnapshot(t.Context()); err == nil {
			t.Fatal("missing manager accepted")
		}
	}
}
