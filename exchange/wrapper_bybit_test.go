package exchange

import (
	"testing"

	"quantmesh/exchange/bybit"
)

func TestBybitWrapperAdvertisesFundingIncomeHistory(t *testing.T) {
	if !(&bybitWrapper{}).SupportsFundingIncomeHistory() {
		t.Fatal("Bybit wrapper must enable authenticated funding-income synchronization")
	}
}

func TestBybitOrderFillOnlySetsPnLAssetWhenPnLIsKnown(t *testing.T) {
	row := &bybit.BybitOrderFill{
		OrderID: 19, TradeID: "exec-19", Symbol: "BTCUSDT", Side: "Buy",
		Price: 100, Quantity: 0.1, Commission: 0.01, CommissionAsset: "USDT",
	}

	unknown := bybitOrderFillToExchange(row, SideBuy)
	if unknown.RealizedPnLKnown || unknown.RealizedPnLAsset != "" {
		t.Fatalf("unknown execution PnL mapping=%+v, want no PnL asset", unknown)
	}

	row.RealizedPnL = 2.5
	row.RealizedPnLKnown = true
	row.RealizedPnLAsset = "USDT"
	known := bybitOrderFillToExchange(row, SideBuy)
	if !known.RealizedPnLKnown || known.RealizedPnL != 2.5 || known.RealizedPnLAsset != "USDT" {
		t.Fatalf("known execution PnL mapping=%+v, want known USDT PnL", known)
	}
	row.CommissionAsset = "BNB"
	if got := bybitOrderFillToExchange(row, SideBuy).RealizedPnLAsset; got != "USDT" {
		t.Fatalf("PnL currency must come from contract settlement metadata, not fee currency; got %q", got)
	}
}
