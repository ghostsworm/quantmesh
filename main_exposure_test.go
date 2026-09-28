package main

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"path/filepath"
	"testing"
	"time"

	"quantmesh/config"
	"quantmesh/exchange"
	"quantmesh/execution"
	"quantmesh/order"
	"quantmesh/position"
	"quantmesh/storage"
	"quantmesh/strategy"
)

func (v *runtimeJournalVenue) GetAccount(context.Context) (*exchange.Account, error) {
	return &exchange.Account{}, nil
}

func (v *runtimeJournalVenue) EstimateFinalOrderAmount(_ string, price, qty float64, _ bool) float64 {
	return price * qty
}

func runtimeExposureFixture(t *testing.T, venue *runtimeJournalVenue) (*order.ExchangeOrderExecutor, *position.SuperPositionManager, *execution.ExposureBook, *storage.SQLStorage) {
	t.Helper()
	executor, gate := newJournalRuntime(venue, runtimeJournalScope())
	book, err := configureRuntimeExposure(executor, func() (float64, time.Time) { return 100, time.Now() })
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{}
	cfg.Trading.Symbol, cfg.Trading.MarketType, cfg.Trading.Direction = "BTCUSDT", "futures", "LONG"
	spm := position.NewSuperPositionManager(cfg, &exchangeExecutorAdapter{executor: executor}, &positionExchangeAdapter{exchange: venue}, 2, 4)
	executor.SetOpeningGate(spm.OpeningGate(), "LONG")
	gate = spm.OpeningGate()
	store, err := storage.NewSQLStorage(filepath.Join(t.TempDir(), "exposure.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.MigrateExecutionIntents(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := bootstrapRuntimeExposure(t.Context(), executor, gate, venue, store, runtimeJournalScope(), book); err != nil {
		t.Fatal(err)
	}
	return executor, spm, book, store
}

func TestRuntimeExposureSharedGridStrategyAndHotLimits(t *testing.T) {
	v := &runtimeJournalVenue{}
	executor, spm, _, _ := runtimeExposureFixture(t, v)
	spm.SetOpenPositionControl(config.OpenPositionControl{MaxPositionQuantity: 1, MaxPositionLayers: 1})
	grid := &exchangeExecutorAdapter{executor: executor}
	if _, err := grid.PlaceOrder(&position.OrderRequest{Symbol: "BTCUSDT", Side: "BUY", Price: 100, Quantity: 1, ClientOrderID: "grid", ExposureKey: "grid:LONG:100"}); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{}
	allocator := strategy.NewCapitalAllocator(cfg, 1000)
	allocator.RegisterStrategy("dca", 1, 0)
	allocator.Allocate()
	multi := strategy.NewMultiStrategyExecutor(executor, allocator)
	adapter := strategy.NewMultiStrategyExecutorAdapter(multi, "dca")
	req := &position.OrderRequest{Symbol: "BTCUSDT", Side: "BUY", PositionSide: "LONG", Price: 100, Quantity: .1, ClientOrderID: "dca"}
	req.BotWideClose = true
	if _, err := adapter.PlaceOrder(req); err == nil {
		t.Fatal("strategy acquired owner-wide close authority")
	}
	req.BotWideClose = false
	if _, err := adapter.PlaceOrder(req); !errors.Is(err, execution.ErrExposureLimit) {
		t.Fatalf("strategy bypassed pending grid: %v", err)
	}
	if v.sends != 1 || allocator.GetAvailable("dca") != 1000 {
		t.Fatal("rejected strategy sent or leaked capital")
	}
	spm.SetRiskControls(config.RiskControls{Open: config.OpenPositionControl{BotRiskControl: &config.BotRiskControl{Enabled: true, MaxPositionQuantity: 2, MaxPositionLayers: 2}}})
	if _, err := adapter.PlaceOrder(req); err != nil {
		t.Fatal(err)
	}
	if s := executor.ExposureSnapshot(); s.ProjectedQuantity != 1.1 || s.Limits.Quantity != 2 || s.Layers != 2 {
		t.Fatalf("wrong physical revision: %+v", s)
	}
	spm.SetOpenPositionControl(config.OpenPositionControl{MaxPositionQuantity: .5})
	deadline := time.Now().Add(3 * time.Second)
	for executor.ExposureSnapshot().PendingQuantity != 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if snapshot := executor.ExposureSnapshot(); snapshot.PendingQuantity != 0 || spm.IsOpeningPaused() {
		t.Fatalf("limit reduction did not reconcile owned pending orders: %+v", snapshot)
	}
	req.ClientOrderID = "over-lowered"
	if _, err := adapter.PlaceOrder(req); err != nil {
		t.Fatalf("verified cancellation should free quota within the new limit: %v", err)
	}
	br := &BotRuntime{Inner: &SymbolRuntime{SuperPositionManager: spm, ExchangeExecutor: executor}}
	if br.GetPositionStatus()["execution_exposure"] == nil {
		t.Fatal("pending exposure missing from status")
	}
	if br.GetPositionStatus()["should_stop_opening"] != false {
		t.Fatal("verified under-limit exposure was reported as blocked")
	}
}

func TestOpeningControllerLimitUpdatesCannotExceedVerifiedCapitalCeiling(t *testing.T) {
	v := &runtimeJournalVenue{}
	executor, spm, _, _ := runtimeExposureFixture(t, v)
	if err := spm.SetVerifiedCapitalLimit(500); err != nil {
		t.Fatal(err)
	}
	spm.SetOpenPositionControl(config.OpenPositionControl{MaxPositionValue: 900})
	if got := spm.GetRiskControls().Open.MaxPositionValue; got != 500 {
		t.Fatalf("published opening-control limit = %v, want verified ceiling 500", got)
	}
	_, err := executor.PlaceOrder(&order.OrderRequest{
		Symbol: "BTCUSDT", Side: "BUY", Price: 100, Quantity: 6, ClientOrderID: "opening-control-over-budget",
	})
	if !errors.Is(err, execution.ErrExposureLimit) || v.sends != 0 {
		t.Fatalf("direct opening-control update bypassed verified capital ceiling: sends=%d err=%v", v.sends, err)
	}
}

func TestBotCapitalNotionalLimitIsSharedByGridAndStrategyOrders(t *testing.T) {
	v := &runtimeJournalVenue{}
	executor, spm, _, _ := runtimeExposureFixture(t, v)
	budget, err := capStrategyCapitalLimit(5000, 500)
	if err != nil {
		t.Fatal(err)
	}
	openControl := config.OpenPositionControl{}
	if err := applyBotCapitalLimit(&openControl, budget); err != nil {
		t.Fatal(err)
	}
	spm.SetRiskControls(config.RiskControls{Open: openControl})

	grid := &exchangeExecutorAdapter{executor: executor}
	if _, err := grid.PlaceOrder(&position.OrderRequest{
		Symbol: "BTCUSDT", Side: "BUY", Price: 100, Quantity: 4, ClientOrderID: "budget-grid", ExposureKey: "grid:LONG:100",
	}); err != nil {
		t.Fatalf("grid order within Bot budget rejected: %v", err)
	}

	allocator := strategy.NewCapitalAllocator(&config.Config{}, budget)
	allocator.RegisterStrategy("dca", 1, 0)
	allocator.Allocate()
	strategyExecutor := strategy.NewMultiStrategyExecutor(executor, allocator)
	strategyAdapter := strategy.NewMultiStrategyExecutorAdapter(strategyExecutor, "dca")
	if _, err := strategyAdapter.PlaceOrder(&position.OrderRequest{
		Symbol: "BTCUSDT", Side: "BUY", PositionSide: "LONG", Price: 100, Quantity: 2, ClientOrderID: "budget-dca",
	}); !errors.Is(err, execution.ErrExposureLimit) {
		t.Fatalf("strategy order exceeded shared Bot budget: %v", err)
	}
	if v.sends != 1 {
		t.Fatalf("over-budget strategy request reached venue: sends=%d", v.sends)
	}
}

func TestRuntimeExposureUsesQuoteEvidenceAtAdmission(t *testing.T) {
	v := &runtimeJournalVenue{}
	executor, _, _, _ := runtimeExposureFixture(t, v)
	price, at := 100.0, time.Now()
	executor.SetExposureMarkProvider(func() (float64, time.Time) { return price, at })
	for n, bad := range []float64{math.NaN(), 0, math.Inf(1)} {
		price, at = bad, time.Now()
		req := &order.OrderRequest{Symbol: "BTCUSDT", Side: "BUY", Price: 100, Quantity: 1, ClientOrderID: string(rune('a' + n))}
		if _, err := executor.PlaceOrder(req); !errors.Is(err, execution.ErrExposureUnverified) {
			t.Fatalf("invalid quote admitted: %v", err)
		}
	}
	price, at = 100, time.Now().Add(-3*time.Minute)
	if s := executor.ExposureSnapshot(); s.Ready {
		t.Fatal("stale quote accepted")
	}
	price, at = 100, time.Now()
	if _, err := executor.PlaceOrder(&order.OrderRequest{Symbol: "BTCUSDT", Side: "BUY", Price: 100, Quantity: 1, ClientOrderID: "good"}); err != nil {
		t.Fatal(err)
	}
	if v.sends != 1 {
		t.Fatalf("invalid evidence reached venue: %d", v.sends)
	}
}

func TestRuntimeExposureBootstrapCannotSeedExistingAccount(t *testing.T) {
	for _, history := range []bool{false, true} {
		v := &runtimeJournalVenue{}
		executor, _, _, store := runtimeExposureFixture(t, v)
		if history {
			if _, err := executor.PlaceOrder(&order.OrderRequest{Symbol: "BTCUSDT", Side: "BUY", Price: 100, Quantity: 1, ClientOrderID: "old"}); err != nil {
				t.Fatal(err)
			}
		} else {
			v.positions = []*exchange.Position{{Symbol: "BTCUSDT", Size: 1}}
		}
		restarted, gate := newJournalRuntime(v, runtimeJournalScope())
		book, err := configureRuntimeExposure(restarted, func() (float64, time.Time) { return 100, time.Now() })
		if err != nil {
			t.Fatal(err)
		}
		if err := bootstrapRuntimeExposure(t.Context(), restarted, gate, v, store, runtimeJournalScope(), book); err == nil {
			t.Fatal("history declared empty")
		}
		if s := restarted.ExposureSnapshot(); s.Ready {
			t.Fatal("old account seeded as flat")
		}
	}
}

func TestRuntimeExposureBootstrapRequiresAuthoritativeEmptyAccount(t *testing.T) {
	tests := []struct {
		name        string
		positions   []*exchange.Position
		orders      []*exchange.Order
		positionErr error
		orderErr    error
	}{
		{name: "position query failure", positionErr: errors.New("unavailable")},
		{name: "order query failure", orderErr: errors.New("unavailable")},
		{name: "nil position entry", positions: []*exchange.Position{nil}},
		{name: "existing position", positions: []*exchange.Position{{Symbol: "BTCUSDT", Size: 0.01}}},
		{name: "existing order", orders: []*exchange.Order{{Symbol: "BTCUSDT", Status: exchange.OrderStatusNew}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			v := &runtimeJournalVenue{positions: test.positions, orders: test.orders, positionErr: test.positionErr, orderErr: test.orderErr}
			executor, gate := newJournalRuntime(v, runtimeJournalScope())
			book, err := configureRuntimeExposure(executor, func() (float64, time.Time) { return 100, time.Now() })
			if err != nil {
				t.Fatal(err)
			}
			store, err := storage.NewSQLStorage(filepath.Join(t.TempDir(), "exposure.db"))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = store.Close() })
			if err := store.MigrateExecutionIntents(t.Context()); err != nil {
				t.Fatal(err)
			}
			if err := bootstrapRuntimeExposure(t.Context(), executor, gate, v, store, runtimeJournalScope(), book); err == nil {
				t.Fatal("unverified or non-empty account seeded as flat")
			}
			if !gate.HasBlock(runtimeExposureBootstrapBlock) {
				t.Fatal("failed startup reconciliation did not retain the opening gate")
			}
			if snapshot := executor.ExposureSnapshot(); snapshot == nil || snapshot.Ready {
				t.Fatalf("startup uncertainty did not keep opening blocked: %+v", snapshot)
			}
		})
	}
}

