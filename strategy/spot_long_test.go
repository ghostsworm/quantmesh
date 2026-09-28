package strategy

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

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
