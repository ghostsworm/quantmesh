package strategy

import (
	"testing"

	"quantmesh/config"
)

func TestNewFuturesLongStrategy(t *testing.T) {
	cfg := &config.Config{}
	cfg.Trading.Symbol = "BTCUSDT"
	s := NewFuturesLongStrategy("futures_long", cfg, nil, nil, map[string]interface{}{
		"group_id": "bg-test123",
		"symbol":   "ETHUSDT",
	})
	if s == nil {
		t.Fatal("expected non-nil strategy")
	}
	if s.Name() != "futures_long" {
		t.Errorf("expected name futures_long, got %s", s.Name())
	}
}
