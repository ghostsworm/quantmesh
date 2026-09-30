package strategy

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"quantmesh/config"
	"quantmesh/exchange"
	"quantmesh/position"
)

func TestNewSpotLongStrategy(t *testing.T) {
	cfg := &config.Config{}
	cfg.Trading.Symbol = "BTCUSDT"
	s := NewSpotLongStrategy("spot_long", cfg, nil, nil, map[string]interface{}{
		"group_id": "bg-test123",
		"symbol":   "ETHUSDT",
	})
	if s == nil {
		t.Fatal("expected non-nil strategy")
	}
	if s.Name() != "spot_long" {
		t.Errorf("expected name spot_long, got %s", s.Name())
	}
}

func TestSpotLongPositionReadFailureDoesNotAssumeFlat(t *testing.T) {
	s := NewSpotLongStrategy("spot_long", &config.Config{}, nil, &failingSpotLongPositionExchange{err: errors.New("venue unavailable")}, nil)
	if got, err := s.getCurrentLongPosition(context.Background()); err == nil || got != 0 {
		t.Fatalf("position read failure must be surfaced, got qty=%v err=%v", got, err)
	}
}

type spotLongPositionResponseExchange struct {
	signalTestExchange
	response interface{}
	err      error
}

func (e *spotLongPositionResponseExchange) GetPositions(context.Context, string) (interface{}, error) {
	return e.response, e.err
}

func TestSpotLongPositionRejectsAmbiguousOrInvalidSnapshots(t *testing.T) {
	tests := []struct {
		name     string
		response interface{}
		want     float64
		wantErr  bool
	}{
		{name: "authoritative empty", response: []*position.PositionInfo{}, want: 0},
		{name: "single long position", response: []*position.PositionInfo{{Symbol: "BTCUSDT", Size: 0.25}}, want: 0.25},
		{name: "nil entry", response: []*position.PositionInfo{nil}, wantErr: true},
		{name: "negative quantity", response: []*position.PositionInfo{{Symbol: "BTCUSDT", Size: -0.25}}, wantErr: true},
		{name: "nan quantity", response: []*position.PositionInfo{{Symbol: "BTCUSDT", Size: math.NaN()}}, wantErr: true},
		{name: "infinite quantity", response: []*position.PositionInfo{{Symbol: "BTCUSDT", Size: math.Inf(1)}}, wantErr: true},
		{name: "duplicate symbol rows", response: []*position.PositionInfo{{Symbol: "BTCUSDT", Size: 0.25}, {Symbol: "BTCUSDT", Size: 0.25}}, wantErr: true},
		{name: "nil response", response: nil, wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ex := &spotLongPositionResponseExchange{response: tc.response}
			cfg := &config.Config{}
			cfg.Trading.Symbol = "BTCUSDT"
			s := NewSpotLongStrategy("spot_long", cfg, nil, ex, nil)
			got, err := s.getCurrentLongPosition(context.Background())
			if tc.wantErr {
				if err == nil || got != 0 {
					t.Fatalf("invalid position evidence accepted: quantity=%v err=%v", got, err)
				}
				return
			}
			if err != nil || got != tc.want {
				t.Fatalf("quantity=%v err=%v, want quantity=%v", got, err, tc.want)
			}
		})
	}
}

type failingSpotLongPositionExchange struct {
	signalTestExchange
	err error
}

func (e *failingSpotLongPositionExchange) GetPositions(context.Context, string) (interface{}, error) {
	return nil, e.err
}

func TestSpotLongPendingOrderPersistsAndRestores(t *testing.T) {
	cfg := &config.Config{}
	cfg.Trading.BotID = "bot-spot-long"
	cfg.Trading.Symbol = "BTCUSDT"
	store := &memoryRuntimeStateStore{}
	first := NewSpotLongStrategy("spot_long", cfg, nil, &spotLongRestoreExchange{order: &exchange.Order{OrderID: 99, Symbol: "BTCUSDT", Side: exchange.SideBuy, Status: exchange.OrderStatusPartiallyFilled, Quantity: 1, ExecutedQty: 0.6}}, nil)
	first.SetRuntimeStateStore(store)
	first.pendingOrders[99] = spotLongPendingOrder{Side: "BUY", Quantity: 1, ExecutedQty: 0.4}
	if err := first.OnOrderUpdate(&position.OrderUpdate{OrderID: 99, Symbol: "BTCUSDT", Side: "BUY", Status: "PARTIALLY_FILLED", ExecutedQty: 0.6}); err != nil {
		t.Fatal(err)
	}

	second := NewSpotLongStrategy("spot_long", cfg, nil, &spotLongRestoreExchange{order: &exchange.Order{OrderID: 99, Symbol: "BTCUSDT", Side: exchange.SideBuy, Status: exchange.OrderStatusPartiallyFilled, Quantity: 1, ExecutedQty: 0.6}}, nil)
	second.SetRuntimeStateStore(store)
	if err := second.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := second.pendingOrders[99]; got.ExecutedQty != 0.6 || got.Quantity != 1 {
		t.Fatalf("active order state did not restore: %+v", got)
	}
}

