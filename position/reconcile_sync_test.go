package position

import (
	"fmt"
	"testing"
	"time"

	"quantmesh/config"
)

func TestSuperPositionManagerReconciliationAndForceSync(t *testing.T) {
	cfg := &config.Config{}
	cfg.Trading.Symbol = "BTCUSDT"
	cfg.Trading.PriceInterval = 100
	cfg.Trading.OrderQuantity = 100
	cfg.Trading.BuyWindowSize = 2
	executor := &MockExecutor{}
	spm := NewSuperPositionManager(cfg, executor, &MockExchange{}, 2, 4)
	spm.setAnchorPrice(1000)
	spm.lastMarketPrice.Store(1000.0)

	when := time.Now().Add(-time.Hour)
	if err := spm.RestoreReconciliationStats(nil, "binance", "BTCUSDT"); err != nil {
		t.Fatalf("nil restore: %v", err)
	}
	store := &fakeReconciliationStorage{
		history: &fakeReconciliationHistory{TotalBuyQty: 1.2, TotalSellQty: 0.8, ReconcileTime: when},
		count:   7,
	}
	if err := spm.RestoreReconciliationStats(store, "binance", "BTCUSDT"); err != nil {
		t.Fatalf("restore stats: %v", err)
	}
	if spm.GetReconcileCount() != 7 || spm.GetTotalBuyQty() != 1.2 || spm.GetTotalSellQty() != 0.8 || !spm.GetLastReconcileTime().Equal(when) {
		t.Fatalf("restored stats count=%d buy=%f sell=%f time=%s", spm.GetReconcileCount(), spm.GetTotalBuyQty(), spm.GetTotalSellQty(), spm.GetLastReconcileTime())
	}
	store.err = fmt.Errorf("boom")
	if err := spm.RestoreReconciliationStats(store, "binance", "BTCUSDT"); err == nil {
		t.Fatalf("restore error should bubble")
	}
	store.err = nil
	store.history = "bad"
	if err := spm.RestoreReconciliationStats(store, "binance", "BTCUSDT"); err == nil {
		t.Fatalf("bad history should fail")
	}

	spm.UpdateSlotOrderStatus(900, OrderStatusPlaced)
	if slot := spm.getOrCreateSlot(900); slot.OrderStatus != OrderStatusPlaced {
		t.Fatalf("slot status=%s", slot.OrderStatus)
	}

	for i, price := range []float64{900, 800, 700} {
		slot := spm.getOrCreateSlot(price)
		slot.OrderID = int64(100 + i)
		slot.OrderSide = "BUY"
		slot.OrderStatus = OrderStatusPlaced
		slot.PositionStatus = PositionStatusEmpty
	}
	spm.CancelExcessOpenOrders(1)
	if len(executor.CancelledOrderIDs) != 2 {
		t.Fatalf("cancelled orders=%#v", executor.CancelledOrderIDs)
	}
	if spm.getOrCreateSlot(900).OrderStatus != OrderStatusCancelRequested {
		t.Fatalf("highest buy should be cancel requested first")
	}

	filledA := spm.getOrCreateSlot(1000)
	filledA.PositionStatus = PositionStatusFilled
	filledA.PositionQty = 1.0
	filledA.OrderID = 222
	filledA.OrderStatus = OrderStatusPlaced
	filledB := spm.getOrCreateSlot(1300)
	filledB.PositionStatus = PositionStatusFilled
	filledB.PositionQty = 0.8
	filledB.OrderID = 333
	filledB.OrderStatus = OrderStatusPlaced

	spm.ForceSyncPositions(1.0)
	if filledB.PositionStatus != PositionStatusEmpty && filledB.PositionQty >= 0.8 {
		t.Fatalf("far excess slot should be trimmed: %#v", filledB)
	}
	before := filledA.PositionQty
	spm.ForceSyncPositions(before + 0.5)
	if filledA.PositionQty <= before {
		t.Fatalf("nearest slot should be filled up: before=%f after=%f", before, filledA.PositionQty)
	}
	spm.ForceSyncPositions(0)
	if filledA.PositionStatus != PositionStatusEmpty || filledA.PositionQty != 0 {
		t.Fatalf("zero exchange position should clear local slots")
	}

	grc := config.GridRiskControl{Enabled: true, StopLossRatio: 0.2}
	spm.SetGridRiskControl(grc)
	if !spm.config.Trading.GridRiskControl.Enabled || spm.config.Trading.GridRiskControl.StopLossRatio != 0.2 {
		t.Fatalf("grid risk control not updated")
	}
}

type fakeReconciliationHistory struct {
	TotalBuyQty   float64
	TotalSellQty  float64
	ReconcileTime time.Time
}

type fakeReconciliationStorage struct {
	history interface{}
	count   int64
	err     error
}

func (s *fakeReconciliationStorage) GetLatestReconciliationHistory(string, string) (interface{}, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.history, nil
}

func (s *fakeReconciliationStorage) GetReconciliationCount(string, string) (int64, error) {
	if s.err != nil {
		return 0, s.err
	}
	return s.count, nil
}
