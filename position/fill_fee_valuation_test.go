package position

import (
	"math"
	"testing"
)

func TestSummarizeFillsUsesOnlyVerifiedQuoteValuation(t *testing.T) {
	t.Run("quote asset", func(t *testing.T) {
		sum, _ := summarizeFills([]*fakeFill{{Commission: 0.25, CommissionAsset: "USDT"}}, "USDT", "BTC")
		if !sum.valuationKnown || sum.commission != 0.25 || sum.asset != "USDT" {
			t.Fatalf("quote fee summary = %+v", sum)
		}
	})
	t.Run("base asset", func(t *testing.T) {
		sum, _ := summarizeFills([]*fakeFill{{Commission: 0.001, CommissionAsset: "BTC", Price: 100}}, "USDT", "BTC")
		if !sum.valuationKnown || math.Abs(sum.commission-0.1) > 1e-12 || sum.asset != "USDT" {
			t.Fatalf("base fee summary = %+v", sum)
		}
	})
	t.Run("verified third asset", func(t *testing.T) {
		fill := &fakeFill{Commission: 0.001, CommissionAsset: "BNB", CommissionQuote: 0.6, CommissionQuoteKnown: true}
		sum, _ := summarizeFills([]*fakeFill{fill}, "USDT", "BTC")
		if !sum.valuationKnown || sum.commission != 0.6 || sum.asset != "USDT" {
			t.Fatalf("historically converted fee summary = %+v", sum)
		}
	})
	t.Run("unknown third asset", func(t *testing.T) {
		sum, _ := summarizeFills([]*fakeFill{{Commission: 0.001, CommissionAsset: "BNB"}}, "USDT", "BTC")
		if sum.valuationKnown || sum.commission != 0 || sum.asset != "BNB" {
			t.Fatalf("unknown fee must not be treated as quote value: %+v", sum)
		}
	})
}

func TestGridFeeValuationRejectsUnknownAssetsAndKeepsRebatesSigned(t *testing.T) {
	spm := newFillFeeSPM(t, "spot", nil)
	if got, ok := spm.commissionInQuote(OrderUpdate{Commission: -0.25, CommissionAsset: "USDT"}, 100); !ok || got != -0.25 {
		t.Fatalf("quote rebate = %v, %v", got, ok)
	}
	if got, ok := spm.commissionInQuote(OrderUpdate{Commission: 0.001, CommissionAsset: "BTC"}, 100); !ok || got != 0.1 {
		t.Fatalf("base commission = %v, %v", got, ok)
	}
	if _, ok := spm.commissionInQuote(OrderUpdate{Commission: 0.01, CommissionAsset: "BNB"}, 100); ok {
		t.Fatal("third asset without historical evidence must not be treated as quote value")
	}
}
