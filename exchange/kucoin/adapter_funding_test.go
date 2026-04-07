package kucoin

import (
	"encoding/json"
	"testing"
)

func TestNormalizeUnifiedSymbol(t *testing.T) {
	if got := normalizeUnifiedSymbol("BTCUSDT"); got != "BTC-USDT" {
		t.Fatalf("normalizeUnifiedSymbol(BTCUSDT) = %q, want BTC-USDT", got)
	}
	if got := normalizeUnifiedSymbol("BTC-USDT"); got != "BTC-USDT" {
		t.Fatalf("normalizeUnifiedSymbol(BTC-USDT) = %q", got)
	}
}

func TestKucoinContractSymbolForFutures(t *testing.T) {
	tests := []struct {
		unified string
		want    string
	}{
		{"BTC-USDT", "XBTUSDTM"},
		{"ETH-USDT", "ETHUSDTM"},
		{"SOL-USDT", "SOLUSDTM"},
		{"BTCUSDT", ""},
		{"BTC-USDC", ""},
	}
	for _, tt := range tests {
		if got := kucoinContractSymbolForFutures(tt.unified); got != tt.want {
			t.Errorf("kucoinContractSymbolForFutures(%q) = %q, want %q", tt.unified, got, tt.want)
		}
	}
	if got := kucoinContractSymbolForFutures(normalizeUnifiedSymbol("BTCUSDT")); got != "XBTUSDTM" {
		t.Errorf("round-trip BTCUSDT -> contract: got %q", got)
	}
}

func TestContractDetailJSON(t *testing.T) {
	const raw = `{"code":"200000","data":{"symbol":"XBTUSDTM","fundingFeeRate":-0.000019,"nextFundingRateDateTime":1775606400000,"markPrice":68342.7,"indexPrice":68346.01}}`
	var resp struct {
		Code string         `json:"code"`
		Data ContractDetail `json:"data"`
	}
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Data.Symbol != "XBTUSDTM" || resp.Data.NextFundingRateDateTime != 1775606400000 {
		t.Fatalf("unexpected detail: %+v", resp.Data)
	}
}
