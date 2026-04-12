package feerate

import (
	"testing"

	"quantmesh/config"
)

func TestFetchFromExchangeAPI_NoKeys(t *testing.T) {
	cfg := &config.Config{
		Exchanges: map[string]config.ExchangeConfig{
			"binance": {APIKey: "", SecretKey: ""},
		},
	}
	_, _, err := FetchFromExchangeAPI(cfg, "binance", "BTCUSDT")
	if err == nil {
		t.Fatal("expected error when API keys missing")
	}
}

func TestFetchFromExchangeAPI_UnsupportedExchange(t *testing.T) {
	cfg := &config.Config{
		Exchanges: map[string]config.ExchangeConfig{
			"bybit": {APIKey: "k", SecretKey: "s"},
		},
	}
	_, _, err := FetchFromExchangeAPI(cfg, "bybit", "BTCUSDT")
	if err == nil {
		t.Fatal("expected unsupported exchange error")
	}
}
