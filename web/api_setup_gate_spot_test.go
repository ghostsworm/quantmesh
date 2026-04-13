package web

import (
	"strings"
	"testing"
)

func TestGateSpotSymbolsFromCurrencyPairsJSON(t *testing.T) {
	// 與 Gate 公開 API 實際回傳一致：quote 為大寫 USDT，可交易為 trade_status=tradable（無舊版 tradeable 布爾）
	const sample = `[
		{"id":"BTC_USDT","base":"BTC","quote":"USDT","trade_status":"tradable"},
		{"id":"ETH_USDT","quote":"USDT","trade_status":"untradable"},
		{"id":"SOL_USDT","quote":"usdt","trade_status":"tradable"}
	]`
	syms, err := gateSpotSymbolsFromCurrencyPairsJSON(strings.NewReader(sample))
	if err != nil {
		t.Fatal(err)
	}
	if len(syms) != 2 {
		t.Fatalf("expected 2 symbols, got %d: %v", len(syms), syms)
	}
	want := map[string]bool{"BTCUSDT": true, "SOLUSDT": true}
	for _, s := range syms {
		if !want[s] {
			t.Errorf("unexpected symbol %q", s)
		}
	}
}
