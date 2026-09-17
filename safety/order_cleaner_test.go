package safety

import (
	"context"
	"testing"
	"time"

	"quantmesh/config"
)

type cleanerSlot struct {
	OrderID     int64
	OrderSide   string
	OrderStatus string
}

type fakeCleanerPM struct {
	slots   map[float64]cleanerSlot
	updated map[float64]string
}

func (f *fakeCleanerPM) IterateSlots(fn func(price float64, slot interface{}) bool) {
	for p, s := range f.slots {
		if !fn(p, s) {
			return
		}
	}
}

func (f *fakeCleanerPM) UpdateSlotOrderStatus(price float64, status string) {
	f.updated[price] = status
}

type fakeCleanerExec struct{ canceled []int64 }

func (f *fakeCleanerExec) BatchCancelOrders(ids []int64) error {
	f.canceled = append(f.canceled, ids...)
	return nil
}

func TestOrderCleaner_CleanupInterval(t *testing.T) {
	cases := []struct {
		name    string
		seconds int
		want    time.Duration
	}{
		{"configured", 60, 60 * time.Second},
		{"zero falls back", 0, DefaultOrderCleanupInterval},
		{"negative falls back", -5, DefaultOrderCleanupInterval},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &config.Config{}
			cfg.Timing.OrderCleanupInterval = tc.seconds
			if got := NewOrderCleaner(cfg, &fakeCleanerExec{}, &fakeCleanerPM{}).CleanupInterval(); got != tc.want {
				t.Fatalf("CleanupInterval() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestOrderCleaner_CleanupOnceCancelsFarthestBuys(t *testing.T) {
	cfg := &config.Config{}
	cfg.Trading.OrderCleanupThreshold = 4
	cfg.Trading.CleanupBatchSize = 2
	pm := &fakeCleanerPM{slots: map[float64]cleanerSlot{
		100: {OrderID: 1, OrderSide: "BUY", OrderStatus: "PLACED"},
		101: {OrderID: 2, OrderSide: "BUY", OrderStatus: "CONFIRMED"},
		102: {OrderID: 3, OrderSide: "BUY", OrderStatus: "PLACED"},
		103: {OrderID: 4, OrderSide: "BUY", OrderStatus: "PLACED"},
		110: {OrderID: 5, OrderSide: "SELL", OrderStatus: "PARTIALLY_FILLED"},
	}, updated: map[float64]string{}}
	exec := &fakeCleanerExec{}
	oc := NewOrderCleaner(cfg, exec, pm)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	oc.CleanupOnce(ctx)
	if len(exec.canceled) != 0 {
		t.Fatalf("canceled ctx must not clean, got cancels %v", exec.canceled)
	}

	oc.CleanupOnce(context.Background())
	if len(exec.canceled) != 2 || exec.canceled[0] != 1 || exec.canceled[1] != 2 {
		t.Fatalf("want the two lowest buys (ids 1,2) canceled, got %v", exec.canceled)
	}
	if pm.updated[100] != "CANCEL_REQUESTED" || pm.updated[101] != "CANCEL_REQUESTED" || len(pm.updated) != 2 {
		t.Fatalf("unexpected slot status updates %v", pm.updated)
	}
}