func TestRuntimeExposureManagedManualCloseUsesOwnedInventory(t *testing.T) {
	v := &runtimeJournalVenue{}
	executor, _, _, store := runtimeExposureFixture(t, v)
	for n, group := range []string{"grid", "dca"} {
		id := group + "-open"
		if _, err := executor.PlaceOrder(&order.OrderRequest{Symbol: "BTCUSDT", Side: "BUY", Price: 100, Quantity: 1, ClientOrderID: id, StrategyName: group}); err != nil {
			t.Fatal(err)
		}
		if !executor.ObserveOrder(&exchange.Order{Symbol: "BTCUSDT", Side: exchange.SideBuy, OrderID: int64(n + 1), ClientOrderID: id, Status: exchange.OrderStatusFilled, Quantity: 1, ExecutedQty: 1, AvgPrice: 100}) {
			t.Fatal("fill not recorded")
		}
	}
	w := position.NewOwnedExchangeAdapterWrapper(v, &exchangeExecutorAdapter{executor: executor}, executor.ObserveOrder)
	close, err := w.PlaceOrder(t.Context(), &position.ExchangeOrderRequest{Symbol: "BTCUSDT", Side: "SELL", Type: "MARKET", Quantity: 2, ReduceOnly: true, ClientOrderID: "managed"})
	if err != nil {
		t.Fatal(err)
	}
	if !executor.ObserveOrder(&exchange.Order{Symbol: "BTCUSDT", Side: exchange.SideSell, OrderID: close.OrderID, ClientOrderID: close.ClientOrderID, Status: exchange.OrderStatusFilled, Quantity: 2, ExecutedQty: 2, AvgPrice: 100}) {
		t.Fatal("close not recorded")
	}
	if s := executor.ExposureSnapshot(); s.PositionQuantity != 0 || !s.Ready {
		t.Fatalf("manual close not allocated: %+v", s)
	}
	key, err := runtimeJournalScope().Key()
	if err != nil {
		t.Fatal(err)
	}
	rows, err := store.LoadExecutionIntents(t.Context(), key, 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, row := range rows {
		if row.ClientOrderID != "managed" {
			continue
		}
		var payload struct{ Request order.OrderRequest }
		if err := json.Unmarshal(row.Payload, &payload); err != nil {
			t.Fatal(err)
		}
		found = payload.Request.BotWideClose && payload.Request.StrategyName == "manual_close"
	}
	if !found {
		t.Fatal("owner-wide close authority lost from durable intent")
	}
}

func TestRuntimeExposureNotionalRevaluesBeforePhysicalSubmission(t *testing.T) {
	v := &runtimeJournalVenue{}
	executor, spm, _, _ := runtimeExposureFixture(t, v)
	price := 100.0
	executor.SetExposureMarkProvider(func() (float64, time.Time) { return price, time.Now() })
	spm.SetOpenPositionControl(config.OpenPositionControl{MaxPositionValue: 150})
	if _, err := executor.PlaceOrder(&order.OrderRequest{Symbol: "BTCUSDT", Side: "BUY", Price: 100, Quantity: 1, ClientOrderID: "first"}); err != nil {
		t.Fatal(err)
	}
	if !executor.ObserveOrder(&exchange.Order{Symbol: "BTCUSDT", Side: exchange.SideBuy, OrderID: 1, ClientOrderID: "first", Quantity: 1, ExecutedQty: 1, AvgPrice: 100, Status: exchange.OrderStatusFilled}) {
		t.Fatal("fill missing")
	}
	price = 200
	if _, err := executor.PlaceOrder(&order.OrderRequest{Symbol: "BTCUSDT", Side: "BUY", Price: 100, Quantity: .1, ClientOrderID: "over-mark"}); !errors.Is(err, execution.ErrExposureLimit) {
		t.Fatalf("old quote used: %v", err)
	}
	if v.sends != 1 || executor.ExposureSnapshot().ProjectedNotional != 200 {
		t.Fatal("marked exposure incorrect")
	}
	// Missing quote does not prevent a verified strategy-owned reduction.
	price = math.NaN()
	if _, err := executor.PlaceOrder(&order.OrderRequest{Symbol: "BTCUSDT", Side: "SELL", Type: "MARKET", Quantity: 1, ReduceOnly: true, ClientOrderID: "protect"}); err != nil {
		t.Fatal(err)
	}
}

func TestRuntimeExposureExternalMutationCannotBeClearedByConfiguration(t *testing.T) {
	v := &runtimeJournalVenue{}
	executor, spm, book, _ := runtimeExposureFixture(t, v)
	executor.InvalidateExposure("account close outside owned journal")
	spm.SetOpenPositionControl(config.OpenPositionControl{})
	if err := book.Seed(nil); err == nil {
		t.Fatal("empty seed reset external mutation")
	}
	if _, err := executor.PlaceOrder(&order.OrderRequest{Symbol: "BTCUSDT", Side: "BUY", Price: 100, Quantity: 1, ClientOrderID: "unsafe"}); !errors.Is(err, execution.ErrExposureUnverified) {
		t.Fatalf("hot update cleared reconciliation: %v", err)
	}
	if v.sends != 0 {
		t.Fatal("unverified inventory admitted")
	}
}
