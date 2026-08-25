package position

import (
	"context"
	"math"
	"testing"
	"time"

	"quantmesh/config"
)

type orderBookExchange struct {
	MockExchange
	orderBook *OrderBook
}

func (e *orderBookExchange) GetOrderBook(ctx context.Context, symbol string, limit int) (*OrderBook, error) {
	return e.orderBook, nil
}

func newStateTestSPM(direction, marketType string) (*SuperPositionManager, *MockExecutor) {
	cfg := &config.Config{}
	cfg.Trading.Symbol = "BTCUSDT"
	cfg.Trading.Direction = direction
	cfg.Trading.MarketType = marketType
	cfg.Trading.PriceInterval = 100
	cfg.Trading.ProfitSpread = 120
	cfg.Trading.BuyWindowSize = 3
	cfg.Trading.SellWindowSize = 4
	cfg.Trading.OrderQuantity = 1000
	exec := &MockExecutor{}
	return NewSuperPositionManager(cfg, exec, &MockExchange{}, 2, 4), exec
}

func TestSuperPositionManagerStateAccessorsAndCounters(t *testing.T) {
	spm, _ := newStateTestSPM("LONG", "futures")

	if spm.logPrefix() != "binance:btcusdt:futures" {
		t.Fatalf("logPrefix() = %q", spm.logPrefix())
	}
	spm.botID = "bot-1"
	if spm.logPrefix() != "bot-1" {
		t.Fatalf("bot logPrefix() = %q", spm.logPrefix())
	}

	spm.Pause()
	if !spm.IsPaused() {
		t.Fatalf("Pause() did not set paused flag")
	}
	spm.Resume()
	if spm.IsPaused() {
		t.Fatalf("Resume() did not clear paused flag")
	}
	spm.openingPauseReason.Store("risk")
	if got := spm.GetOpeningPauseReason(); got != "risk" {
		t.Fatalf("GetOpeningPauseReason() = %q", got)
	}
	spm.ResumeOpening()
	if spm.IsOpeningPaused() || spm.GetOpeningPauseReason() != "" {
		t.Fatalf("ResumeOpening() did not clear opening pause state")
	}

	spm.SetTradeStorage(nil)
	spm.SetTrendDetector(nil)
	spm.SetFundingMonitor(nil)
	if spm.GetFundingMonitor() != nil {
		t.Fatalf("GetFundingMonitor() = non-nil after setting nil")
	}
	spm.SetRequestStopFunc(func() {})
	spm.SetArbitrageManager(nil)
	if spm.GetArbitrageManager() != nil {
		t.Fatalf("GetArbitrageManager() = non-nil after setting nil")
	}

	spm.fillTimestamps = []time.Time{time.Now().Add(-2 * time.Minute), time.Now().Add(-30 * time.Second)}
	if got := spm.GetFillCountInLastMinute(); got != 1 {
		t.Fatalf("GetFillCountInLastMinute() = %d, want 1", got)
	}
	spm.IncrementReconcileCount()
	if spm.GetReconcileCount() != 1 {
		t.Fatalf("GetReconcileCount() = %d, want 1", spm.GetReconcileCount())
	}
	now := time.Now()
	spm.UpdateLastReconcileTime(now)
	if !spm.GetLastReconcileTime().Equal(now) {
		t.Fatalf("GetLastReconcileTime() mismatch")
	}
	if spm.GetSymbol() != "BTCUSDT" || spm.GetExchange() != "binance" {
		t.Fatalf("symbol/exchange = %s/%s", spm.GetSymbol(), spm.GetExchange())
	}
	if spm.GetAllocationManager() == nil {
		t.Fatalf("GetAllocationManager() = nil")
	}
}

