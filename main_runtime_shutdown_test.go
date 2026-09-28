package main

import (
	"context"
	"errors"
	"math"
	"path/filepath"
	"testing"
	"time"

	"quantmesh/config"
	"quantmesh/event"
	"quantmesh/exchange"
	"quantmesh/execution"
	"quantmesh/lock"
	"quantmesh/order"
	"quantmesh/position"
	"quantmesh/storage"
	"quantmesh/utils"
)

type shutdownBarrierVenue struct {
	*fakeCloseExchange
	beforeQuery func()
	ackOnly     bool
}

func (v *shutdownBarrierVenue) GetOrder(ctx context.Context, symbol string, id int64) (*exchange.Order, error) {
	if v.beforeQuery != nil {
		v.beforeQuery()
	}
	return v.fakeCloseExchange.GetOrder(ctx, symbol, id)
}

func (v *shutdownBarrierVenue) CancelOrder(ctx context.Context, symbol string, id int64) error {
	if v.ackOnly {
		return nil
	}
	return v.fakeCloseExchange.CancelOrder(ctx, symbol, id)
}

func newShutdownRuntime(venue exchange.IExchange, bot, account string) *SymbolRuntime {
	cfg := &config.Config{}
	cfg.Trading.Symbol, cfg.Trading.MarketType, cfg.Trading.Direction = "BTCUSDT", "futures", "LONG"
	executor := order.NewExchangeOrderExecutor(venue, "BTCUSDT", 0, 0, lock.NewNopLock(), bot)
	adapter := &exchangeExecutorAdapter{executor: executor, exchange: "fake"}
	spm := position.NewSuperPositionManager(cfg, adapter, &positionExchangeAdapter{exchange: venue}, 2, 4)
	executor.SetOpeningGate(spm.OpeningGate(), "LONG")
	return &SymbolRuntime{Config: config.SymbolConfig{ID: bot, Exchange: "fake", Symbol: "BTCUSDT", MarketType: "futures"},
		Exchange: venue, ExchangeExecutor: executor, SuperPositionManager: spm, AccountScope: account, AccountMarketType: "futures"}
}

func seedShutdownOpening(t *testing.T, rt *SymbolRuntime, cid string) {
	t.Helper()
	if _, err := rt.ExchangeExecutor.PlaceOrder(&order.OrderRequest{Symbol: "BTCUSDT", Side: "BUY", Quantity: 1, Price: 90, ClientOrderID: cid}); err != nil {
		t.Fatal(err)
	}
}

func TestProcessShutdownSealsEveryRuntimeBeforeFirstQuery(t *testing.T) {
	v := &shutdownBarrierVenue{fakeCloseExchange: newFakeCloseExchange(100, 0)}
	a, b := newShutdownRuntime(v, "a", "account"), newShutdownRuntime(v, "b", "account")
	seedShutdownOpening(t, a, "a-open")
	seedShutdownOpening(t, b, "b-open")
	v.orders[999] = &exchange.Order{OrderID: 999, Symbol: "BTCUSDT", ClientOrderID: "manual", Status: exchange.OrderStatusNew}
	v.beforeQuery = func() {
		for _, rt := range []*SymbolRuntime{a, b} {
			for _, closing := range []bool{false, true} {
				req := &order.OrderRequest{Symbol: "BTCUSDT", Side: "BUY", Quantity: 1, Price: 80, ClientOrderID: "late"}
				if closing {
					req.Side, req.ReduceOnly = "SELL", true
				}
				_, err := rt.ExchangeExecutor.PlaceOrder(req)
				if !errors.Is(err, execution.ErrOpeningPaused) && !errors.Is(err, order.ErrRuntimeStopping) {
					t.Fatalf("live tick escaped shutdown: %v", err)
				}
			}
		}
	}
	if err := prepareProcessShutdown(t.Context(), []*SymbolRuntime{a, b}, true); err != nil {
		t.Fatal(err)
	}
	if len(v.placed) != 2 || v.orders[999].Status != exchange.OrderStatusNew {
		t.Fatal("late submission or foreign cancellation")
	}
	for _, rt := range []*SymbolRuntime{a, b} {
		if rt.shutdownCloseUnverifiedReason() != "" || !rt.SuperPositionManager.OpeningGate().HasBlock(order.RuntimeShutdownBlock) {
			t.Fatal("incorrect shutdown state")
		}
	}
}

