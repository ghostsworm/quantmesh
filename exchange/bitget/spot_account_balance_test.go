package bitget

import "testing"

func TestSummarizeBitgetSpotQuoteBalanceDoesNotMixAssets(t *testing.T) {
	balances := []bitgetSpotAccountAsset{
		{Coin: "BTC", Available: "2", Frozen: "0", Locked: "1"},
		{Coin: "USDT", Available: "10", Frozen: "2", Locked: "3"},
		{Coin: "USDC", Available: "99", Frozen: "0", Locked: "4"},
	}
	total, available, err := summarizeBitgetSpotQuoteBalance(balances, "usdt")
	if err != nil || total != 15 || available != 10 {
		t.Fatalf("USDT total/available = %v/%v, err=%v; want 15/10", total, available, err)
	}
	total, available, err = summarizeBitgetSpotQuoteBalance(balances, "EUR")
	if err != nil || total != 0 || available != 0 {
		t.Fatalf("missing quote balance = %v/%v, err=%v; want 0/0", total, available, err)
	}
}

func TestSummarizeBitgetSpotQuoteBalanceRejectsInvalidQuote(t *testing.T) {
	for _, invalid := range []string{"bad", "NaN", "Inf", "-1"} {
		t.Run(invalid, func(t *testing.T) {
			_, _, err := summarizeBitgetSpotQuoteBalance([]bitgetSpotAccountAsset{{Coin: "USDT", Available: invalid, Frozen: "0", Locked: "0"}}, "USDT")
			if err == nil {
				t.Fatalf("invalid quote amount %q accepted", invalid)
			}
		})
	}
	if _, _, err := summarizeBitgetSpotQuoteBalance([]bitgetSpotAccountAsset{{Coin: "USDT", Available: "1", Frozen: "bad", Locked: "0"}}, "USDT"); err == nil {
		t.Fatal("invalid frozen amount must block account balance")
	}
	if _, _, err := summarizeBitgetSpotQuoteBalance(nil, " "); err == nil {
		t.Fatal("empty quote asset must be rejected")
	}
}
