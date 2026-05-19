package strategy

import (
	"testing"
	"time"

	"quantmesh/config"
	"quantmesh/event"
)

func TestGetTargetSpotPosition(t *testing.T) {
	hc := &HedgeCoordinator{
		group: config.BotGroup{
			HedgeConfig: config.HedgeConfig{
				HedgeRatio: 0.5,
			},
		},
	}
	target := hc.GetTargetSpotPosition(100)
	if target != 50 {
		t.Errorf("HedgeRatio 0.5: expected 50, got %f", target)
	}
	hc.group.HedgeConfig.HedgeRatio = 0
	target = hc.GetTargetSpotPosition(100)
	if target != 50 {
		t.Errorf("HedgeRatio 0 defaults to 0.5: expected 50, got %f", target)
	}
}

func TestGetFloat64(t *testing.T) {
	m := map[string]interface{}{
		"a": float64(1.5),
		"b": 2,
		"c": int64(3),
	}
	if getFloat64(m, "a") != 1.5 {
		t.Error("float64")
	}
	if getFloat64(m, "b") != 2 {
		t.Error("int")
	}
	if getFloat64(m, "c") != 3 {
		t.Error("int64")
	}
	if getFloat64(m, "x") != 0 {
		t.Error("missing key")
	}
}

func TestGetInt(t *testing.T) {
	m := map[string]interface{}{
		"a": float64(5),
		"b": 6,
	}
	if getInt(m, "a") != 5 {
		t.Error("float64")
	}
	if getInt(m, "b") != 6 {
		t.Error("int")
	}
}

func TestGetString(t *testing.T) {
	m := map[string]interface{}{
		"s": "hello",
	}
	if getString(m, "s") != "hello" {
		t.Error("string")
	}
	if getString(m, "x") != "" {
		t.Error("missing key")
	}
}

func TestHedgeCoordinatorPublishesZeroTargetBelowTrigger(t *testing.T) {
	bus := event.NewEventBus(10)
	ch := bus.Subscribe()
	hc := NewHedgeCoordinator(config.BotGroup{
		ID:     "group-1",
		BotIDs: []string{"spot-bot", "futures-bot"},
		HedgeConfig: config.HedgeConfig{
			PrimaryLeg:         "spot",
			Direction:          "LONG",
			HedgeTriggerLayers: 3,
			ShortNotionalRatio: 0.5,
		},
	}, bus)

	hc.onEvent(&event.Event{
		Type: event.EventTypePositionClosed,
		Data: map[string]interface{}{
			"bot_id":        "spot-bot",
			"market_type":   "spot",
			"position":      0.0,
			"filled_layers": 0,
			"symbol":        "BTCUSDT",
			"exchange":      "binance",
		},
	})

	select {
	case evt := <-ch:
		if evt.Type != event.EventTypeHedgeSignal {
			t.Fatalf("事件类型=%s want %s", evt.Type, event.EventTypeHedgeSignal)
		}
		if got := getFloat64(evt.Data, "target_futures_short"); got != 0 {
			t.Fatalf("target_futures_short=%.8f want 0", got)
		}
	case <-time.After(time.Second):
		t.Fatal("未收到对冲归零信号")
	}
}
