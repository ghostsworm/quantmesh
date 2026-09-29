package web

import (
	"strings"
	"testing"

	"quantmesh/config"
)

func TestAccountIDForExchangeIsOpaqueAndEnvironmentScoped(t *testing.T) {
	apiKey := "secret-key-prefix-and-suffix"
	cfg := &config.Config{Exchanges: map[string]config.ExchangeConfig{
		"binance": {APIKey: apiKey},
	}}
	live := accountIDForExchange(cfg, "binance")
	cfg.Exchanges["binance"] = config.ExchangeConfig{APIKey: apiKey, Testnet: true}
	testnet := accountIDForExchange(cfg, "binance")
	if len(live) != 64 || live == apiKey || strings.Contains(live, apiKey[:8]) {
		t.Fatalf("exchange account identifier exposes credential material: %q", live)
	}
	if live == testnet {
		t.Fatal("live and testnet account identifiers collided")
	}
}

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
