package okx

import "testing"

func TestHandlePriceMessageFiltersByInstId(t *testing.T) {
	w := NewWebSocketManager("k", "s", "p", false)
	w.priceInstID = "BTC-USDT"
	var got float64
	w.priceCallback = func(p float64) { got = p }

	wrong := []byte(`{"arg":{"channel":"tickers","instId":"ETH-USDT"},"data":[{"instId":"ETH-USDT","last":"3000"}]}`)
	w.handlePriceMessage(wrong)
	if got != 0 {
		t.Fatalf("expected no update for wrong instId, got %v", got)
	}

	ok := []byte(`{"arg":{"channel":"tickers","instId":"BTC-USDT"},"data":[{"instId":"BTC-USDT","last":"74459.5"}]}`)
	w.handlePriceMessage(ok)
	if got != 74459.5 {
		t.Fatalf("expected 74459.5, got %v", got)
	}
	if v := w.GetLatestPrice(); v != 74459.5 {
		t.Fatalf("GetLatestPrice: got %v", v)
	}
}