func TestSuperPositionManagerSlotFiltersAndSlotSnapshots(t *testing.T) {
	spm, exec := newStateTestSPM("LONG", "futures")
	slot := spm.getOrCreateSlot(49900)
	slot.mu.Lock()
	slot.OrderID = 77
	slot.OrderSide = "BUY"
	slot.OrderStatus = OrderStatusPlaced
	slot.OrderPrice = 49900
	slot.OrderFilledQty = 0.01
	slot.PositionStatus = PositionStatusFilled
	slot.PositionQty = 0.02
	slot.AvgBuyPrice = 49800
	slot.StrategyName = "grid"
	slot.StrategyType = "grid"
	slot.mu.Unlock()
	spm.getOrCreateSlot(49800)

	filter := &config.SlotFilterConfig{Rules: []config.SlotFilterRule{
		{Type: "exclude", Prices: []float64{49900}, Reason: "manual"},
		{Type: "include", MinPrice: 49700, MaxPrice: 49850, Reason: "range"},
	}}
	spm.slotFilter = filter
	if spm.GetSlotFilter() != filter {
		t.Fatalf("GetSlotFilter() did not return configured filter")
	}
	if spm.isSlotEnabled(49900) {
		t.Fatalf("excluded slot should be disabled")
	}
	if !spm.isSlotEnabled(49800) {
		t.Fatalf("included range slot should be enabled")
	}
	spm.cancelFilteredSlotOrders()
	if len(exec.CancelledOrderIDs) != 1 || exec.CancelledOrderIDs[0] != 77 {
		t.Fatalf("cancelFilteredSlotOrders() cancelled IDs = %v, want [77]", exec.CancelledOrderIDs)
	}

	var iterated int
	spm.IterateSlots(func(price float64, slot interface{}) bool {
		iterated++
		if _, ok := slot.(SlotData); !ok {
			t.Fatalf("IterateSlots slot type = %T, want SlotData", slot)
		}
		return true
	})
	if iterated != spm.GetSlotCount() {
		t.Fatalf("IterateSlots count = %d, want %d", iterated, spm.GetSlotCount())
	}

	detailed := spm.GetAllSlotsDetailed()
	if len(detailed) != spm.GetSlotCount() {
		t.Fatalf("GetAllSlotsDetailed() length = %d, want %d", len(detailed), spm.GetSlotCount())
	}
	if got, _, ok := spm.findSlotByOrderID(77); got == nil || !ok {
		t.Fatalf("findSlotByOrderID(77) did not find slot")
	}
	if slot, _, ok := spm.findSlotByOrderID(0); slot != nil || ok {
		t.Fatalf("findSlotByOrderID(0) = %v/%v, want nil/false", slot, ok)
	}
}

