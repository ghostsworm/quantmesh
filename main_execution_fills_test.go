package main

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"quantmesh/exchange"
	"quantmesh/execution"
	"quantmesh/order"
	"quantmesh/position"
	"quantmesh/storage"
)

type runtimeFillExchange struct {
	exchange.IExchange
	calls atomic.Int32
}

func (e *runtimeFillExchange) GetName() string       { return "binance" }
func (e *runtimeFillExchange) GetMarketType() string { return "futures" }
func (e *runtimeFillExchange) GetOrderFills(context.Context, string, int64) ([]*exchange.OrderFill, error) {
	e.calls.Add(1)
	return []*exchange.OrderFill{{OrderID: 12, TradeID: "fill-1", Symbol: "BTCUSDT", Side: exchange.SideBuy, Price: 100, Quantity: 1, TradeTime: 1_790_000_000_000}}, nil
}

type runtimeFillWriter struct {
	mu    sync.Mutex
	fills []*storage.OrderFill
}

type runtimeGridFillVenue struct {
	*runtimeJournalVenue
	fillCalls atomic.Int32
}

func (e *runtimeGridFillVenue) GetOrderFills(context.Context, string, int64) ([]*exchange.OrderFill, error) {
	e.fillCalls.Add(1)
	return []*exchange.OrderFill{{OrderID: 1, TradeID: "grid-fill-1", Symbol: "BTCUSDT", Side: exchange.SideBuy,
		Price: 100, Quantity: 1, TradeTime: 1_790_000_000_000}}, nil
}

type sequencedRuntimeFillVenue struct {
	*runtimeJournalVenue
	calls atomic.Int32
}

func (e *sequencedRuntimeFillVenue) GetOrderFills(context.Context, string, int64) ([]*exchange.OrderFill, error) {
	call := e.calls.Add(1)
	if call == 1 {
		return []*exchange.OrderFill{{OrderID: 12, TradeID: "fill-half", Symbol: "BTCUSDT", Side: exchange.SideBuy,
			Price: 100, Quantity: 0.5, TradeTime: 1_790_000_000_000}}, nil
	}
	return []*exchange.OrderFill{
		{OrderID: 12, TradeID: "fill-half", Symbol: "BTCUSDT", Side: exchange.SideBuy, Price: 100, Quantity: 0.5, TradeTime: 1_790_000_000_000},
		{OrderID: 12, TradeID: "fill-second-half", Symbol: "BTCUSDT", Side: exchange.SideBuy, Price: 101, Quantity: 0.5, TradeTime: 1_790_000_000_001},
	}, nil
}

type blockingRuntimeFillWriter struct {
	entered chan struct{}
	release chan struct{}
	mu      sync.Mutex
	fills   []*storage.OrderFill
}

func (w *blockingRuntimeFillWriter) SaveOrderFill(fill *storage.OrderFill) error {
	w.entered <- struct{}{}
	<-w.release
	w.mu.Lock()
	w.fills = append(w.fills, fill)
	w.mu.Unlock()
	return nil
}

func (w *runtimeFillWriter) SaveOrderFill(fill *storage.OrderFill) error {
	w.mu.Lock()
	w.fills = append(w.fills, fill)
	w.mu.Unlock()
	return nil
}

func TestTerminalOrderUpdate(t *testing.T) {
	for _, test := range []struct {
		status string
		want   bool
	}{
		{status: "FILLED", want: true},
		{status: "CANCELED", want: true},
		{status: "PARTIALLY_FILLED_CANCELED", want: true},
		{status: "NEW", want: false},
		{status: "PARTIALLY_FILLED", want: false},
	} {
		if got := terminalOrderUpdate(test.status); got != test.want {
			t.Errorf("terminalOrderUpdate(%q)=%v, want %v", test.status, got, test.want)
		}
	}
}

func TestRuntimeFillCaptureRunsOnceForOwnedTerminalFill(t *testing.T) {
	capture := newRuntimeFillCapture()
	provider, writer := &runtimeFillExchange{}, &runtimeFillWriter{}
	success := make(chan struct{}, 1)
	var failures atomic.Int32
	update := position.OrderUpdate{OrderID: 12, Symbol: "BTCUSDT", Status: "FILLED", ExecutedQty: 1}
	for i := 0; i < 2; i++ {
		capture.Observe(context.Background(), provider, writer, update, "binance", "futures", "scope", "acct", "bot", func(error) {
			failures.Add(1)
		}, func() { success <- struct{}{} })
	}
	select {
	case <-success:
	case <-time.After(time.Second):
		t.Fatal("terminal fill was not captured")
	}
	time.Sleep(10 * time.Millisecond)
	if provider.calls.Load() != 1 || failures.Load() != 0 {
		t.Fatalf("capture should be idempotent: provider calls=%d failures=%d", provider.calls.Load(), failures.Load())
	}
	writer.mu.Lock()
	defer writer.mu.Unlock()
	if len(writer.fills) != 1 || writer.fills[0].BotID != "bot" {
		t.Fatalf("expected one Bot-attributed execution, got %+v", writer.fills)
	}
}

