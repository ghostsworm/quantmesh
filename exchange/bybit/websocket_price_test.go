package bybit

import "testing"

func TestHandlePriceMessageFiltersTopic(t *testing.T) {
	w := NewWebSocketManager("", "", false)
	w.priceTickerKey = "BTCUSDT"
	var got float64
	w.priceCallback = func(p float64) { got = p }

	wrong := []byte(`{"topic":"tickers.ETHUSDT","data":{"lastPrice":"3000"}}`)
	w.handlePriceMessage(wrong)
	if got != 0 {
		t.Fatalf("expected no update for wrong topic, got %v", got)
	}

	ok := []byte(`{"topic":"tickers.BTCUSDT","data":{"lastPrice":"74459.5"}}`)
	w.handlePriceMessage(ok)
	if got != 74459.5 {
		t.Fatalf("expected 74459.5, got %v", got)
	}
	if v := w.GetLatestPrice(); v != 74459.5 {
		t.Fatalf("GetLatestPrice: got %v", v)
	}
}
