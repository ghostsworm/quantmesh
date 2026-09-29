package strategy

import (
	"math"
	"testing"

	"quantmesh/config"
	"quantmesh/position"
)

func TestMartingaleRealizedPnLIncludesQuoteValuedEntryAndExitFees(t *testing.T) {
	s := NewMartingaleStrategy("martingale", "BTCUSDT", &config.Config{}, &hedgeOrderExecutor{}, &hedgeExchange{}, nil)
	setTestRuntimeStateStore(t, s)
	s.direction = "LONG"
	entry := &MartingaleEntry{Level: 1, OrderID: 11, Status: entryStatusPending}
	s.entries = []*MartingaleEntry{entry}
	s.handleEntryOrderUpdate(entry, &position.OrderUpdate{
		OrderID: 11, Status: "FILLED", ExecutedQty: 1, AvgPrice: 100,
		Commission: 0.2, CommissionAsset: "USDT",
	})
	if entry.OpeningFee != 0.2 || s.totalQty != 1 {
		t.Fatalf("entry fee/quantity not booked: entry=%+v total=%v", entry, s.totalQty)
	}
	s.isClosing, s.closeOrderID = true, 12
	if err := s.OnOrderUpdate(&position.OrderUpdate{
		OrderID: 12, Status: "FILLED", ExecutedQty: 1, AvgPrice: 110,
		Commission: 0.1, CommissionAsset: "USDT",
	}); err != nil {
		t.Fatal(err)
	}
	if s.stats.TotalPnL != 9.7 || s.totalQty != 0 {
		t.Fatalf("net PnL should be 10 gross - 0.2 entry fee - 0.1 exit fee: stats=%+v qty=%v", s.stats, s.totalQty)
	}
}

func TestMartingaleNonFiniteEntryFillRetainsOrderForReconciliation(t *testing.T) {
	s := NewMartingaleStrategy("martingale", "BTCUSDT", &config.Config{}, &hedgeOrderExecutor{}, &hedgeExchange{}, nil)
	setTestRuntimeStateStore(t, s)
	s.direction = "LONG"
	entry := &MartingaleEntry{Level: 1, OrderID: 31, Status: entryStatusPending}
	s.entries = []*MartingaleEntry{entry}
	if err := s.OnOrderUpdate(&position.OrderUpdate{
		OrderID: 31, Status: "FILLED", ExecutedQty: math.NaN(), AvgPrice: 100,
	}); err != nil {
		t.Fatal(err)
	}
	if len(s.entries) != 1 || s.entries[0] != entry || entry.Status != position.OrderStatusUnknown || entry.Quantity != 0 || s.totalQty != 0 {
		t.Fatalf("non-finite fill must retain an UNKNOWN entry for reconciliation: entries=%+v total=%v", s.entries, s.totalQty)
	}
}

func TestMartingaleNonFiniteCloseFillDoesNotConsumeInventory(t *testing.T) {
	tests := []struct {
		name   string
		update position.OrderUpdate
	}{
		{name: "quantity", update: position.OrderUpdate{ExecutedQty: math.NaN(), AvgPrice: 110}},
		{name: "price", update: position.OrderUpdate{ExecutedQty: 1, AvgPrice: math.Inf(1)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewMartingaleStrategy("martingale", "BTCUSDT", &config.Config{}, &hedgeOrderExecutor{}, &hedgeExchange{}, nil)
			setTestRuntimeStateStore(t, s)
			s.direction = "LONG"
			s.entries = []*MartingaleEntry{{Level: 1, Price: 100, Quantity: 1, Cost: 100, Status: entryStatusFilled}}
			s.updateTotals()
			s.isClosing, s.closeOrderID, s.closeRequestedQty = true, 41, 1
			tt.update.OrderID, tt.update.Status = 41, "PARTIALLY_FILLED"
			if err := s.OnOrderUpdate(&tt.update); err != nil {
				t.Fatal(err)
			}
			if !s.isClosing || s.totalQty != 1 || s.closeProgress.Quantity != 0 || s.stats.TotalPnL != 0 {
				t.Fatalf("invalid close fill consumed state: closing=%v qty=%v progress=%+v pnl=%v", s.isClosing, s.totalQty, s.closeProgress, s.stats.TotalPnL)
			}
		})
	}
}

func TestMartingaleUnknownFeeAssetDoesNotConsumeFill(t *testing.T) {
	s := NewMartingaleStrategy("martingale", "BTCUSDT", &config.Config{}, &hedgeOrderExecutor{}, &hedgeExchange{}, nil)
	setTestRuntimeStateStore(t, s)
	s.direction = "LONG"
	entry := &MartingaleEntry{Level: 1, OrderID: 21, Status: entryStatusPending}
	s.entries = []*MartingaleEntry{entry}
	s.handleEntryOrderUpdate(entry, &position.OrderUpdate{
		OrderID: 21, Status: "PARTIALLY_FILLED", ExecutedQty: 0.5, AvgPrice: 100,
		Commission: 0.01, CommissionAsset: "BNB",
	})
	if entry.FillProgress.Quantity != 0 || entry.Quantity != 0 || s.totalQty != 0 {
		t.Fatalf("unknown fee currency consumed fill: entry=%+v total=%v", entry, s.totalQty)
	}
}
