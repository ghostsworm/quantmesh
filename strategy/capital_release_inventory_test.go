package strategy

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestCapitalReleaseInventoryWaitRespectsDeadline(t *testing.T) {
	current := &TrendFollowingStrategy{}
	current.mu.Lock()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, _, err := current.CapitalReleaseInventorySnapshot(ctx)
		done <- err
	}()
	var err error
	select {
	case err = <-done:
		current.mu.Unlock()
	case <-time.After(time.Second):
		current.mu.Unlock()
		err = <-done
		t.Error("inventory getter waited beyond proof deadline")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("inventory error = %v", err)
	}
}

func TestCapitalReleaseInventoryCopiesAndLockRecovery(t *testing.T) {
	position := &Position{Symbol: "BTCUSDT", Size: 1}
	order := &Order{Symbol: "BTCUSDT", Status: "NEW"}
	trend := &TrendFollowingStrategy{position: position, activeOrder: order}
	mean := &MeanReversionStrategy{position: position, activeOrder: order}
	momentum := &MomentumStrategy{position: position, activeOrder: order}
	spotLong := &SpotLongStrategy{positions: []*Position{position, nil}, orders: []*Order{order, nil}}
	spotShort := &SpotShortStrategy{positions: []*Position{position}, orders: []*Order{order}}
	futuresLong := &FuturesLongStrategy{positions: []*Position{position}, orders: []*Order{order}}
	futuresShort := &FuturesShortStrategy{positions: []*Position{position}, orders: []*Order{order}}
	for _, tc := range []struct {
		name   string
		reader CapitalReleaseInventoryReader
		mu     *sync.RWMutex
	}{
		{"trend", trend, &trend.mu}, {"mean", mean, &mean.mu}, {"momentum", momentum, &momentum.mu},
		{"spot long", spotLong, &spotLong.mu}, {"spot short", spotShort, &spotShort.mu},
		{"futures long", futuresLong, &futuresLong.mu}, {"futures short", futuresShort, &futuresShort.mu},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.mu.Lock()
			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
			defer cancel()
			done := make(chan error, 1)
			go func() { _, _, err := tc.reader.CapitalReleaseInventorySnapshot(ctx); done <- err }()
			var err error
			select {
			case err = <-done:
				tc.mu.Unlock()
			case <-time.After(time.Second):
				tc.mu.Unlock()
				err = <-done
				t.Error("inventory lock wait outlived context")
			}
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("lock cancellation: %v", err)
			}
			positions, orders, err := tc.reader.CapitalReleaseInventorySnapshot(t.Context())
			if err != nil || positions[0] == position || orders[0] == order || positions[0].Size != 1 || orders[0].Status != "NEW" {
				t.Fatalf("inventory copy failed: positions=%v orders=%v err=%v", positions, orders, err)
			}
			positions[0].Size = 0
			orders[0].Status = "FILLED"
			if position.Size != 1 || order.Status != "NEW" {
				t.Fatal("proof result aliases mutable strategy inventory")
			}
			if tc.name == "spot long" && (len(positions) != 2 || positions[1] != nil || orders[1] != nil) {
				t.Fatal("malformed nil rows disappeared from proof")
			}
			if positions, orders, err := tc.reader.CapitalReleaseInventorySnapshot(nil); err == nil || positions != nil || orders != nil {
				t.Fatal("nil context accepted")
			}
		})
	}
}
