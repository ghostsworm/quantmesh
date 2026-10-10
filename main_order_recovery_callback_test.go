package main

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
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

type recoveryCallbackVenue struct {
	*runtimeJournalVenue
	muFills sync.Mutex
	fills   []*exchange.OrderFill
	err     error
}

func (v *recoveryCallbackVenue) GetOrderFills(context.Context, string, int64) ([]*exchange.OrderFill, error) {
	v.muFills.Lock()
	defer v.muFills.Unlock()
	if v.err != nil {
		return nil, v.err
	}
	result := make([]*exchange.OrderFill, len(v.fills))
	copy(result, v.fills)
	return result, nil
}

func (v *recoveryCallbackVenue) setFills(fills ...*exchange.OrderFill) {
	v.muFills.Lock()
	v.fills = append([]*exchange.OrderFill(nil), fills...)
	v.muFills.Unlock()
}

type recoveryCallbackFillWriter struct {
	mu     sync.Mutex
	fills  map[string]*storage.OrderFill
	events *[]string
}

func (w *recoveryCallbackFillWriter) SaveOrderFill(fill *storage.OrderFill) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.fills == nil {
		w.fills = make(map[string]*storage.OrderFill)
	}
	if _, exists := w.fills[fill.TradeID]; exists {
		return nil
	}
	copy := *fill
	w.fills[fill.TradeID] = &copy
	if w.events != nil {
		*w.events = append(*w.events, "fill:"+fill.TradeID)
	}
	return nil
}

func (w *recoveryCallbackFillWriter) snapshot() []*storage.OrderFill {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := make([]*storage.OrderFill, 0, len(w.fills))
	for _, fill := range w.fills {
		copy := *fill
		out = append(out, &copy)
	}
	return out
}

type recoveryAccountingStrategy struct {
	name   string
	writer *recoveryCallbackFillWriter
	events *[]string
	mu     sync.Mutex
	seen   []position.OrderUpdate
	bad    bool
}

func (s *recoveryAccountingStrategy) Name() string { return s.name }
func (*recoveryAccountingStrategy) Initialize(*config.Config, position.OrderExecutorInterface, position.IExchange) error {
	return nil
}
func (*recoveryAccountingStrategy) OnPriceChange(float64) error                 { return nil }
func (s *recoveryAccountingStrategy) OnOrderUpdate(*position.OrderUpdate) error { return nil }
func (s *recoveryAccountingStrategy) GetPositions() []*strategy.Position        { return nil }
func (s *recoveryAccountingStrategy) GetOrders() []*strategy.Order              { return nil }
func (s *recoveryAccountingStrategy) GetStatistics() *strategy.StrategyStatistics {
	return &strategy.StrategyStatistics{}
}
func (*recoveryAccountingStrategy) Start(context.Context) error                  { return nil }
func (*recoveryAccountingStrategy) Stop() error                                  { return nil }
func (*recoveryAccountingStrategy) SetEventBus(strategy.EventBus)                {}
func (*recoveryAccountingStrategy) GetVisualizationData() map[string]interface{} { return nil }

func (s *recoveryAccountingStrategy) OnOrderUpdateWithAccounting(update *position.OrderUpdate) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var total float64
	for _, fill := range s.writer.snapshot() {
		total += fill.Quantity
	}
	if total+1e-12 < update.ExecutedQty {
		s.bad = true
		return false, errors.New("strategy accounting ran before cumulative fills were durable")
	}
	s.seen = append(s.seen, *update)
	if s.events != nil {
		*s.events = append(*s.events, "account:"+update.Status)
	}
	return true, nil
}

func (s *recoveryAccountingStrategy) observations() []position.OrderUpdate {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]position.OrderUpdate(nil), s.seen...)
}

