package position

import (
	"fmt"
	"math"
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

	if err := spm.ForceSyncPositions(1.0); err != nil {
		t.Fatalf("trim sync: %v", err)
	}
	if filledB.PositionStatus != PositionStatusEmpty && filledB.PositionQty >= 0.8 {
		t.Fatalf("far excess slot should be trimmed: %#v", filledB)
	}
	before := filledA.PositionQty
	if err := spm.ForceSyncPositions(before + 0.5); err != nil {
		t.Fatalf("deficit sync: %v", err)
	}
	if filledA.PositionQty <= before {
		t.Fatalf("nearest slot should be filled up: before=%f after=%f", before, filledA.PositionQty)
	}
	if err := spm.ForceSyncPositions(0); err != nil {
		t.Fatalf("zero sync: %v", err)
	}
	if filledA.PositionStatus != PositionStatusEmpty || filledA.PositionQty != 0 {
		t.Fatalf("zero exchange position should clear local slots")
	}

	grc := config.GridRiskControl{Enabled: true, StopLossRatio: 0.2}
	initial := spm.config.Trading.GridRiskControl
	spm.SetGridRiskControl(grc)
	if got := spm.GetRiskControls().Grid; !got.Enabled || got.StopLossRatio != 0.2 {
		t.Fatalf("grid risk control not updated")
	}
	if spm.config.Trading.GridRiskControl != initial {
		t.Fatal("hot update mutated original config retained by other components")
	}
}

func TestForceSyncPositionsRejectsUnresolvedInventory(t *testing.T) {
	cfg := &config.Config{}
	cfg.Trading.Symbol = "BTCUSDT"
	cfg.Trading.PriceInterval = 100
	cfg.Trading.ProfitSpread = 50
	cfg.Trading.OrderQuantity = 100
	cfg.Trading.BuyWindowSize = 2
	cfg.Trading.SellWindowSize = 2
	spm := NewSuperPositionManager(cfg, &MockExecutor{}, &MockExchange{}, 2, 4)
	spm.setAnchorPrice(1000)
	slot := spm.getOrCreateSlot(1000)
	slot.PositionStatus = PositionStatusFilled
	slot.PositionQty = math.NaN()
	if err := spm.ForceSyncPositions(0.1); err == nil {
		t.Fatal("ForceSyncPositions() succeeded despite unresolved NaN inventory")
	}
}

func TestForceSyncPositionsMarksAdoptedQuantityCostUnverified(t *testing.T) {
	cfg := &config.Config{}
	cfg.Trading.Symbol = "BTCUSDT"
	cfg.Trading.PriceInterval = 100
	cfg.Trading.ProfitSpread = 50
	cfg.Trading.OrderQuantity = 100
	cfg.Trading.BuyWindowSize = 2
	cfg.Trading.SellWindowSize = 2
	spm := NewSuperPositionManager(cfg, &MockExecutor{}, &MockExchange{}, 2, 4)
	spm.setAnchorPrice(1000)
	slot := spm.getOrCreateSlot(1000)
	slot.PositionStatus = PositionStatusFilled
	slot.PositionQty = 0.5
	slot.AvgBuyPrice = 900

	if err := spm.ForceSyncPositions(1); err != nil {
		t.Fatalf("ForceSyncPositions() error = %v", err)
	}
	slot.mu.RLock()
	gotQty, gotEntry, gotUnverified := slot.PositionQty, slot.AvgBuyPrice, slot.CostBasisUnverified
	slot.mu.RUnlock()
	if gotQty != 1 || gotEntry != 0 || !gotUnverified {
		t.Fatalf("adopted slot qty/entry/unverified = %v/%v/%v, want 1/0/true", gotQty, gotEntry, gotUnverified)
	}
	if !spm.OpeningGate().HasBlock("grid_cost_basis_unverified") {
		t.Fatal("unverified cost basis did not block new openings")
	}
	if got := spm.GetUnrealizedPnL(1200); got != 0 {
		t.Fatalf("unverified inventory fabricated unrealized PnL %v, want 0", got)
	}
	if err := spm.ForceSyncPositions(0); err != nil {
		t.Fatalf("flat position sync: %v", err)
	}
	if spm.OpeningGate().HasBlock("grid_cost_basis_unverified") {
		t.Fatal("flat inventory did not clear the cost-basis opening block")
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
