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

type runtimeGridStateMemoryStore struct {
	version int
	payload string
}

func (s *runtimeGridStateMemoryStore) LoadRuntimeState(string) (int, string, bool, error) {
	return s.version, s.payload, true, nil
}

func (*runtimeGridStateMemoryStore) SaveRuntimeState(string, int, string) error { return nil }

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
	if err := bootstrapRuntimeExposure(t.Context(), executor, gate, venue, store, runtimeJournalScope(), book, spm, nil, false); err != nil {
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
	// Terminal observations release pending exposure before the cancellation
	// worker removes its opening hold. Await both required final conditions.
	for (executor.ExposureSnapshot().PendingQuantity != 0 || spm.IsOpeningPaused()) && time.Now().Before(deadline) {
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

func TestRestoredFuturesInventoryReconcilesGridAndSignalStrategyLots(t *testing.T) {
	venue := []*exchange.Position{{Symbol: "BTCUSDT", Size: 0.75, PositionSide: "LONG"}}
	inventory := []execution.ExposurePosition{
		{Key: "grid:long:100", Group: "grid", Leg: "LONG", Quantity: 0.5, EntryOrderID: 1},
		{Key: "signal/trend/trend-entry", Group: "trend", Leg: "LONG", Quantity: 0.25, EntryOrderID: 2, EntryClientOrderID: "trend-entry"},
	}
	if err := verifyRestoredFuturesInventory(venue, inventory, true, "LONG"); err != nil {
		t.Fatalf("mixed strategy inventory did not reconcile: %v", err)
	}
	inventory[1].Group = "unrecognized"
	if err := verifyRestoredFuturesInventory(venue, inventory, true, "LONG"); err == nil {
		t.Fatal("accepted inventory from a strategy without a verified restore path")
	}
}

func TestReconciledGridInventoryUpdatesPhysicalExposureAndBlocksOverLimit(t *testing.T) {
	v := &runtimeJournalVenue{}
	executor, spm, book, _ := runtimeExposureFixture(t, v)
	spm.SetOpenPositionControl(config.OpenPositionControl{MaxPositionQuantity: 0.5})
	if err := book.ReconcileGroupPositions("dca", []execution.ExposurePosition{
		{Key: "dca:lot", Group: "dca", Leg: "LONG", Quantity: 0.4},
	}); err != nil {
		t.Fatal(err)
	}
	adapter := &exchangeExecutorAdapter{executor: executor}
	if err := adapter.ReconcileExposurePositions([]execution.ExposurePosition{
		{Key: "grid:LONG:1000", Group: "grid", Leg: "LONG", Quantity: 0.75},
	}); err != nil {
		t.Fatal(err)
	}
	if snapshot := executor.ExposureSnapshot(); snapshot.PositionQuantity != 1.15 {
		t.Fatalf("physical exposure quantity = %v, want grid plus preserved DCA inventory 1.15", snapshot.PositionQuantity)
	}
	if !spm.OpeningGate().HasBlock(order.ExposureLimitBlock) {
		t.Fatal("reconciled over-limit inventory did not block further openings")
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
		if err := bootstrapRuntimeExposure(t.Context(), restarted, gate, v, store, runtimeJournalScope(), book, nil, nil, false); err == nil {
			t.Fatal("history declared empty")
		}
		if s := restarted.ExposureSnapshot(); s.Ready {
			t.Fatal("old account seeded as flat")
		}
	}
}

func TestRuntimeExposureBootstrapSeedsOnlyExactlyReconciledRestoredFuturesInventory(t *testing.T) {
	for _, test := range []struct {
		name              string
		venueQty          float64
		wantPosition      float64
		wantCloseOK       bool
		persistOwnerOrder bool
		localOrderStatus  string
	}{
		{name: "matching restored owner", venueQty: 0.25, wantPosition: 0.25, wantCloseOK: true, persistOwnerOrder: true, localOrderStatus: "NOT_PLACED"},
		{name: "venue quantity mismatch", venueQty: 0.3, persistOwnerOrder: true, localOrderStatus: "NOT_PLACED"},
		{name: "missing durable entry-order evidence", venueQty: 0.25, localOrderStatus: "NOT_PLACED"},
		{name: "unresolved restored slot order", venueQty: 0.25, persistOwnerOrder: true, localOrderStatus: "UNKNOWN"},
	} {
		t.Run(test.name, func(t *testing.T) {
			venue := &runtimeJournalVenue{}
			scope := runtimeJournalScope()
			scope.Bot = config.GenerateBotID("fake", scope.Symbol, scope.Market)
			store, err := storage.NewSQLStorage(filepath.Join(t.TempDir(), "restored-exposure.db"))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = store.Close() })
			if err := store.MigrateExecutionIntents(t.Context()); err != nil {
				t.Fatal(err)
			}
			journalWriter, _ := newJournalRuntime(venue, scope)
			if err := journalWriter.ConfigureIntentJournal(t.Context(), store, scope); err != nil {
				t.Fatal(err)
			}
			const entryClientOrderID = "restored-entry"
			entry, err := journalWriter.PlaceOrder(&order.OrderRequest{Symbol: scope.Symbol, Side: "BUY", Price: 99, Quantity: 0.25, ClientOrderID: entryClientOrderID, StrategyType: "grid"})
			if err != nil {
				t.Fatal(err)
			}
			venue.mu.Lock()
			venue.liveOrders[entry.OrderID].Status = exchange.OrderStatusFilled
			venue.liveOrders[entry.OrderID].ExecutedQty = 0.25
			venue.liveOrders[entry.OrderID].AvgPrice = 99
			venue.mu.Unlock()
			if !journalWriter.ObserveOrder(&exchange.Order{OrderID: entry.OrderID, ClientOrderID: entryClientOrderID, Symbol: scope.Symbol,
				Side: exchange.SideBuy, Quantity: 0.25, ExecutedQty: 0.25, AvgPrice: 99, Status: exchange.OrderStatusFilled}) {
				t.Fatal("entry order fill was not recorded")
			}
			if err := journalWriter.SettleIntent(t.Context(), entryClientOrderID); err != nil {
				t.Fatal(err)
			}
			if test.persistOwnerOrder {
				if err := store.SaveOrder(&storage.Order{OrderID: entry.OrderID, BotID: scope.Bot, Account: scope.Bot, MarketType: scope.Market,
					AccountScope: scope.Account, ClientOrderID: entryClientOrderID, Symbol: scope.Symbol, Side: "BUY", Exchange: scope.Exchange,
					Price: 99, Quantity: 0.25, FilledQty: 0.25, Status: "FILLED", CreatedAt: time.Now(), UpdatedAt: time.Now()}); err != nil {
					t.Fatal(err)
				}
			}
			venue.positions = []*exchange.Position{{Symbol: scope.Symbol, Size: test.venueQty}}
			executor, gate := newJournalRuntime(venue, scope)
			book, err := configureRuntimeExposure(executor, func() (float64, time.Time) { return 100, time.Now() })
			if err != nil {
				t.Fatal(err)
			}
			cfg := &config.Config{}
			cfg.Trading.Symbol, cfg.Trading.MarketType, cfg.Trading.Direction = scope.Symbol, scope.Market, "LONG"
			cfg.Trading.BotID, cfg.App.CurrentExchange = scope.Bot, scope.Exchange
			spm := position.NewSuperPositionManager(cfg, &exchangeExecutorAdapter{executor: executor}, &positionExchangeAdapter{exchange: venue}, 2, 4)
			executor.SetOpeningGate(spm.OpeningGate(), "LONG")
			gate = spm.OpeningGate()
			state, err := json.Marshal(map[string]any{
				"version": 4, "bot_id": scope.Bot, "exchange": scope.Exchange, "market_type": scope.Market,
				"symbol": scope.Symbol, "direction": "LONG", "anchor_price": 100, "last_market_price": 100,
				"slots": []map[string]any{{"price": 100, "position_status": "FILLED", "position_qty": 0.25,
					"slot_status": "FREE", "order_status": test.localOrderStatus, "avg_buy_price": 99,
					"position_entry_order_id": entry.OrderID, "position_leg": "LONG"}},
			})
			if err != nil {
				t.Fatal(err)
			}
			spm.SetGridRuntimeStateStore(&runtimeGridStateMemoryStore{version: 4, payload: string(state)})
			if restored, err := spm.RestoreGridRuntimeState(); err != nil || !restored {
				t.Fatalf("restore owner state: restored=%t err=%v", restored, err)
			}
			gate.Block("grid_runtime_state_reconciliation")
			err = bootstrapRuntimeExposure(t.Context(), executor, gate, venue, store, scope, book, spm, nil, false)
			if !test.wantCloseOK {
				if err == nil || book.Snapshot(time.Now()).PositionQuantity != 0 {
					t.Fatalf("mismatched venue state was accepted: err=%v snapshot=%+v", err, book.Snapshot(time.Now()))
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if snapshot := book.Snapshot(time.Now()); snapshot.PositionQuantity != test.wantPosition {
				t.Fatalf("restored exposure was not seeded: %+v", snapshot)
			}
			if _, err := executor.PlaceOrder(&order.OrderRequest{Symbol: scope.Symbol, Side: "BUY", Price: 100, Quantity: 0.01, ClientOrderID: "restore-open"}); !errors.Is(err, execution.ErrOpeningPaused) {
				t.Fatalf("restored non-empty owner opened before full strategy reconciliation: %v", err)
			}
			if _, err := executor.PlaceOrder(&order.OrderRequest{Symbol: scope.Symbol, Side: "SELL", Price: 99, Quantity: 0.25,
				PositionSide: "LONG", ExposureKey: "grid:LONG:100", ReduceOnly: true, ClientOrderID: "verified-close"}); err != nil {
				t.Fatalf("matched restored owner could not submit a reducing close: %v", err)
			}
		})
	}
}

func TestRuntimeExposureBootstrapRetriesAfterStrategyRecoverySettlesIntent(t *testing.T) {
	venue := &runtimeJournalVenue{}
	scope := runtimeJournalScope()
	store, err := storage.NewSQLStorage(filepath.Join(t.TempDir(), "recovered-intent.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.MigrateExecutionIntents(t.Context()); err != nil {
		t.Fatal(err)
	}

	writer, _ := newJournalRuntime(venue, scope)
	if err := writer.ConfigureIntentJournal(t.Context(), store, scope); err != nil {
		t.Fatal(err)
	}
	const clientOrderID = "recovered-grid-fill"
	placed, err := writer.PlaceOrder(&order.OrderRequest{Symbol: scope.Symbol, Side: "BUY", Price: 99, Quantity: 0.25,
		ClientOrderID: clientOrderID, StrategyName: "grid", StrategyType: "grid"})
	if err != nil {
		t.Fatal(err)
	}

	venue.mu.Lock()
	venue.liveOrders[placed.OrderID].Status = exchange.OrderStatusFilled
	venue.liveOrders[placed.OrderID].ExecutedQty = 0.25
	venue.liveOrders[placed.OrderID].AvgPrice = 99
	venue.positions = []*exchange.Position{{Symbol: scope.Symbol, Size: 0.25, PositionSide: "LONG"}}
	venue.mu.Unlock()

	executor, gate := newJournalRuntime(venue, scope)
	book, err := configureRuntimeExposure(executor, func() (float64, time.Time) { return 100, time.Now() })
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{}
	cfg.Trading.Symbol, cfg.Trading.MarketType, cfg.Trading.Direction, cfg.Trading.BotID = scope.Symbol, scope.Market, "LONG", scope.Bot
	cfg.App.CurrentExchange = scope.Exchange
	spm := position.NewSuperPositionManager(cfg, &exchangeExecutorAdapter{executor: executor}, &positionExchangeAdapter{exchange: venue}, 2, 4)
	executor.SetOpeningGate(spm.OpeningGate(), "LONG")
	gate = spm.OpeningGate()
	state, err := json.Marshal(map[string]any{
		"version": 4, "bot_id": scope.Bot, "exchange": scope.Exchange, "market_type": scope.Market,
		"symbol": scope.Symbol, "direction": "LONG", "anchor_price": 100, "last_market_price": 100,
		"slots": []map[string]any{{"price": 100, "position_status": "FILLED", "position_qty": 0.25,
			"slot_status": "FREE", "order_status": "NOT_PLACED", "avg_buy_price": 99,
			"position_entry_order_id": placed.OrderID, "position_leg": "LONG"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	spm.SetGridRuntimeStateStore(&runtimeGridStateMemoryStore{version: 4, payload: string(state)})
	if restored, err := spm.RestoreGridRuntimeState(); err != nil || !restored {
		t.Fatalf("restore grid owner state: restored=%v err=%v", restored, err)
	}
	gate.Block("grid_runtime_state_reconciliation")

	if err := bootstrapRuntimeExposure(t.Context(), executor, gate, venue, store, scope, book, spm, nil, false); !errors.Is(err, execution.ErrOrderUnknown) {
		t.Fatalf("initial bootstrap error = %v, want unresolved-intent hold", err)
	}
	if book.Snapshot(time.Now()).Ready || !gate.HasBlock(runtimeExposureBootstrapBlock) {
		t.Fatal("unresolved intent was admitted before strategy recovery")
	}

	if err := store.SaveOrder(&storage.Order{OrderID: placed.OrderID, BotID: scope.Bot, Account: scope.Bot, AccountScope: scope.Account,
		MarketType: scope.Market, ClientOrderID: clientOrderID, Symbol: scope.Symbol, Side: "BUY", Exchange: scope.Exchange,
		Price: 99, Quantity: 0.25, FilledQty: 0.25, Status: "FILLED", CreatedAt: time.Now(), UpdatedAt: time.Now()}); err != nil {
		t.Fatal("persist recovered execution row:", err)
	}
	allocator := strategy.NewCapitalAllocator(cfg, 1000)
	allocator.RegisterStrategy("grid", 1, 0)
	allocator.Allocate()
	multiExecutor := strategy.NewMultiStrategyExecutor(executor, allocator)
	wrongOwner := strategy.NewMultiStrategyExecutorAdapter(multiExecutor, "dca")
	if err := wrongOwner.SettleRecoveredIntent(t.Context(), clientOrderID); err == nil {
		t.Fatal("strategy adapter settled an intent owned by another strategy")
	}
	gridOwner := strategy.NewMultiStrategyExecutorAdapter(multiExecutor, "grid")
	if err := gridOwner.SettleRecoveredIntent(t.Context(), clientOrderID); err != nil {
		t.Fatal("settle strategy-accounted recovered intent:", err)
	}

	if err := retryRuntimeExposureBootstrapAfterStrategyRecovery(t.Context(), *cfg, venue, &positionExchangeAdapter{exchange: venue}, nil,
		scope.Bot, executor, gate, store, scope, book, spm); err != nil {
		t.Fatalf("post-strategy recovery bootstrap: %v", err)
	}
	markAt := time.Now()
	if err := book.ObserveMark(100, markAt, markAt); err != nil {
		t.Fatalf("record fresh exposure mark: %v", err)
	}
	if snapshot := book.Snapshot(time.Now()); !snapshot.Ready || snapshot.PositionQuantity != 0.25 {
		t.Fatalf("recovered physical exposure was not seeded: %+v", snapshot)
	}
	if gate.HasBlock(runtimeExposureBootstrapBlock) || gate.HasBlock("grid_runtime_state_reconciliation") {
		t.Fatalf("fully verified recovery retained its owned startup holds: %v", gate.Sources())
	}
}

func TestRuntimeExposureBootstrapRequiresAuthoritativeEmptyAccount(t *testing.T) {
	tests := []struct {
		name         string
		positions    []*exchange.Position
		orders       []*exchange.Order
		positionsNil bool
		ordersNil    bool
		positionErr  error
		orderErr     error
	}{
		{name: "position query failure", positionErr: errors.New("unavailable")},
		{name: "order query failure", orderErr: errors.New("unavailable")},
		{name: "nil position collection", positionsNil: true},
		{name: "nil order collection", ordersNil: true},
		{name: "nil position entry", positions: []*exchange.Position{nil}},
		{name: "position without symbol", positions: []*exchange.Position{{Size: 0}}},
		{name: "position query returned another symbol", positions: []*exchange.Position{{Symbol: "ETHUSDT", Size: 0}}},
		{name: "existing position", positions: []*exchange.Position{{Symbol: "BTCUSDT", Size: 0.01}}},
		{name: "existing order", orders: []*exchange.Order{{Symbol: "BTCUSDT", Status: exchange.OrderStatusNew}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			v := &runtimeJournalVenue{positions: test.positions, orders: test.orders,
				positionsNil: test.positionsNil, ordersNil: test.ordersNil,
				positionErr: test.positionErr, orderErr: test.orderErr}
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
			if err := bootstrapRuntimeExposure(t.Context(), executor, gate, v, store, runtimeJournalScope(), book, nil, nil, false); err == nil {
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
