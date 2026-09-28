package strategy

import (
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