type spotLongRestoreExchange struct {
	signalTestExchange
	order *exchange.Order
	err   error
}

type spotLongClientOrderLookupExchange struct {
	spotLongRestoreExchange
	clientOrderID string
}

func (e *spotLongClientOrderLookupExchange) GetOrderByClientOrderID(_ context.Context, symbol, clientOrderID string) (*exchange.Order, error) {
	if symbol != "BTCUSDT" || clientOrderID != e.clientOrderID {
		return nil, errors.New("unexpected spot long client order lookup")
	}
	if e.order == nil {
		return nil, nil
	}
	return e.order, nil
}

type spotLongOrderCallbackExecutor struct {
	signalTestExecutor
	onPlace func(*position.OrderRequest, *position.Order)
}

func (e *spotLongOrderCallbackExecutor) PlaceOrder(req *position.OrderRequest) (*position.Order, error) {
	order, err := e.signalTestExecutor.PlaceOrder(req)
	if err == nil && e.onPlace != nil {
		e.onPlace(req, order)
	}
	return order, err
}

func (e *spotLongRestoreExchange) GetOrder(context.Context, string, int64) (interface{}, error) {
	return e.order, e.err
}

func TestSpotLongRefusesStartWithoutRuntimeStateStore(t *testing.T) {
	s := NewSpotLongStrategy("spot_long", &config.Config{}, nil, &signalTestExchange{}, nil)
	if err := s.Start(context.Background()); err == nil {
		t.Fatal("spot long started without durable pending-order recovery")
	}
}

func TestSpotLongOrderHistoryFailureBlocksStartup(t *testing.T) {
	cfg := &config.Config{}
	cfg.Trading.Symbol = "BTCUSDT"
	state := spotLongRuntimeState{Strategy: "spot_long", Symbol: "BTCUSDT", BaseAsset: "BTC",
		PendingOrders: map[int64]spotLongPendingOrder{99: {Side: "BUY", Quantity: 1}}}
	payload, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	store := &memoryRuntimeStateStore{version: spotLongRuntimeStateSchemaVersion, payload: string(payload), found: true}
	s := NewSpotLongStrategy("spot_long", cfg, nil, &spotLongRestoreExchange{err: errors.New("order history unavailable")}, nil)
	s.SetRuntimeStateStore(store)
	if err := s.Start(context.Background()); err == nil {
		t.Fatal("startup should fail when persisted order cannot be reconciled with exchange")
	}
}

func TestSpotLongPersistsIntentBeforeSubmitAndHandlesUpdateBeforeAck(t *testing.T) {
	for _, side := range []string{"BUY", "SELL"} {
		t.Run(side, func(t *testing.T) {
			cfg := &config.Config{}
			cfg.Trading.Symbol = "BTCUSDT"
			store := &memoryRuntimeStateStore{}
			var strategy *SpotLongStrategy
			executor := &spotLongOrderCallbackExecutor{}
			executor.onPlace = func(req *position.OrderRequest, order *position.Order) {
				var state spotLongRuntimeState
				if err := json.Unmarshal([]byte(store.payload), &state); err != nil {
					t.Fatalf("decode pre-submit intent: %v", err)
				}
				intent, found := state.PendingIntents[req.ClientOrderID]
				if !found || req.ClientOrderID == "" || intent.Side != side || intent.Quantity != req.Quantity {
					t.Fatalf("order was submitted without exact durable intent: req=%+v pending=%+v", req, state.PendingIntents)
				}
				if err := strategy.OnOrderUpdate(&position.OrderUpdate{OrderID: order.OrderID,
					ClientOrderID: req.ClientOrderID, Symbol: req.Symbol, Side: side,
					Status: "FILLED", ExecutedQty: req.Quantity}); err != nil {
					t.Fatalf("process order update before ack: %v", err)
				}
			}
			strategy = NewSpotLongStrategy("spot_long", cfg, executor, &signalTestExchange{}, nil)
			strategy.SetRuntimeStateStore(store)
			var err error
			if side == "BUY" {
				err = strategy.increaseLong(context.Background(), 0.25)
			} else {
				err = strategy.decreaseLong(context.Background(), 0.25)
			}
			if err != nil {
				t.Fatalf("place %s: %v", side, err)
			}
			if len(strategy.pendingIntents) != 0 || len(strategy.pendingOrders) != 0 {
				t.Fatalf("terminal update should clear durable intent/order: intents=%v orders=%v", strategy.pendingIntents, strategy.pendingOrders)
			}
		})
	}
}

