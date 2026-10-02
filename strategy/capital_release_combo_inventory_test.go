package strategy

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"
)

func TestCapitalReleaseComboInventoryWaitDeadline(t *testing.T) {
	for _, phase := range []string{"parent", "child"} {
		t.Run(phase, func(t *testing.T) {
			child := &TrendFollowingStrategy{}
			combo := &ComboStrategy{strategies: []Strategy{child}}
			var mu *sync.RWMutex
			if phase == "parent" {
				mu = &combo.mu
			} else {
				mu = &child.mu
			}
			mu.Lock()
			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
			defer cancel()
			done := make(chan error, 1)
			go func() { _, _, err := combo.CapitalReleaseInventorySnapshot(ctx); done <- err }()
			var err error
			select {
			case err = <-done:
				mu.Unlock()
			case <-time.After(time.Second):
				mu.Unlock()
				err = <-done
				t.Error("Combo inventory lock wait outlived deadline")
			}
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("Combo cancellation = %v", err)
			}
			if !combo.mu.TryLock() {
				t.Fatal("cancelled child wait retained parent lock")
			}
			combo.mu.Unlock()
			positions, orders, err := combo.CapitalReleaseInventorySnapshot(t.Context())
			if err != nil || len(positions) != 0 || len(orders) != 0 {
				t.Fatalf("legitimate retry failed: %v %v %v", positions, orders, err)
			}
		})
	}
}

func TestCapitalReleaseComboInventoryBuiltInTypes(t *testing.T) {
	for _, kind := range []string{"dca", "dca_enhanced", "martingale", "trend", "mean_reversion", "momentum"} {
		t.Run(kind, func(t *testing.T) {
			combo, _, _ := comboCapitalFixture(t, kind)
			positions, orders, err := combo.CapitalReleaseInventorySnapshot(t.Context())
			if err != nil || !reflect.DeepEqual(positions, combo.GetPositions()) || !reflect.DeepEqual(orders, combo.GetOrders()) {
				t.Fatalf("supported %s inventory changed: %v %v %v", kind, positions, orders, err)
			}
		})
	}
}

func TestCapitalReleaseComboInventoryRejectsMissingChildren(t *testing.T) {
	var missing *TrendFollowingStrategy
	for _, children := range [][]Strategy{nil, {nil}, {missing}, {&fakeComboSubStrategy{}}, {&ComboStrategy{}}} {
		combo := &ComboStrategy{strategies: children}
		positions, orders, err := combo.CapitalReleaseInventorySnapshot(t.Context())
		if err == nil || positions != nil || orders != nil {
			t.Fatalf("unsupported child returned partial proof: %v %v %v", positions, orders, err)
		}
	}
	combo := &ComboStrategy{strategies: []Strategy{&TrendFollowingStrategy{}}}
	if _, _, err := combo.CapitalReleaseInventorySnapshot(nil); err == nil {
		t.Fatal("nil context accepted")
	}
}

func TestCapitalReleaseComboInventoryCombinesIndependentRows(t *testing.T) {
	position := &Position{Symbol: "BTCUSDT", Size: 1}
	order := &Order{Symbol: "BTCUSDT", Status: "NEW"}
	combo := &ComboStrategy{strategies: []Strategy{
		&TrendFollowingStrategy{position: position}, &MomentumStrategy{activeOrder: order},
	}}
	positions, orders, err := combo.CapitalReleaseInventorySnapshot(t.Context())
	if err != nil || len(positions) != 1 || len(orders) != 1 || positions[0] == position || orders[0] == order {
		t.Fatalf("Combo lost/aliased child inventory: %v %v %v", positions, orders, err)
	}
	positions[0].Size = 0
	orders[0].Status = "FILLED"
	if position.Size != 1 || order.Status != "NEW" {
		t.Fatal("Combo snapshot changed child state")
	}
}

type comboInventoryContextReader struct {
	fakeComboSubStrategy
	read func(context.Context) error
}

func (s *comboInventoryContextReader) CapitalReleaseInventorySnapshot(ctx context.Context) ([]*Position, []*Order, error) {
	return nil, nil, s.read(ctx)
}

func TestCapitalReleaseComboInventoryDoesNotReturnPartialProof(t *testing.T) {
	readErr := errors.New("child inventory unavailable")
	for _, scenario := range []string{"read failure", "cancelled child"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			var received context.Context
			reader := &comboInventoryContextReader{read: func(childCtx context.Context) error {
				received = childCtx
				if scenario == "cancelled child" {
					cancel()
					return nil // Even a child ignoring cancellation cannot grant proof.
				}
				return readErr
			}}
			combo := &ComboStrategy{strategies: []Strategy{
				&TrendFollowingStrategy{position: &Position{Symbol: "BTCUSDT", Size: 1}}, reader,
			}}
			positions, orders, err := combo.CapitalReleaseInventorySnapshot(ctx)
			wantErr := readErr
			if scenario == "cancelled child" {
				wantErr = context.Canceled
			}
			if positions != nil || orders != nil || !errors.Is(err, wantErr) || received != ctx {
				t.Fatalf("partial/cancelled inventory proof: %v %v %v", positions, orders, err)
			}
		})
	}
}