func TestSpecialRuntimeUsesOwnedShutdownHooksAndVerifiesFlatness(t *testing.T) {
	venue := &shutdownBarrierVenue{fakeCloseExchange: newFakeCloseExchange(100, 1)}
	rt := &SymbolRuntime{Config: config.SymbolConfig{ID: "carry", Exchange: "fake", Symbol: "BTCUSDT"},
		Exchange: venue, AccountScope: "account", AccountMarketType: config.MarketTypeFundingCarry,
		OpeningGate: &execution.OpeningGate{}}
	prepared, closed := false, false
	rt.PrepareShutdown = func(_ context.Context, cancel bool) error {
		prepared = true
		if !cancel {
			t.Fatal("expected owned order cancellation policy")
		}
		return nil
	}
	rt.CloseForShutdown = func(context.Context) error {
		closed = true
		venue.position = 0
		return nil
	}
	if err := prepareProcessShutdown(t.Context(), []*SymbolRuntime{rt}, true); err != nil || !prepared {
		t.Fatalf("special shutdown prepare failed: prepared=%v err=%v", prepared, err)
	}
	if err := closeProcessRuntimeGroup(t.Context(), []*SymbolRuntime{rt}); err != nil || !closed {
		t.Fatalf("special owned close failed: closed=%v err=%v", closed, err)
	}
}

func TestSpecialRuntimeCannotUseProcessCloseWithSharedOwnership(t *testing.T) {
	rt := &SymbolRuntime{Config: config.SymbolConfig{Symbol: "BTCUSDT"}, AccountScope: "account",
		CloseForShutdown: func(context.Context) error { t.Fatal("shared owner was closed"); return nil }}
	peer := &SymbolRuntime{Config: config.SymbolConfig{Symbol: "BTCUSDT"}, AccountScope: "account"}
	if err := closeProcessRuntimeGroup(t.Context(), []*SymbolRuntime{rt, peer}); err == nil {
		t.Fatal("shared specialized runtime ownership was accepted")
	}
}

