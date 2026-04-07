package config

import "testing"

func TestGenerateBotIDFundingPerpSpread_orderStable(t *testing.T) {
	fp := &FundingPerpSpreadConfig{
		LegA: FundingPerpLeg{Exchange: "okx", Symbol: "BTC-USDT-SWAP"},
		LegB: FundingPerpLeg{Exchange: "binance", Symbol: "BTCUSDT"},
	}
	fp2 := &FundingPerpSpreadConfig{
		LegA: FundingPerpLeg{Exchange: "binance", Symbol: "BTCUSDT"},
		LegB: FundingPerpLeg{Exchange: "okx", Symbol: "BTC-USDT-SWAP"},
	}
	if GenerateBotIDFundingPerpSpread(fp) != GenerateBotIDFundingPerpSpread(fp2) {
		t.Fatal("leg order should not change generated id")
	}
}

func TestValidateFundingPerpSpread_sameLeg(t *testing.T) {
	err := ValidateFundingPerpSpread(&FundingPerpSpreadConfig{
		LegA: FundingPerpLeg{Exchange: "binance", Symbol: "BTCUSDT"},
		LegB: FundingPerpLeg{Exchange: "binance", Symbol: "BTCUSDT"},
	})
	if err == nil {
		t.Fatal("expected error for identical legs")
	}
}