func newRecoveryCallbackRuntime(t *testing.T, venue *recoveryCallbackVenue) (*order.ExchangeOrderExecutor, *execution.OpeningGate, *position.SuperPositionManager, *storage.SQLStorage, execution.IntentScope) {
	t.Helper()
	scope := runtimeJournalScope()
	store, err := storage.NewSQLStorage(filepath.Join(t.TempDir(), "callback-recovery.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.MigrateExecutionIntents(t.Context()); err != nil {
		t.Fatal(err)
	}
	executor, _ := newJournalRuntime(venue, scope)
	if err := executor.ConfigureIntentJournal(t.Context(), store, scope); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{}
	cfg.Trading.Symbol, cfg.Trading.MarketType, cfg.Trading.Direction, cfg.Trading.BotID = scope.Symbol, scope.Market, "LONG", scope.Bot
	cfg.App.CurrentExchange = scope.Exchange
	spm := position.NewSuperPositionManager(cfg, &exchangeExecutorAdapter{executor: executor}, &positionExchangeAdapter{exchange: venue}, 2, 4)
	executor.SetOpeningGate(spm.OpeningGate(), "LONG")
	return executor, spm.OpeningGate(), spm, store, scope
}

func recoveryFill(orderID int64, tradeID string, qty, fee float64) *exchange.OrderFill {
	return &exchange.OrderFill{OrderID: orderID, TradeID: tradeID, Symbol: "BTCUSDT", Side: exchange.SideBuy,
		Price: 100, Quantity: qty, Commission: fee, CommissionAsset: "USDT", TradeTime: 1_790_000_000_000}
}

func awaitCallback(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for production order-update coordinator")
	}
}

func TestProductionOrderUpdateStagesPersistCumulativeFeesBeforeAccounting(t *testing.T) {
	base := &runtimeJournalVenue{}
	venue := &recoveryCallbackVenue{runtimeJournalVenue: base}
	executor, gate, spm, store, scope := newRecoveryCallbackRuntime(t, venue)
	const cid = "callback-fills-first"
	placed, err := executor.PlaceOrder(&order.OrderRequest{Symbol: scope.Symbol, Side: "BUY", Price: 100, Quantity: 1,
		ClientOrderID: cid, StrategyName: "trend", StrategyType: "trend"})
	if err != nil {
		t.Fatal(err)
	}
	var events []string
	writer := &recoveryCallbackFillWriter{events: &events}
	strategyImpl := &recoveryAccountingStrategy{name: "trend", writer: writer, events: &events}
	cfg := &config.Config{}
	cfg.Strategies.Configs = map[string]config.StrategyConfig{"trend": {Enabled: true}}
	manager := strategy.NewStrategyManager(cfg, 1000)
	manager.RegisterStrategy("trend", strategyImpl, 1, 0)
	stages := ownedPositiveOrderStages(venue, writer, scope.Exchange, scope.Market, "scope", scope.Account, scope.Bot, scope.Symbol,
		executor, gate, manager, nil, spm, nil, nil)
	coordinator := newRuntimeOrderUpdateCoordinator()
	const reason = "test_owned_callback_pending"
	updates := []struct {
		qty    float64
		status string
		fills  []*exchange.OrderFill
	}{
		{qty: .5, status: "PARTIALLY_FILLED", fills: []*exchange.OrderFill{recoveryFill(placed.OrderID, "trade-a", .5, .001)}},
		{qty: .75, status: "PARTIALLY_FILLED", fills: []*exchange.OrderFill{recoveryFill(placed.OrderID, "trade-a", .5, .001), recoveryFill(placed.OrderID, "trade-b", .25, .002)}},
	}
	for _, item := range updates {
		venue.setFills(item.fills...)
		update := position.OrderUpdate{OrderID: placed.OrderID, ClientOrderID: cid, Symbol: scope.Symbol, Side: "BUY", Status: item.status, ExecutedQty: item.qty}
		completed := make(chan struct{}, 1)
		if err := coordinator.Submit(t.Context(), update, stages, func() { gate.Block(reason) }, func() {
			gate.Unblock(reason)
			completed <- struct{}{}
		}, func(err error) { t.Errorf("production stage failed: %v", err) }, func() {
			gate.Unblock(reason)
			completed <- struct{}{}
		}); err != nil {
			t.Fatal(err)
		}
		awaitCallback(t, completed)
		if gate.HasBlock(reason) {
			t.Fatal("completed callback left its admission gate blocked")
		}
	}
	observed := strategyImpl.observations()
	if len(observed) != 2 || observed[0].ExecutedQty != .5 || observed[1].ExecutedQty != .75 {
		t.Fatalf("strategy saw unexpected cumulative updates: %+v", observed)
	}
	rows := writer.snapshot()
	var totalQty, totalFees float64
	for _, row := range rows {
		totalQty += row.Quantity
		totalFees += row.Commission
	}
	if len(rows) != 2 || totalQty != .75 || totalFees != .003 {
		t.Fatalf("durable fill/fee ledger = rows:%d qty:%v fees:%v", len(rows), totalQty, totalFees)
	}
	if len(events) < 4 || events[0] != "fill:trade-a" || events[1] != "account:PARTIALLY_FILLED" || events[2] != "fill:trade-b" || events[3] != "account:PARTIALLY_FILLED" {
		t.Fatalf("fills must become durable before each strategy accounting callback: %v", events)
	}
	if strategyImpl.bad {
		t.Fatal("strategy callback observed incomplete durable fills")
	}
	_ = store
}

