package order

import (
	"testing"

	"quantmesh/exchange"
)

func TestNewExchangeOrderExecutorTrimsBotID(t *testing.T) {
	var ex exchange.IExchange
	oe := NewExchangeOrderExecutor(ex, "BTCUSDT", 1, 100, nil, "  bid-1  ")
	if oe.botID != "bid-1" {
		t.Fatalf("botID=%q", oe.botID)
	}
}
