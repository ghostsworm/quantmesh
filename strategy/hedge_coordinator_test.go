package strategy

import (
	"testing"

	"quantmesh/config"
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

