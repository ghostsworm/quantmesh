package web

import (
	"testing"
)

func TestUnregisterSymbolProvidersClearsMaps(t *testing.T) {
	statusBySymbol = make(map[string]*SystemStatus)
	priceProviders = make(map[string]PriceProvider)
	exchangeProviders = make(map[string]ExchangeProvider)
	positionProviders = make(map[string]PositionManagerProvider)
	riskProviders = make(map[string]RiskMonitorProvider)
	storageProviders = make(map[string]StorageServiceProvider)
	fundingProviders = make(map[string]FundingMonitorProvider)

	st := &SystemStatus{Running: true, Exchange: "binance", Symbol: "BTCUSDT", MarketType: "futures"}
	RegisterSymbolProviders("binance", "BTCUSDT", &SymbolScopedProviders{Status: st}, "futures")

	key := makeSymbolKey("binance", "BTCUSDT", "futures")
	if _, ok := statusBySymbol[key]; !ok {
		t.Fatalf("expected status registered under key %s", key)
	}

	UnregisterSymbolProviders("binance", "BTCUSDT", "futures")

	if st.Running {
		t.Fatal("expected Running=false before map delete")
	}
	statusMu.RLock()
	_, still := statusBySymbol[key]
	statusMu.RUnlock()
	if still {
		t.Fatal("expected status unregistered")
	}
}

func TestIsSymbolStatusRegistered(t *testing.T) {
	statusBySymbol = make(map[string]*SystemStatus)
	priceProviders = make(map[string]PriceProvider)
	exchangeProviders = make(map[string]ExchangeProvider)
	positionProviders = make(map[string]PositionManagerProvider)
	riskProviders = make(map[string]RiskMonitorProvider)
	storageProviders = make(map[string]StorageServiceProvider)
	fundingProviders = make(map[string]FundingMonitorProvider)

	if IsSymbolStatusRegistered("binance", "ETHUSDT", "futures") {
		t.Fatal("unexpected registration")
	}
	st := &SystemStatus{Running: true, Exchange: "binance", Symbol: "ETHUSDT"}
	RegisterSymbolProviders("binance", "ETHUSDT", &SymbolScopedProviders{Status: st}, "futures")
	if !IsSymbolStatusRegistered("binance", "ETHUSDT", "futures") {
		t.Fatal("expected registration")
	}
	got, ok := GetRegisteredSystemStatus("binance", "ETHUSDT", "futures")
	if !ok || got != st {
		t.Fatal("GetRegisteredSystemStatus mismatch")
	}
}
