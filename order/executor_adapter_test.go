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

func TestIsMarginInsufficientError_OKX51008(t *testing.T) {
	if !isMarginInsufficientError("下單失败: 51008 - insufficient balance") {
		t.Fatal("expected 51008 to be margin insufficient")
	}
	if isMarginInsufficientError("unknown") {
		t.Fatal("unexpected")
	}
}