func TestSuperPositionManagerGridMathRocketAndSummary(t *testing.T) {
	spm, _ := newStateTestSPM("LONG", "futures")
	spm.setAnchorPrice(50000)
	spm.lastMarketPrice.Store(50120.0)

	if !spm.isLong() || spm.isShort() || spm.isSpot() || spm.isBoth() {
		t.Fatalf("direction/market helpers mismatch")
	}
	if got := spm.findNearestGridPrice(50149); got != 50100 {
		t.Fatalf("findNearestGridPrice() = %.2f, want 50100", got)
	}
	spm.config.Trading.GridMode = "geometric"
	spm.config.Trading.PriceInterval = 0.01
	if got := spm.findNearestGridPrice(50501); got <= 50000 {
		t.Fatalf("geometric nearest grid = %.2f, want above anchor", got)
	}
	spm.config.Trading.GridMode = "arithmetic"
	spm.config.Trading.PriceInterval = 100

	spm.config.Trading.RocketTieredGrid = &config.RocketTieredGridConfig{
		Enabled: true,
		Tiers: []config.RocketTier{
			{FilledThreshold: 2, Interval: 50, ProfitSpread: 60},
			{FilledThreshold: 4, Interval: 150, ProfitSpread: 170},
			{FilledThreshold: 0, Interval: 300, ProfitSpread: 330},
		},
	}
	prices := spm.calculateSlotPrices(50000, 5, "down")
	want := []float64{49950, 49900, 49750, 49600, 49300}
	if len(prices) != len(want) {
		t.Fatalf("rocket prices length = %d, want %d: %v", len(prices), len(want), prices)
	}
	for i := range want {
		if prices[i] != want[i] {
			t.Fatalf("rocket price[%d] = %.2f, want %.2f (%v)", i, prices[i], want[i], prices)
		}
	}
	if got := spm.getRocketIntervalForSlotIndex(3, spm.config.Trading.RocketTieredGrid.Tiers, 100); got != 150 {
		t.Fatalf("rocket interval = %.2f, want 150", got)
	}
	if got := spm.getProfitSpreadForSlot(49750, 50000); got != 170 {
		t.Fatalf("rocket profit spread = %.2f, want 170", got)
	}
	if got := spm.inferRocketSlotIndex(250, spm.config.Trading.RocketTieredGrid.Tiers); got != 2 {
		t.Fatalf("inferRocketSlotIndex() = %d, want 2", got)
	}

	changed := spm.UpdateTradingParams(120, 130, 1500, 4, 5)
	if !changed || spm.GetPriceInterval() != 120 || spm.GetProfitSpread() != 130 {
		t.Fatalf("UpdateTradingParams changed=%v interval/spread=%.2f/%.2f", changed, spm.GetPriceInterval(), spm.GetProfitSpread())
	}
	if spm.UpdateTradingParams(120, 130, 1500, 4, 5) {
		t.Fatalf("UpdateTradingParams() reported change for identical values")
	}
	summary := spm.GetTradingParamsSummary()
	if summary["price_interval"].(float64) != 120 || summary["current_price"].(float64) != 50120 {
		t.Fatalf("GetTradingParamsSummary() = %+v", summary)
	}
}

func TestSuperPositionManagerPnLInventoryAndCleanup(t *testing.T) {
	spm, _ := newStateTestSPM("BOTH", "futures")
	longSlot := spm.getOrCreateSlot(49900)
	longSlot.mu.Lock()
	longSlot.PositionStatus = PositionStatusFilled
	longSlot.PositionQty = 0.1
	longSlot.AvgBuyPrice = 49800
	longSlot.PositionLeg = PositionLegLong
	longSlot.mu.Unlock()
	shortSlot := spm.getOrCreateSlot(50200)
	shortSlot.mu.Lock()
	shortSlot.PositionStatus = PositionStatusFilled
	shortSlot.PositionQty = 0.2
	shortSlot.AvgBuyPrice = 50300
	shortSlot.PositionLeg = PositionLegShort
	shortSlot.mu.Unlock()
	pending := spm.getOrCreateSlot(49700)
	pending.mu.Lock()
	pending.OrderSide = "BUY"
	pending.OrderPrice = 49700
	pending.OrderStatus = OrderStatusPartiallyFilled
	pending.OrderFilledQty = 0.01
	pending.mu.Unlock()
	empty := spm.getOrCreateSlot(45000)

	if got := spm.GetTotalPositionValueUSDT(); math.Abs(got-(49900*0.1+50200*0.2)) > 1e-9 {
		t.Fatalf("GetTotalPositionValueUSDT() = %.4f", got)
	}
	if got := spm.GetPendingBuyOrderValueUSDT(); math.Abs(got-(1000-497)) > 1e-9 {
		t.Fatalf("GetPendingBuyOrderValueUSDT() = %.4f, want 503", got)
	}
	if got := spm.GetUnrealizedPnL(50100); math.Abs(got-70) > 1e-9 {
		t.Fatalf("GetUnrealizedPnL() = %.4f, want 70", got)
	}
	if got := spm.GetTotalPositionValueAtPrice(50100); math.Abs(got-15030) > 1e-9 {
		t.Fatalf("GetTotalPositionValueAtPrice() = %.4f, want 15030", got)
	}
	if got := spm.GetActiveLayers(); got != 2 {
		t.Fatalf("GetActiveLayers() = %d, want 2", got)
	}
	if got := spm.GetLastMarketPrice(); got != 0 {
		t.Fatalf("GetLastMarketPrice() = %.2f, want 0 before store", got)
	}
	spm.lastMarketPrice.Store(50100.0)
	if got := spm.GetLastMarketPrice(); got != 50100 {
		t.Fatalf("GetLastMarketPrice() = %.2f, want 50100", got)
	}

	deleted := spm.CleanupEmptySlots()
	if deleted != 1 {
		t.Fatalf("CleanupEmptySlots() = %d, want 1", deleted)
	}
	if _, _, ok := spm.findSlotByOrderID(empty.OrderID); ok {
		t.Fatalf("empty slot should not be findable after cleanup")
	}
}

