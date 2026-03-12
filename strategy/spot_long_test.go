package strategy

import (
	"testing"

	"quantmesh/config"
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
