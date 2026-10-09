package strategy

import (
	"context"
	"encoding/json"
	"testing"

	"quantmesh/config"
	"quantmesh/exchange"
	"quantmesh/execution"
	"quantmesh/position"
)

func TestFuturesHedgeStrategiesBlockDuplicateOrdersUntilTerminalUpdate(t *testing.T) {
	tests := []struct {
		name string
		run  func(*testing.T, RuntimeStateStore) (Strategy, *hedgeOrderExecutor, error)
	}{
		{name: "long", run: func(t *testing.T, store RuntimeStateStore) (Strategy, *hedgeOrderExecutor, error) {
			executor := &hedgeOrderExecutor{}
			cfg := &config.Config{}
			cfg.Trading.Symbol = "BTCUSDT"
			strategy := NewFuturesLongStrategy("futures_long", cfg, executor, &hedgeExchange{price: 100}, nil)
			strategy.SetRuntimeStateStore(store)
			return strategy, executor, strategy.increaseLong(context.Background(), 1)
		}},
		{name: "short", run: func(t *testing.T, store RuntimeStateStore) (Strategy, *hedgeOrderExecutor, error) {
			executor := &hedgeOrderExecutor{}
			cfg := &config.Config{}
			cfg.Trading.Symbol = "BTCUSDT"
			strategy := NewFuturesShortStrategy("futures_short", cfg, executor, &hedgeExchange{price: 100}, nil)
			strategy.SetRuntimeStateStore(store)
			return strategy, executor, strategy.increaseShort(context.Background(), 1)
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &memoryRuntimeStateStore{}
			strategy, executor, err := tt.run(t, store)
			if err != nil || len(executor.orders) != 1 {
				t.Fatalf("initial hedge order failed: orders=%d err=%v", len(executor.orders), err)
			}
			request := executor.orders[0]
			if request.ClientOrderID == "" {
				t.Fatal("hedge order did not carry durable client identity")
			}
			var persisted futuresHedgeRuntimeState
			if !store.found || json.Unmarshal([]byte(store.payload), &persisted) != nil || persisted.Pending == nil || persisted.Pending.ClientOrderID != request.ClientOrderID {
				t.Fatalf("pending hedge order was not persisted: found=%v state=%+v", store.found, persisted)
			}
			if err := strategy.OnOrderUpdate(&position.OrderUpdate{OrderID: 1, ClientOrderID: request.ClientOrderID,
				Symbol: "BTCUSDT", Side: request.Side, Status: "PARTIALLY_FILLED", ExecutedQty: 0.25}); err != nil {
				t.Fatal(err)
			}
			if json.Unmarshal([]byte(store.payload), &persisted) != nil || persisted.Pending == nil || persisted.Pending.ExecutedQty != 0.25 {
				t.Fatalf("partial execution was not durably retained: %+v", persisted)
			}
			if tt.name == "long" {
				err = strategy.(*FuturesLongStrategy).increaseLong(context.Background(), 0.5)
			} else {
				err = strategy.(*FuturesShortStrategy).increaseShort(context.Background(), 0.5)
			}
			if err == nil || len(executor.orders) != 1 {
				t.Fatalf("active hedge order did not block a duplicate: orders=%d err=%v", len(executor.orders), err)
			}
			if err := strategy.OnOrderUpdate(&position.OrderUpdate{OrderID: 1, ClientOrderID: request.ClientOrderID,
				Symbol: "BTCUSDT", Side: request.Side, Status: "CANCELED", ExecutedQty: 0.25}); err != nil {
				t.Fatal(err)
			}
			if tt.name == "long" {
				err = strategy.(*FuturesLongStrategy).increaseLong(context.Background(), 0.5)
			} else {
				err = strategy.(*FuturesShortStrategy).increaseShort(context.Background(), 0.5)
			}
			if err != nil || len(executor.orders) != 2 {
				t.Fatalf("terminal order did not release hedge guard: orders=%d err=%v", len(executor.orders), err)
			}
		})
	}
}

