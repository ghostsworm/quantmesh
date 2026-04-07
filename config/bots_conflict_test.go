package config

import "testing"

func TestBotsShareFuturesLeg_fundingPerpSpread(t *testing.T) {
	a := &BotConfig{
		MarketType: MarketTypeFundingPerpSpread,
		FundingPerpSpread: &FundingPerpSpreadConfig{
			LegA: FundingPerpLeg{Exchange: "binance", Symbol: "BTCUSDT"},
			LegB: FundingPerpLeg{Exchange: "okx", Symbol: "BTC-USDT-SWAP"},
		},
	}
	b := &BotConfig{
		Exchange:   "binance",
		Symbol:     "BTCUSDT",
		MarketType: "futures",
	}
	if !BotsShareFuturesLeg(a, b) {
		t.Fatal("expected overlap on binance BTCUSDT")
	}
	c := &BotConfig{
		Exchange:   "bybit",
		Symbol:     "BTCUSDT",
		MarketType: "futures",
	}
	if BotsShareFuturesLeg(a, c) {
		t.Fatal("expected no overlap")
	}
}

func TestCarriesFundingCarryConflict_vsFundingPerpLeg(t *testing.T) {
	carry := &BotConfig{
		Exchange:   "binance",
		Symbol:     "BTCUSDT",
		MarketType: MarketTypeFundingCarry,
	}
	perp := &BotConfig{
		Exchange:   "okx",
		Symbol:     "BTC-USDT-SWAP",
		MarketType: MarketTypeFundingPerpSpread,
		FundingPerpSpread: &FundingPerpSpreadConfig{
			LegA: FundingPerpLeg{Exchange: "okx", Symbol: "BTC-USDT-SWAP"},
			LegB: FundingPerpLeg{Exchange: "gate", Symbol: "BTC_USDT"},
		},
	}
	// carry 為 binance BTCUSDT；雙永续兩腿為 okx+gate，與 carry 無期貨腿重疊
	if CarriesFundingCarryConflict(carry, perp) {
		t.Fatal("carry should not conflict when legs do not touch")
	}
	perp2 := &BotConfig{
		Exchange:   "binance",
		Symbol:     "BTCUSDT",
		MarketType: MarketTypeFundingPerpSpread,
		FundingPerpSpread: &FundingPerpSpreadConfig{
			LegA: FundingPerpLeg{Exchange: "binance", Symbol: "BTCUSDT"},
			LegB: FundingPerpLeg{Exchange: "okx", Symbol: "BTC-USDT-SWAP"},
		},
	}
	if !CarriesFundingCarryConflict(carry, perp2) {
		t.Fatal("carry should conflict when perp touches carry futures leg")
	}
}

func TestBotsConflict_carryVsSpotSameSymbol(t *testing.T) {
	carry := &BotConfig{
		Exchange:   "binance",
		Symbol:     "BTCUSDT",
		MarketType: MarketTypeFundingCarry,
	}
	spot := &BotConfig{
		Exchange:   "binance",
		Symbol:     "BTCUSDT",
		MarketType: "spot",
	}
	if !BotsConflict(carry, spot) {
		t.Fatal("carry vs spot same exchange+symbol should conflict")
	}
}