func TestSpotLongUncertainSubmissionRetainsDurableIntent(t *testing.T) {
	store := &memoryRuntimeStateStore{}
	strategy := NewSpotLongStrategy("spot_long", &config.Config{}, &failingOrderExecutor{err: errors.New("response lost")}, &signalTestExchange{}, nil)
	strategy.SetRuntimeStateStore(store)
	if err := strategy.increaseLong(context.Background(), 0.25); err == nil {
		t.Fatal("ambiguous submit unexpectedly succeeded")
	}
	var state spotLongRuntimeState
	if !store.found || json.Unmarshal([]byte(store.payload), &state) != nil || len(state.PendingIntents) != 1 {
		t.Fatalf("ambiguous order result lost durable intent: found=%v intents=%+v", store.found, state.PendingIntents)
	}
}

func TestSpotLongStartupReconcilesPendingIntentByExactClientOrderID(t *testing.T) {
	cfg := &config.Config{}
	cfg.Trading.BotID = "spot-long-cid-recovery"
	cfg.Trading.Symbol = "BTCUSDT"
	clientOrderID := "spot-long-recovery-cid"
	order := &exchange.Order{OrderID: 103, ClientOrderID: clientOrderID, Symbol: "BTCUSDT", Side: exchange.SideSell,
		Status: exchange.OrderStatusNew, Quantity: 0.3}
	state := spotLongRuntimeState{BotID: cfg.Trading.BotID, Strategy: "spot_long", Symbol: "BTCUSDT", BaseAsset: "BTC",
		PendingOrders: map[int64]spotLongPendingOrder{}, PendingIntents: map[string]spotLongPendingIntent{
			clientOrderID: {Side: "SELL", Quantity: 0.3, CreatedAtUnixMilli: time.Now().UnixMilli()},
		}}
	payload, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	store := &memoryRuntimeStateStore{version: spotLongRuntimeStateSchemaVersion, payload: string(payload), found: true}
	venue := &spotLongClientOrderLookupExchange{spotLongRestoreExchange: spotLongRestoreExchange{order: order}, clientOrderID: clientOrderID}
	strategy := NewSpotLongStrategy("spot_long", cfg, &signalTestExecutor{}, venue, nil)
	strategy.SetRuntimeStateStore(store)
	if err := strategy.Start(context.Background()); err != nil {
		t.Fatalf("restore pending intent: %v", err)
	}
	if len(strategy.pendingIntents) != 0 || strategy.pendingOrders[103].ClientOrderID != clientOrderID || strategy.pendingOrders[103].Side != "SELL" {
		t.Fatalf("restored order did not retain exact identity: intents=%v orders=%+v", strategy.pendingIntents, strategy.pendingOrders)
	}
}

func TestSpotLongStartupRejectsMismatchedPersistedOrderIdentity(t *testing.T) {
	cfg := &config.Config{}
	cfg.Trading.BotID = "spot-long-mismatch"
	cfg.Trading.Symbol = "BTCUSDT"
	state := spotLongRuntimeState{BotID: cfg.Trading.BotID, Strategy: "spot_long", Symbol: "BTCUSDT", BaseAsset: "BTC",
		PendingOrders: map[int64]spotLongPendingOrder{103: {ClientOrderID: "expected-cid", Side: "BUY", Quantity: 0.3}}}
	payload, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	store := &memoryRuntimeStateStore{version: spotLongRuntimeStateSchemaVersion, payload: string(payload), found: true}
	venue := &spotLongRestoreExchange{order: &exchange.Order{OrderID: 103, ClientOrderID: "wrong-cid", Symbol: "BTCUSDT",
		Side: exchange.SideSell, Status: exchange.OrderStatusNew, Quantity: 0.4}}
	strategy := NewSpotLongStrategy("spot_long", cfg, &signalTestExecutor{}, venue, nil)
	strategy.SetRuntimeStateStore(store)
	if err := strategy.Start(context.Background()); err == nil || !strings.Contains(err.Error(), "identity or quantity") {
		t.Fatalf("startup must reject a mismatched exchange order: %v", err)
	}
}
