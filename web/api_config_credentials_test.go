package web

import "testing"

func TestDefaultTestSymbolAndMarket(t *testing.T) {
	s, m := defaultTestSymbolAndMarket("binance", "")
	if s != "BTCUSDT" || m != "futures" {
		t.Fatalf("binance default: got %s %s", s, m)
	}
	s, m = defaultTestSymbolAndMarket("Bitkub", "futures")
	if s != "BTC_THB" || m != "spot" {
		t.Fatalf("bitkub: got %s %s", s, m)
	}
	s, m = defaultTestSymbolAndMarket("coinsph", "")
	if s != "BTC_PHP" || m != "spot" {
		t.Fatalf("coinsph: got %s %s", s, m)
	}
	s, m = defaultTestSymbolAndMarket("okx", "spot")
	if s != "BTCUSDT" || m != "spot" {
		t.Fatalf("okx spot: got %s %s", s, m)
	}
}
