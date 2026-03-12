package strategy

import (
	"testing"

	"quantmesh/config"
)

func TestNewFuturesShortStrategy(t *testing.T) {
	cfg := &config.Config{}
	cfg.Trading.Symbol = "BTCUSDT"
	s := NewFuturesShortStrategy("futures_short", cfg, nil, nil, map[string]interface{}{
		"group_id": "bg-test123",
		"symbol":   "ETHUSDT",
	})
	if s == nil {
		t.Fatal("expected non-nil strategy")
	}
	if s.Name() != "futures_short" {
		t.Errorf("expected name futures_short, got %s", s.Name())
	}
}
