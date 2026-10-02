package strategy

import (
	"context"
	"errors"
	"math"
	"reflect"
	"sync"
	"testing"
	"time"

	"quantmesh/position"
)

func TestCapitalReleaseLayerInventoryWaitDeadline(t *testing.T) {
	dca := &DCAEnhancedStrategy{strategyCfg: &DCAEnhancedConfig{}}
	martingale := &MartingaleStrategy{strategyCfg: &MartingaleConfig{}}
	for _, tc := range []struct {
		name string
		mu   *sync.RWMutex
		read func(context.Context) error
	}{
		{"dca", &dca.mu, func(ctx context.Context) error { _, _, err := dca.CapitalReleaseInventorySnapshot(ctx); return err }},
		{"martingale", &martingale.mu, func(ctx context.Context) error {
			_, _, err := martingale.CapitalReleaseInventorySnapshot(ctx)
			return err
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.mu.Lock()
			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
			defer cancel()
			done := make(chan error, 1)
			go func() { done <- tc.read(ctx) }()
			var err error
			select {
			case err = <-done:
				tc.mu.Unlock()
			case <-time.After(time.Second):
				tc.mu.Unlock()
				err = <-done
				t.Error("layer inventory getter outlived deadline")
			}
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("inventory cancellation = %v", err)
			}
			if err := tc.read(t.Context()); err != nil {
				t.Fatalf("retry after timeout failed: %v", err)
			}
		})
	}
}

func TestCapitalReleaseLayerInventoryRejectsMalformedState(t *testing.T) {
	for _, reader := range []CapitalReleaseInventoryReader{
		&DCAEnhancedStrategy{}, &MartingaleStrategy{},
		&DCAEnhancedStrategy{strategyCfg: &DCAEnhancedConfig{}, layers: []*DCALayer{nil}},
		&MartingaleStrategy{strategyCfg: &MartingaleConfig{}, entries: []*MartingaleEntry{nil}},
		&DCAEnhancedStrategy{strategyCfg: &DCAEnhancedConfig{}, totalQty: -1},
		&MartingaleStrategy{strategyCfg: &MartingaleConfig{}, totalQty: math.NaN()},
	} {
		positions, orders, err := reader.CapitalReleaseInventorySnapshot(t.Context())
		if err == nil || positions != nil || orders != nil {
			t.Fatalf("malformed %T inventory returned proof: %v %v %v", reader, positions, orders, err)
		}
	}
	for _, reader := range []CapitalReleaseInventoryReader{
		&DCAEnhancedStrategy{strategyCfg: &DCAEnhancedConfig{}},
		&MartingaleStrategy{strategyCfg: &MartingaleConfig{}},
	} {
		if _, _, err := reader.CapitalReleaseInventorySnapshot(nil); err == nil {
			t.Fatal("nil context accepted")
		}
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		if _, _, err := reader.CapitalReleaseInventorySnapshot(ctx); !errors.Is(err, context.Canceled) {
			t.Fatalf("cancelled inventory: %v", err)
		}
		positions, orders, err := reader.CapitalReleaseInventorySnapshot(t.Context())
		if err != nil || len(positions) != 0 || len(orders) != 0 {
			t.Fatalf("legitimate empty recovery failed: %v %v %v", positions, orders, err)
		}
	}
}

// These fixtures pin projection semantics, not just equality with a shared helper.
func TestCapitalReleaseLayerInventoryProjection(t *testing.T) {
	dca := &DCAEnhancedStrategy{strategyCfg: &DCAEnhancedConfig{Symbol: "BTCUSDT"}, totalQty: 2, totalCost: 200, avgEntryPrice: 100, lastPrice: 110,
		layers: []*DCALayer{{OrderID: 1, Price: 100, Quantity: 2, RequestedQuantity: 3, Status: "filled", OpeningFee: 4, FillProgress: position.FillProgress{Quantity: 2, Notional: 200}}}}
	martingale := &MartingaleStrategy{strategyCfg: &MartingaleConfig{Symbol: "BTCUSDT"}, direction: "SHORT", totalQty: 2, totalCost: 200, avgEntryPrice: 100, lastPrice: 110,
		entries: []*MartingaleEntry{{OrderID: 1, Price: 100, Quantity: 2, RequestedQuantity: 3, Status: "filled", OpeningFee: 4, FillProgress: position.FillProgress{Quantity: 2, Notional: 200}}}}
	for _, tc := range []struct {
		name    string
		current Strategy
		pnl     float64
		side    string
	}{
		{"dca", dca, 16, "BUY"}, {"martingale", martingale, -24, "SELL"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			positions, orders := tc.current.GetPositions(), tc.current.GetOrders()
			if positions[0].Size != 2 || positions[0].OpeningFee != 4 || positions[0].PnL != tc.pnl || orders[0].Quantity != 3 || orders[0].Side != tc.side || orders[0].FillProgress.Quantity != 2 {
				t.Fatalf("baseline projection unexpected: %v %v", positions, orders)
			}
			snapshotPositions, snapshotOrders, err := tc.current.(CapitalReleaseInventoryReader).CapitalReleaseInventorySnapshot(t.Context())
			if err != nil || !reflect.DeepEqual(snapshotPositions, positions) || !reflect.DeepEqual(snapshotOrders, orders) {
				t.Fatalf("context snapshot changed projection: %v %v %v", snapshotPositions, snapshotOrders, err)
			}
			snapshotPositions[0].Size = 0
			snapshotOrders[0].Quantity = 0
			if tc.current.GetPositions()[0].Size != 2 || tc.current.GetOrders()[0].Quantity != 3 {
				t.Fatal("projection aliases strategy accounting")
			}
		})
	}
}