func TestSuperPositionManagerOrderBookOptimizationHelpers(t *testing.T) {
	cfg := &config.Config{}
	cfg.Trading.Symbol = "BTCUSDT"
	cfg.Trading.Direction = "LONG"
	cfg.Trading.MarketType = "futures"
	cfg.Trading.PriceInterval = 100
	cfg.Trading.OrderbookOptimization.Enabled = true
	cfg.Trading.OrderbookOptimization.DepthLevels = 5
	cfg.Trading.OrderbookOptimization.LookbackLevels = 1
	cfg.Trading.OrderbookOptimization.MinDepthUSDT = 100000
	ex := &orderBookExchange{orderBook: &OrderBook{
		Bids: []OrderBookLevel{{Price: 49990, Quantity: 0.5}, {Price: 49980, Quantity: 3}},
		Asks: []OrderBookLevel{{Price: 50010, Quantity: 0.5}, {Price: 50020, Quantity: 3}},
	}}
	spm := NewSuperPositionManager(cfg, &MockExecutor{}, ex, 2, 4)

	if got := spm.calculateNearbyDepth(ex.orderBook.Asks, 2); got != 50010*0.5+50020*3 {
		t.Fatalf("calculateNearbyDepth() = %.2f", got)
	}
	if got := spm.findNextLiquidLevel(49900, ex.orderBook.Asks, 100000, -1, 100); got != 50020-1 {
		t.Fatalf("findNextLiquidLevel(buy) = %.2f, want %.2f", got, 50019.0)
	}
	if got := spm.optimizeBuyPrice(49900, ex.orderBook.Asks, 1, 100000, 100); got != 49910 {
		t.Fatalf("optimizeBuyPrice() = %.2f, want capped up adjustment 49910", got)
	}
	if got := spm.optimizeSellPrice(50100, ex.orderBook.Bids, 1, 100000, 100); got != 50090 {
		t.Fatalf("optimizeSellPrice() = %.2f, want capped down adjustment 50090", got)
	}
	if got := spm.optimizeSinglePrice(49900, ex.orderBook, 100); got != 49910 {
		t.Fatalf("optimizeSinglePrice(buy) = %.2f, want 49910", got)
	}
	optimized := spm.optimizeSlotPricesWithOrderBook(context.Background(), "BTCUSDT", []float64{49900, 50100})
	if len(optimized) != 2 || optimized[0] != 49910 || optimized[1] != 50090 {
		t.Fatalf("optimizeSlotPricesWithOrderBook() = %v", optimized)
	}
	disabled := *cfg
	disabled.Trading.OrderbookOptimization.Enabled = false
	spmDisabled := NewSuperPositionManager(&disabled, &MockExecutor{}, ex, 2, 4)
	original := []float64{49900}
	if got := spmDisabled.optimizeSlotPricesWithOrderBook(context.Background(), "BTCUSDT", original); got[0] != original[0] {
		t.Fatalf("disabled optimization changed prices: %v", got)
	}
}