func TestFuturesHedgeTrackerPreservesUnknownAndRestoresOpenOrder(t *testing.T) {
	store := &memoryRuntimeStateStore{}
	tracker := newFuturesHedgeOrderTracker(&config.Config{}, "futures_long", "group-a", "BTCUSDT", "")
	tracker.SetStore(store)
	cid, err := tracker.Begin("BUY", 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := tracker.SubmissionFailed(cid, execution.ErrOrderUnknown); err != nil {
		t.Fatal(err)
	}
	if _, err := tracker.Begin("BUY", 1); err == nil {
		t.Fatal("unknown submission should retain duplicate-order guard")
	}

	venue := &futuresHedgeLookupExchange{hedgeExchange: &hedgeExchange{}, order: &exchange.Order{
		OrderID: 81, ClientOrderID: cid, Symbol: "BTCUSDT", Side: exchange.SideBuy,
		Quantity: 1, Status: exchange.OrderStatusNew,
	}}
	restored := newFuturesHedgeOrderTracker(&config.Config{}, "futures_long", "group-a", "BTCUSDT", "")
	restored.SetStore(store)
	if err := restored.RestoreAndReconcile(context.Background(), venue); err != nil {
		t.Fatal(err)
	}
	if restored.pending == nil || restored.pending.OrderID != 81 || restored.pending.ClientOrderID != cid {
		t.Fatalf("open hedge order was not rebound after restart: %+v", restored.pending)
	}
}

func TestFuturesHedgeTrackerDoesNotClearMissingExchangeEvidence(t *testing.T) {
	store := &memoryRuntimeStateStore{}
	tracker := newFuturesHedgeOrderTracker(&config.Config{}, "futures_long", "group-a", "BTCUSDT", "mock")
	tracker.SetStore(store)
	cid, err := tracker.Begin("BUY", 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := tracker.SubmissionFailed(cid, execution.ErrOrderUnknown); err != nil {
		t.Fatal(err)
	}
	restarted := newFuturesHedgeOrderTracker(&config.Config{}, "futures_long", "group-a", "BTCUSDT", "mock")
	restarted.SetStore(store)
	venue := &futuresHedgeLookupExchange{hedgeExchange: &hedgeExchange{}}
	if err := restarted.RestoreAndReconcile(context.Background(), venue); err == nil {
		t.Fatal("missing exchange evidence must not release an unknown hedge order")
	}
	if _, err := restarted.Begin("BUY", 1); err == nil {
		t.Fatal("missing exchange evidence must keep duplicate-order guard active")
	}
}

func TestFuturesHedgeTrackerRejectsFilledWithoutExecution(t *testing.T) {
	store := &memoryRuntimeStateStore{}
	tracker := newFuturesHedgeOrderTracker(&config.Config{}, "futures_short", "group-a", "BTCUSDT", "")
	tracker.SetStore(store)
	cid, err := tracker.Begin("SELL", 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := tracker.Bind(cid, &position.Order{OrderID: 82, Quantity: 1}); err != nil {
		t.Fatal(err)
	}
	if err := tracker.OnOrderUpdate(&position.OrderUpdate{OrderID: 82, ClientOrderID: cid, Symbol: "BTCUSDT",
		Side: "SELL", Status: "FILLED", ExecutedQty: 0}); err == nil {
		t.Fatal("zero-fill FILLED update should be rejected")
	}
	if tracker.pending == nil {
		t.Fatal("invalid terminal update cleared durable hedge intent")
	}
}

func TestFuturesHedgeTrackerRetainsUnderfilledFilledOrder(t *testing.T) {
	store := &memoryRuntimeStateStore{}
	tracker := newFuturesHedgeOrderTracker(&config.Config{}, "futures_long", "group-a", "BTCUSDT", "mock")
	tracker.SetStore(store)
	cid, err := tracker.Begin("BUY", 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := tracker.Bind(cid, &position.Order{OrderID: 83, ClientOrderID: cid, Symbol: "BTCUSDT", Side: "BUY", Quantity: 1}); err != nil {
		t.Fatal(err)
	}
	if err := tracker.OnOrderUpdate(&position.OrderUpdate{OrderID: 83, ClientOrderID: cid, Symbol: "BTCUSDT",
		Side: "BUY", Status: "FILLED", ExecutedQty: 0.75}); err == nil {
		t.Fatal("underfilled FILLED update should be rejected")
	}
	if tracker.pending == nil || tracker.pending.ClientOrderID != cid {
		t.Fatal("underfilled FILLED update cleared the durable hedge-order guard")
	}
	var persisted futuresHedgeRuntimeState
	if err := json.Unmarshal([]byte(store.payload), &persisted); err != nil || persisted.Pending == nil || persisted.Pending.ClientOrderID != cid {
		t.Fatalf("underfilled FILLED update was not retained in runtime state: state=%+v err=%v", persisted, err)
	}
}

func TestFuturesHedgeRestoreRetainsUnderfilledFilledOrder(t *testing.T) {
	store := &memoryRuntimeStateStore{}
	tracker := newFuturesHedgeOrderTracker(&config.Config{}, "futures_short", "group-a", "BTCUSDT", "mock")
	tracker.SetStore(store)
	cid, err := tracker.Begin("SELL", 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := tracker.SubmissionFailed(cid, execution.ErrOrderUnknown); err != nil {
		t.Fatal(err)
	}
	restarted := newFuturesHedgeOrderTracker(&config.Config{}, "futures_short", "group-a", "BTCUSDT", "mock")
	restarted.SetStore(store)
	venue := &futuresHedgeLookupExchange{hedgeExchange: &hedgeExchange{}, order: &exchange.Order{
		OrderID: 84, ClientOrderID: cid, Symbol: "BTCUSDT", Side: exchange.SideSell,
		Quantity: 1, ExecutedQty: 0.5, Status: exchange.OrderStatusFilled,
	}}
	if err := restarted.RestoreAndReconcile(context.Background(), venue); err == nil {
		t.Fatal("underfilled FILLED REST snapshot should fail startup reconciliation")
	}
	if restarted.pending == nil || restarted.pending.ClientOrderID != cid {
		t.Fatal("underfilled FILLED REST snapshot cleared the durable hedge-order guard")
	}
}

type futuresHedgeLookupExchange struct {
	*hedgeExchange
	order *exchange.Order
}

func (e *futuresHedgeLookupExchange) GetOrderByClientOrderID(context.Context, string, string) (*exchange.Order, error) {
	if e.order == nil {
		return nil, nil
	}
	order := *e.order
	return &order, nil
}
