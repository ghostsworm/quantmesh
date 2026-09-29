package main

import (
	"math"
	"testing"

	"quantmesh/exchange"
)

func TestInspectorAccountSummaryRequiresExplicitBalanceAsset(t *testing.T) {
	account := &exchange.Account{
		TotalWalletBalance: 12,
		TotalMarginBalance: 10,
		AvailableBalance:   4,
		BalanceAsset:       " btc ",
	}
	summary, err := inspectorAccountSummary("binance", "main", account)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Currency != "BTC" || summary.TotalBalance != 10 || summary.AvailableBalance != 4 || summary.UsedMargin != 6 {
		t.Fatalf("unexpected explicit-asset account summary: %+v", summary)
	}

	account.BalanceAsset = ""
	if summary, err := inspectorAccountSummary("binance", "main", account); err == nil || summary.Currency != "" {
		t.Fatalf("account without explicit balance asset must be rejected: summary=%+v err=%v", summary, err)
	}
	if summary, err := inspectorAccountSummary("binance", "main", nil); err == nil || summary.TotalBalance != 0 {
		t.Fatalf("nil exchange account must be rejected: summary=%+v err=%v", summary, err)
	}

	account.BalanceAsset = "USDT"
	account.TotalMarginBalance = math.NaN()
	if summary, err := inspectorAccountSummary("binance", "main", account); err == nil || summary.Currency != "" {
		t.Fatalf("non-finite exchange balance must be rejected: summary=%+v err=%v", summary, err)
	}
}
