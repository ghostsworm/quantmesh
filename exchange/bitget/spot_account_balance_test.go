package bitget

import "testing"

func TestSummarizeBitgetSpotQuoteBalanceDoesNotMixAssets(t *testing.T) {
	balances := []bitgetSpotAccountAsset{
		{Coin: "BTC", Available: "2", Locked: "1"},
		{Coin: "USDT", Available: "10", Locked: "3"},
		{Coin: "USDC", Available: "99", Locked: "4"},
	}
	total, available, err := summarizeBitgetSpotQuoteBalance(balances, "usdt")
	if err != nil || total != 13 || available != 10 {
		t.Fatalf("USDT total/available = %v/%v, err=%v; want 13/10", total, available, err)
	}
	total, available, err = summarizeBitgetSpotQuoteBalance(balances, "EUR")
	if err != nil || total != 0 || available != 0 {
		t.Fatalf("missing quote balance = %v/%v, err=%v; want 0/0", total, available, err)
	}
}

func TestSummarizeBitgetSpotQuoteBalanceRejectsInvalidQuote(t *testing.T) {
	for _, invalid := range []string{"bad", "NaN", "Inf", "-1"} {
		t.Run(invalid, func(t *testing.T) {
			_, _, err := summarizeBitgetSpotQuoteBalance([]bitgetSpotAccountAsset{{Coin: "USDT", Available: invalid, Locked: "0"}}, "USDT")
			if err == nil {
				t.Fatalf("invalid quote amount %q accepted", invalid)
			}
		})
	}
	if _, _, err := summarizeBitgetSpotQuoteBalance(nil, " "); err == nil {
		t.Fatal("empty quote asset must be rejected")
	}
}
