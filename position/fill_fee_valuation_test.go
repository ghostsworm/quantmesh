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

func TestGridOrderRejectsOverflowingOpeningFeeBeforeAdvancingFillCursor(t *testing.T) {
	spm := newFillFeeSPM(t, "futures", nil)
	spm.executor = &tradeLedgerHoldTestExecutor{}
	clientOID := openBuy(spm, 91)
	spm.OnOrderUpdate(OrderUpdate{OrderID: 91, ClientOrderID: clientOID, Symbol: "ETHUSDT", Status: "PARTIALLY_FILLED", Side: "BUY",
		ExecutedQty: 0.1, AvgPrice: 100, Commission: math.MaxFloat64, CommissionAsset: "USDT"})
	spm.OnOrderUpdate(OrderUpdate{OrderID: 91, ClientOrderID: clientOID, Symbol: "ETHUSDT", Status: "PARTIALLY_FILLED", Side: "BUY",
		ExecutedQty: 0.2, AvgPrice: 100, Commission: math.MaxFloat64, CommissionAsset: "USDT"})

	slot := spm.getOrCreateSlot(fillFeeTestPrice)
	slot.mu.RLock()
	defer slot.mu.RUnlock()
	if slot.OrderFilledQty != 0.1 || slot.PositionQty != 0.1 || slot.BuyFee != math.MaxFloat64 {
		t.Fatalf("overflowing fill advanced/mutated ledger: cursor=%v position=%v fee=%v", slot.OrderFilledQty, slot.PositionQty, slot.BuyFee)
	}
	if slot.OrderStatus != OrderStatusUnknown || slot.SlotStatus != SlotStatusLocked || !spm.OpeningGate().HasBlock("trade_ledger_unverified") {
		t.Fatalf("overflowing fill did not enter reconciliation hold: order=%s slot=%s", slot.OrderStatus, slot.SlotStatus)
	}
}

func TestSummarizeFillsRejectsOverflowAndInvalidFillEconomics(t *testing.T) {
	tests := []struct {
		name  string
		fills []*fakeFill
	}{
		{
			name: "commission aggregate overflow",
			fills: []*fakeFill{
				{Commission: math.MaxFloat64, CommissionAsset: "USDT"},
				{Commission: math.MaxFloat64, CommissionAsset: "USDT"},
			},
		},
		{
			name:  "notional multiplication overflow",
			fills: []*fakeFill{{Price: math.MaxFloat64, Quantity: 2}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sum, _ := summarizeFills(tt.fills, "USDT", "BTC")
			if sum.valid && sum.valuationKnown {
				t.Fatalf("invalid fill summary accepted: %+v", sum)
			}
		})
	}
}

func TestSummarizeFillsRejectsBaseFeeAboveFillQuantity(t *testing.T) {
	sum, _ := summarizeFills([]*detailedFill{{Price: 1, Quantity: 1, BaseFeeQty: 2}}, "USDT", "BTC")
	if sum.valid {
		t.Fatalf("base fee above fill quantity accepted: %+v", sum)
	}
}