func TestOrderUpdateCoordinatorSerializesAndRecoversAfterTransientCaptureFailure(t *testing.T) {
	coordinator := newRuntimeOrderUpdateCoordinator()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var mu sync.Mutex
	var orderOfStages []string
	var captureAttempts int
	var settledQty float64
	var settled bool
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	completed := make(chan struct{}, 4)
	failures := make(chan error, 2)
	gate := &execution.OpeningGate{}
	reason := "test_serialized_callback"
	stages := runtimeOrderUpdateStages{
		settledQty: func(position.OrderUpdate) (float64, bool) { return settledQty, settled },
		capture: func(_ context.Context, update position.OrderUpdate) error {
			mu.Lock()
			captureAttempts++
			attempt := captureAttempts
			mu.Unlock()
			if attempt == 1 {
				entered <- struct{}{}
				<-release
				return errors.New("temporary fill-store failure")
			}
			return nil
		},
		account: func(_ context.Context, update position.OrderUpdate) error {
			mu.Lock()
			orderOfStages = append(orderOfStages, "account:"+update.Status)
			mu.Unlock()
			return nil
		},
		settle: func(_ context.Context, update position.OrderUpdate) error {
			mu.Lock()
			orderOfStages = append(orderOfStages, "settle:"+update.Status)
			if terminalOrderUpdate(update.Status) {
				settledQty, settled = update.ExecutedQty, true
			}
			mu.Unlock()
			return nil
		},
	}
	submit := func(update position.OrderUpdate) {
		t.Helper()
		if err := coordinator.Submit(ctx, update, stages, func() { gate.Block(reason) }, func() {
			gate.Unblock(reason)
			completed <- struct{}{}
		}, func(err error) {
			gate.Block(reason)
			failures <- err
		}, func() {
			gate.Unblock(reason)
			completed <- struct{}{}
		}); err != nil {
			t.Fatal(err)
		}
	}
	first := position.OrderUpdate{OrderID: 9401, Status: "PARTIALLY_FILLED", ExecutedQty: .5}
	submit(first)
	awaitCallback(t, entered)
	// These queue behind the blocked capture: .75 must be accounted before stale .4;
	// the duplicate .5 is discarded after the first worker reports its failure.
	submit(position.OrderUpdate{OrderID: 9401, Status: "PARTIALLY_FILLED", ExecutedQty: .75})
	submit(position.OrderUpdate{OrderID: 9401, Status: "PARTIALLY_FILLED", ExecutedQty: .4})
	close(release)
	if err := <-failures; err == nil {
		t.Fatal("injected transient capture failure was not surfaced")
	}
	awaitCallback(t, completed) // stale .4 is skipped
	awaitCallback(t, completed) // .75 succeeds; no permanent quarantine
	if gate.HasBlock(reason) {
		t.Fatal("failure/skip/success callbacks did not release their matching gate")
	}
	mu.Lock()
	got := append([]string(nil), orderOfStages...)
	mu.Unlock()
	if len(got) != 2 || got[0] != "account:PARTIALLY_FILLED" || got[1] != "settle:PARTIALLY_FILLED" {
		t.Fatalf("unexpected serial stage execution after recovery: %v", got)
	}
	// A settled terminal update releases the worker; exact duplicate terminal
	// observations are skipped without reopening accounting.
	terminal := position.OrderUpdate{OrderID: 9401, Status: "FILLED", ExecutedQty: .75}
	submit(terminal)
	awaitCallback(t, completed)
	mu.Lock()
	before := len(orderOfStages)
	mu.Unlock()
	submit(terminal)
	select {
	case <-completed:
		t.Fatal("already-settled duplicate unexpectedly acquired and released an admission gate")
	case <-time.After(20 * time.Millisecond):
	}
	mu.Lock()
	after := len(orderOfStages)
	mu.Unlock()
	if after != before || after != 4 {
		t.Fatalf("terminal callback should be processed once (stage count=%d, before=%d)", after, before)
	}
}

