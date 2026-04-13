package web

import (
	"testing"

	"quantmesh/config"
)

func TestOpeningControlRuntimeMatchesQuery(t *testing.T) {
	rt := struct {
		Config config.SymbolConfig
	}{
		Config: config.SymbolConfig{
			Exchange: "binance",
			Symbol:   "BTCUSDT",
		},
	}
	if !openingControlRuntimeMatchesQuery(&rt, "Binance", "btcusdt", "futures") {
		t.Fatal("expected futures match")
	}
	if openingControlRuntimeMatchesQuery(&rt, "okx", "BTCUSDT", "futures") {
		t.Fatal("exchange mismatch should fail")
	}
	rt2 := struct {
		Config config.SymbolConfig
	}{
		Config: config.SymbolConfig{
			Exchange:    "binance",
			Symbol:      "BTCUSDT",
			MarketType:  "spot",
			UseSpotMargin: true,
		},
	}
	if !openingControlRuntimeMatchesQuery(&rt2, "binance", "BTCUSDT", "spot") {
		t.Fatal("spot_margin should normalize to spot for comparison")
	}
}