func TestProcessShutdownClosesThroughBotIntentJournal(t *testing.T) {
	venue := &shutdownBarrierVenue{fakeCloseExchange: newFakeCloseExchange(100, 0)}
	rt := newShutdownRuntime(venue, "owner-a", "full-account-scope")
	store, err := storage.NewSQLStorage(filepath.Join(t.TempDir(), "shutdown-intents.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.MigrateExecutionIntents(t.Context()); err != nil {
		t.Fatal(err)
	}
	scope := execution.IntentScope{Account: rt.AccountScope, Exchange: "fake", Market: "futures", Symbol: "BTCUSDT", Bot: "owner-a"}
	if err := configureRuntimeIntentJournal(t.Context(), rt.ExchangeExecutor, rt.SuperPositionManager.OpeningGate(), venue, store, scope); err != nil {
		t.Fatal(err)
	}
	cid := utils.GenerateOrderIDWithSource(100, "BUY", 2, "")
	opened, err := rt.ExchangeExecutor.PlaceOrder(&order.OrderRequest{Symbol: "BTCUSDT", Side: "BUY", Type: "LIMIT", Quantity: 1, Price: 100, ClientOrderID: cid})
	if err != nil || opened == nil || venue.position != 1 {
		t.Fatalf("seed Bot-owned position: order=%+v err=%v position=%v", opened, err, venue.position)
	}
	rt.SuperPositionManager.OnOrderUpdate(position.OrderUpdate{OrderID: opened.OrderID, ClientOrderID: opened.ClientOrderID,
		Symbol: "BTCUSDT", Side: "BUY", Status: "FILLED", ExecutedQty: 1, AvgPrice: 100})
	if long, short := rt.SuperPositionManager.GetPositionLegQuantities(); long != 1 || short != 0 {
		t.Fatalf("Bot ledger not seeded: long=%v short=%v", long, short)
	}
	if err := prepareProcessShutdown(t.Context(), []*SymbolRuntime{rt}, true); err != nil {
		t.Fatal(err)
	}
	runProcessLevelCloseOnExit(true, []*SymbolRuntime{rt}, closeProcessRuntimeGroup)
	if venue.position != 0 || len(venue.placed) != 2 || rt.shutdownCloseHandledReason() == "" || rt.shutdownCloseUnverifiedReason() != "" {
		t.Fatalf("owner close incomplete: position=%v orders=%d handled=%q unknown=%q", venue.position, len(venue.placed), rt.shutdownCloseHandledReason(), rt.shutdownCloseUnverifiedReason())
	}
	key, err := scope.Key()
	if err != nil {
		t.Fatal(err)
	}
	rows, err := store.LoadExecutionIntents(t.Context(), key, 0, 10)
	if err != nil || len(rows) != 2 {
		t.Fatalf("opening and close intents were not durably recorded: rows=%d err=%v", len(rows), err)
	}
}

func TestProcessShutdownUnverifiedCancellationStopsAccountClose(t *testing.T) {
	v := &shutdownBarrierVenue{fakeCloseExchange: newFakeCloseExchange(100, 1), ackOnly: true}
	a, b := newShutdownRuntime(v, "a", "shared"), newShutdownRuntime(v, "b", "shared")
	other := newShutdownRuntime(&shutdownBarrierVenue{fakeCloseExchange: newFakeCloseExchange(100, 0)}, "c", "different")
	seedShutdownOpening(t, a, "pending")
	if err := prepareProcessShutdown(t.Context(), []*SymbolRuntime{b, a, other}, true); err == nil {
		t.Fatal("cancel ACK accepted")
	}
	if a.shutdownCloseUnverifiedReason() == "" || b.shutdownCloseUnverifiedReason() == "" || other.shutdownCloseUnverifiedReason() != "" {
		t.Fatal("failure not isolated/propagated by account")
	}
	var closed [][]*SymbolRuntime
	runProcessLevelCloseOnExit(true, []*SymbolRuntime{b, a, other}, func(_ context.Context, group []*SymbolRuntime) error { closed = append(closed, group); return nil })
	if len(closed) != 1 || len(closed[0]) != 1 || closed[0][0] != other {
		t.Fatalf("blind account close after failed preparation: %v", closed)
	}
}

func TestProcessShutdownWithoutCancelKeepsOrdersButSealsSubmission(t *testing.T) {
	v := &shutdownBarrierVenue{fakeCloseExchange: newFakeCloseExchange(100, 0)}
	rt := newShutdownRuntime(v, "a", "account")
	seedShutdownOpening(t, rt, "resting")
	if err := prepareProcessShutdown(t.Context(), []*SymbolRuntime{rt}, false); err != nil {
		t.Fatal(err)
	}
	if v.orders[101].Status != exchange.OrderStatusNew {
		t.Fatal("cancel_on_exit=false unexpectedly cancelled order")
	}
	ctx, err := rt.ExchangeExecutor.ShutdownCloseContext(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	// The real grid liquidation adapter must propagate the scoped permission.
	if _, err := rt.SuperPositionManager.NewLiquidationVenue(v).PlaceMarketOrder(ctx, "BTCUSDT", "SELL", 1, true); err != nil {
		t.Fatal(err)
	}
}

func TestShutdownFlatnessNeverNetsOppositeOrTinyLegs(t *testing.T) {
	for _, positions := range [][]*exchange.Position{
		{{Symbol: "BTCUSDT", Size: 1}, {Symbol: "BTCUSDT", Size: -1}},
		{{Symbol: "BTCUSDT", Size: 1e-13}},
		{{Symbol: "BTCUSDT", Size: math.NaN()}}, {nil},
	} {
		rt := &SymbolRuntime{Exchange: &runtimeJournalVenue{positions: positions}}
		flat, err := exchangePositionFlatChecker(config.SymbolConfig{Symbol: "BTCUSDT", MarketType: "futures"}, rt)(t.Context())
		if flat {
			t.Fatalf("gross/malformed exposure treated as flat: %v %v", positions, err)
		}
	}
}

func TestProcessShutdownFreezesBotLifecycleAndWaitsForAdmittedTransition(t *testing.T) {
	v := &shutdownBarrierVenue{fakeCloseExchange: newFakeCloseExchange(100, 0)}
	rt := newShutdownRuntime(v, "a", "account")
	bm := &BotManager{runtimes: map[string]*BotRuntime{"a": {BotID: "a", Inner: rt}}}
	finish, err := bm.runtimeAdmissions.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer finish()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
	defer cancel()
	rts, err := bm.sealProcessRuntimes(ctx)
	if !errors.Is(err, context.DeadlineExceeded) || len(rts) != 1 || rt.shutdownCloseUnverifiedReason() == "" {
		t.Fatalf("inflight transition not held: %v", err)
	}
	if _, err := bm.StartBot(t.Context(), config.BotConfig{ID: "new"}); err == nil {
		t.Fatal("new startup admitted during shutdown")
	}
	if err := bm.StopBot("a"); err == nil {
		t.Fatal("API stop removed shutdown inventory")
	}
	if len(bm.ListSymbolRuntimes()) != 1 {
		t.Fatal("shutdown inventory changed")
	}
	finish()
	if _, err := bm.sealProcessRuntimes(t.Context()); err != nil {
		t.Fatal(err)
	}
	if !rt.SuperPositionManager.OpeningGate().HasBlock(order.RuntimeShutdownBlock) {
		t.Fatal("runtime unsealed")
	}
}

func TestProcessShutdownRetainsUnverifiedOverlappingTransition(t *testing.T) {
	v := &shutdownBarrierVenue{fakeCloseExchange: newFakeCloseExchange(100, 0)}
	rt := newShutdownRuntime(v, "a", "account")
	bm := &BotManager{runtimes: map[string]*BotRuntime{"a": {BotID: "a", Inner: rt}}}
	bm.shutdownTransitionUnverified.Store(true)
	if _, err := bm.sealProcessRuntimes(t.Context()); err == nil || rt.shutdownCloseUnverifiedReason() == "" {
		t.Fatal("lost overlapping runtime uncertainty")
	}
}

func TestProcessShutdownTotalDeadlineRetainsEveryUnverifiedScope(t *testing.T) {
	first := newShutdownRuntime(&shutdownBarrierVenue{fakeCloseExchange: newFakeCloseExchange(100, 0)}, "a", "account-a")
	second := newShutdownRuntime(&shutdownBarrierVenue{fakeCloseExchange: newFakeCloseExchange(100, 0)}, "b", "account-b")
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	started := time.Now()
	runProcessLevelCloseOnExitContext(ctx, true, []*SymbolRuntime{first, second}, func(ctx context.Context, _ []*SymbolRuntime) error {
		<-ctx.Done()
		return ctx.Err()
	})
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("shutdown exceeded its shared deadline: %v", elapsed)
	}
	if first.shutdownCloseUnverifiedReason() == "" || second.shutdownCloseUnverifiedReason() == "" {
		t.Fatal("deadline expiry did not retain uncertainty for every affected scope")
	}
}

func TestRuntimeStopContextPreservesProcessDeadline(t *testing.T) {
	parent, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	rt := &SymbolRuntime{}
	rt.setShutdownContext(parent)
	stopCtx := rt.stopContext(context.Background())
	parentDeadline, parentOK := parent.Deadline()
	stopDeadline, stopOK := stopCtx.Deadline()
	if !parentOK || !stopOK || !parentDeadline.Equal(stopDeadline) {
		t.Fatalf("Bot stop context lost shared deadline: parent=%v stop=%v", parentDeadline, stopDeadline)
	}
	cancel()
	if !errors.Is(stopCtx.Err(), context.Canceled) {
		t.Fatal("Bot stop context detached from process shutdown cancellation")
	}
}

func TestBotStopKeepsOwnershipUntilRuntimeActuallyStops(t *testing.T) {
	bus := event.NewEventBus(10)
	defer bus.Close()
	entered, finish := make(chan struct{}), make(chan struct{})
	br := &BotRuntime{BotID: "a", Config: config.BotConfig{ID: "a"}, Inner: &SymbolRuntime{Stop: func() { close(entered); <-finish }}}
	bm := &BotManager{cfg: &config.Config{}, eventBus: bus, runtimes: map[string]*BotRuntime{"a": br}, botStatesFileOverride: filepath.Join(t.TempDir(), "states.json")}
	done := make(chan error, 1)
	go func() { done <- bm.StopBot("a") }()
	<-entered
	if _, ok := bm.Get("a"); !ok {
		t.Fatal("stopping owner disappeared early")
	}
	if rt, err := bm.StartBot(t.Context(), config.BotConfig{ID: "a"}); err != nil || rt != nil {
		t.Fatalf("same Bot restarted while stopping: %v", err)
	}
	close(finish)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if _, ok := bm.Get("a"); ok {
		t.Fatal("stopped runtime not removed")
	}
}