func TestSettledLateFillHoldCannotBeClearedByOlderDuplicate(t *testing.T) {
	coordinator := newRuntimeOrderUpdateCoordinator()
	gate := &execution.OpeningGate{}
	const reason = "owned_order_update_unverified:9402"
	stages := runtimeOrderUpdateStages{
		capture:    func(context.Context, position.OrderUpdate) error { return nil },
		settledQty: func(position.OrderUpdate) (float64, bool) { return 1, true },
	}
	lateFill := position.OrderUpdate{OrderID: 9402, ClientOrderID: "settled-owner", ExecutedQty: 1.25, Status: "FILLED"}
	if err := coordinator.Submit(t.Context(), lateFill, stages, nil, nil, func(error) { gate.Block(reason) }, nil); err == nil {
		t.Fatal("late cumulative fill above durable settled quantity was accepted")
	}
	if !gate.HasBlock(reason) {
		t.Fatal("late fill did not retain the owner-scoped reconciliation hold")
	}
	olderDuplicate := lateFill
	olderDuplicate.ExecutedQty = 1
	if err := coordinator.Submit(t.Context(), olderDuplicate, stages, nil, nil, nil, func() { gate.Unblock(reason) }); err != nil {
		t.Fatal(err)
	}
	if !gate.HasBlock(reason) {
		t.Fatal("older duplicate cleared the hold raised by an unresolved late fill")
	}
}

func TestZeroFillProductionHelperRejectsWrongVenueIdentityBeforeAccounting(t *testing.T) {
	base := &runtimeJournalVenue{}
	venue := &recoveryCallbackVenue{runtimeJournalVenue: base}
	executor, _, spm, _, scope := newRecoveryCallbackRuntime(t, venue)
	const cid = "zero-fill-identity-check"
	placed, err := executor.PlaceOrder(&order.OrderRequest{Symbol: scope.Symbol, Side: "BUY", Price: 100, Quantity: 1,
		ClientOrderID: cid, StrategyName: "trend", StrategyType: "trend"})
	if err != nil {
		t.Fatal(err)
	}
	base.mu.Lock()
	base.liveOrders[placed.OrderID].Status = exchange.OrderStatusCanceled
	base.liveOrders[placed.OrderID].ClientOrderID = "another-bot-order"
	base.mu.Unlock()
	cfg := &config.Config{}
	cfg.Strategies.Configs = map[string]config.StrategyConfig{"trend": {Enabled: true}}
	manager := strategy.NewStrategyManager(cfg, 1000)
	accounting := &recoveryAccountingStrategy{name: "trend", writer: &recoveryCallbackFillWriter{}}
	manager.RegisterStrategy("trend", accounting, 1, 0)
	update := position.OrderUpdate{OrderID: placed.OrderID, ClientOrderID: cid, Symbol: scope.Symbol, Side: "BUY", Status: "CANCELED"}
	if err := processOwnedZeroFillTerminal(t.Context(), update, venue, executor, manager, nil, spm, nil); err == nil {
		t.Fatal("wrong venue client-order identity was accepted as a zero-fill terminal")
	}
	if len(accounting.observations()) != 0 {
		t.Fatal("strategy accounting ran before venue identity was verified")
	}
	if _, _, owned := executor.IntentStrategyType(cid); !owned {
		t.Fatal("rejected identity unexpectedly settled the owned intent")
	}
}
