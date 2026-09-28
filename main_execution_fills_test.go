package main

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"quantmesh/exchange"
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