func TestRuntimeFillCaptureConfirmsAlreadyDurableTarget(t *testing.T) {
	capture := newRuntimeFillCapture()
	capture.captured[12] = 1
	provider, writer := &runtimeFillExchange{}, &runtimeFillWriter{}
	succeeded := false
	capture.Observe(context.Background(), provider, writer,
		position.OrderUpdate{OrderID: 12, Symbol: "BTCUSDT", Status: "FILLED", ExecutedQty: 1},
		"binance", "futures", "scope", "acct", "bot", func(error) { t.Fatal("unexpected capture failure") },
		func() { succeeded = true })
	if !succeeded {
		t.Fatal("duplicate verified terminal update must release its reconciliation gate")
	}
	if provider.calls.Load() != 0 {
		t.Fatalf("already durable target should not be fetched again: calls=%d", provider.calls.Load())
	}
}

func TestCapturedGridFillSettlesOnlyAfterFillHistoryIsDurable(t *testing.T) {
	baseVenue := &runtimeJournalVenue{}
	venue := &runtimeGridFillVenue{runtimeJournalVenue: baseVenue}
	journal, err := storage.NewSQLStorage(filepath.Join(t.TempDir(), "grid-fill-settlement.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = journal.Close() })
	if err := journal.MigrateExecutionIntents(t.Context()); err != nil {
		t.Fatal(err)
	}
	scope := runtimeJournalScope()
	executor, gate := newJournalRuntime(venue, scope)
	if err := executor.ConfigureIntentJournal(t.Context(), journal, scope); err != nil {
		t.Fatal(err)
	}
	const clientOrderID = "grid-fill-capture"
	if _, err := executor.PlaceOrder(&order.OrderRequest{Symbol: scope.Symbol, Side: "BUY", Price: 100, Quantity: 1,
		ClientOrderID: clientOrderID, StrategyName: "Grid-BTCUSDT", StrategyType: "grid"}); err != nil {
		t.Fatal(err)
	}
	baseVenue.mu.Lock()
	baseVenue.liveOrders[1].Status = exchange.OrderStatusFilled
	baseVenue.liveOrders[1].ExecutedQty = 1
	baseVenue.liveOrders[1].AvgPrice = 100
	baseVenue.mu.Unlock()
	update := position.OrderUpdate{OrderID: 1, ClientOrderID: clientOrderID, Symbol: scope.Symbol,
		Side: "BUY", Status: "FILLED", ExecutedQty: 1}
	if !observeOwnedRuntimeOrder(executor, &update) {
		t.Fatal("terminal grid fill was not observed")
	}
	writer := &blockingRuntimeFillWriter{entered: make(chan struct{}, 1), release: make(chan struct{})}
	settled := make(chan struct{}, 1)
	captureTerminalOrderAndSettleOwnedIntent(context.Background(), newRuntimeFillCapture(), venue, writer, update,
		"fake", "futures", "account-scope", "account", scope.Bot, executor, gate, "grid", false, true,
		func(err error) { t.Errorf("capture failed: %v", err) }, func(err error) { t.Errorf("grid settlement failed: %v", err) },
		func() { settled <- struct{}{} })
	select {
	case <-writer.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("complete venue fill capture did not start")
	}
	if !gate.Blocked() {
		t.Fatal("opening gate was released before the execution fill ledger became durable")
	}
	restarted, restartedGate := newJournalRuntime(venue, scope)
	if err := restarted.ConfigureIntentJournal(t.Context(), journal, scope); !errors.Is(err, execution.ErrOrderUnknown) || !restartedGate.HasBlock(order.IntentRecoveryBlock) {
		t.Fatalf("in-flight grid intent was not retained as unresolved: err=%v blocks=%v", err, restartedGate.Sources())
	}
	close(writer.release)
	select {
	case <-settled:
	case <-time.After(3 * time.Second):
		t.Fatal("durable fill capture did not settle the grid intent")
	}
	writer.mu.Lock()
	fillCount := len(writer.fills)
	writer.mu.Unlock()
	if fillCount != 1 || venue.fillCalls.Load() != 1 {
		t.Fatalf("fill history capture = %d rows / %d venue calls, want one each", fillCount, venue.fillCalls.Load())
	}
	if gate.Blocked() {
		t.Fatalf("grid settlement left opening gate blocked: %v", gate.Sources())
	}
	recovered, recoveredGate := newJournalRuntime(venue, scope)
	if err := recovered.ConfigureIntentJournal(t.Context(), journal, scope); err != nil {
		t.Fatalf("settled grid intent still blocks restart recovery: %v", err)
	}
	if recoveredGate.HasBlock(order.IntentRecoveryBlock) {
		t.Fatal("successful restart recovery kept the execution journal blocked")
	}
}

func TestCapturedStrategyFillSettlesOnlyAfterFillHistoryIsDurable(t *testing.T) {
	baseVenue := &runtimeJournalVenue{}
	venue := &runtimeGridFillVenue{runtimeJournalVenue: baseVenue}
	journal, err := storage.NewSQLStorage(filepath.Join(t.TempDir(), "strategy-fill-settlement.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = journal.Close() })
	if err := journal.MigrateExecutionIntents(t.Context()); err != nil {
		t.Fatal(err)
	}
	scope := runtimeJournalScope()
	executor, gate := newJournalRuntime(venue, scope)
	if err := executor.ConfigureIntentJournal(t.Context(), journal, scope); err != nil {
		t.Fatal(err)
	}
	const clientOrderID = "spot-long-fill-capture"
	if _, err := executor.PlaceOrder(&order.OrderRequest{Symbol: scope.Symbol, Side: "BUY", Price: 100, Quantity: 1,
		ClientOrderID: clientOrderID, StrategyName: "spot_long", StrategyType: "spot_long"}); err != nil {
		t.Fatal(err)
	}
	baseVenue.mu.Lock()
	baseVenue.liveOrders[1].Status = exchange.OrderStatusFilled
	baseVenue.liveOrders[1].ExecutedQty = 1
	baseVenue.liveOrders[1].AvgPrice = 100
	baseVenue.mu.Unlock()
	update := position.OrderUpdate{OrderID: 1, ClientOrderID: clientOrderID, Symbol: scope.Symbol,
		Side: "BUY", Status: "FILLED", ExecutedQty: 1}
	if !observeOwnedRuntimeOrder(executor, &update) {
		t.Fatal("terminal strategy fill was not observed")
	}
	writer := &blockingRuntimeFillWriter{entered: make(chan struct{}, 1), release: make(chan struct{})}
	settled := make(chan struct{}, 1)
	captureTerminalOrderAndSettleOwnedIntent(context.Background(), newRuntimeFillCapture(), venue, writer, update,
		"fake", "spot", "account-scope", "account", scope.Bot, executor, gate, "spot_long", true, false,
		func(err error) { t.Errorf("capture failed: %v", err) }, func(err error) { t.Errorf("strategy settlement failed: %v", err) },
		func() { settled <- struct{}{} })
	select {
	case <-writer.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("complete venue fill capture did not start")
	}
	if !gate.Blocked() {
		t.Fatal("opening gate was released before the execution fill ledger became durable")
	}
	restarted, restartedGate := newJournalRuntime(venue, scope)
	if err := restarted.ConfigureIntentJournal(t.Context(), journal, scope); !errors.Is(err, execution.ErrOrderUnknown) || !restartedGate.HasBlock(order.IntentRecoveryBlock) {
		t.Fatalf("strategy intent was settled before durable fill history: err=%v blocks=%v", err, restartedGate.Sources())
	}
	close(writer.release)
	select {
	case <-settled:
	case <-time.After(3 * time.Second):
		t.Fatal("durable strategy fill capture did not settle the intent")
	}
	if gate.Blocked() {
		t.Fatalf("strategy settlement left opening gate blocked: %v", gate.Sources())
	}
	recovered, recoveredGate := newJournalRuntime(venue, scope)
	if err := recovered.ConfigureIntentJournal(t.Context(), journal, scope); err != nil {
		t.Fatalf("durably captured strategy fill still blocks restart: %v", err)
	}
	if recoveredGate.HasBlock(order.IntentRecoveryBlock) {
		t.Fatal("successful restart recovery kept the execution journal blocked")
	}
}

func TestRuntimeFillCaptureDoesNotSettleBeforeLatestCumulativeTarget(t *testing.T) {
	venue := &sequencedRuntimeFillVenue{runtimeJournalVenue: &runtimeJournalVenue{}}
	capture := newRuntimeFillCapture()
	writer := &blockingRuntimeFillWriter{entered: make(chan struct{}, 3), release: make(chan struct{})}
	success := make(chan int, 2)
	update := position.OrderUpdate{OrderID: 12, Symbol: "BTCUSDT", Side: "BUY", Status: "CANCELED", ExecutedQty: 0.5}
	capture.Observe(context.Background(), venue, writer, update, "fake", "futures", "scope", "acct", "bot",
		func(err error) { t.Errorf("first cumulative fill capture failed: %v", err) }, func() {
			writer.mu.Lock()
			count := len(writer.fills)
			writer.mu.Unlock()
			success <- count
		})
	select {
	case <-writer.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("first cumulative fill capture did not start")
	}
	update.ExecutedQty = 1
	capture.Observe(context.Background(), venue, writer, update, "fake", "futures", "scope", "acct", "bot",
		func(err error) { t.Errorf("latest cumulative fill capture failed: %v", err) }, func() {
			writer.mu.Lock()
			count := len(writer.fills)
			writer.mu.Unlock()
			success <- count
		})
	close(writer.release)
	select {
	case count := <-success:
		if count != 3 || venue.calls.Load() != 2 {
			t.Fatalf("capture success preceded latest cumulative target: rows=%d venue_calls=%d", count, venue.calls.Load())
		}
	case <-time.After(3 * time.Second):
		t.Fatal("latest cumulative fill target was not captured")
	}
}
