package web

import (
	"math"
	"strings"
	"testing"
)

type verifiedProfitPosition struct {
	providerInfraPosition
	pnl      float64
	verified bool
	asset    string
}

func (p *verifiedProfitPosition) GetVerifiedUnrealizedPnL(float64) (float64, bool) {
	return p.pnl, p.verified
}

func (p *verifiedProfitPosition) GetVerifiedUnrealizedPnLForAsset(_ float64, asset string) (float64, bool) {
	return p.pnl, p.verified && strings.EqualFold(p.asset, asset)
}

func TestVerifiedProviderPnLRequiresScopedEvidence(t *testing.T) {
	slots := []SlotInfo{{Exchange: "binance", Symbol: "BTCUSDT"}}
	tests := []struct {
		name     string
		provider PositionManagerProvider
		exchange string
		asset    string
		price    float64
		wantPnL  float64
		wantOK   bool
	}{
		{name: "verified scoped value and asset", provider: &verifiedProfitPosition{pnl: 12.5, verified: true, asset: "USDT"}, exchange: "BINANCE", asset: "USDT", price: 101, wantPnL: 12.5, wantOK: true},
		{name: "legacy provider has no verification contract", provider: &providerInfraPosition{}, exchange: "binance", price: 101},
		{name: "exchange scope mismatch", provider: &verifiedProfitPosition{pnl: 12.5, verified: true, asset: "USDT"}, exchange: "okx", asset: "USDT", price: 101},
		{name: "asset scope mismatch", provider: &verifiedProfitPosition{pnl: 12.5, verified: true, asset: "BTC"}, exchange: "binance", asset: "USDT", price: 101},
		{name: "invalid market price", provider: &verifiedProfitPosition{pnl: 12.5, verified: true, asset: "USDT"}, exchange: "binance", asset: "USDT", price: math.NaN()},
		{name: "provider says unverified", provider: &verifiedProfitPosition{pnl: 12.5}, exchange: "binance", price: 101},
		{name: "provider returns nonfinite pnl", provider: &verifiedProfitPosition{pnl: math.Inf(1), verified: true, asset: "USDT"}, exchange: "binance", asset: "USDT", price: 101},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := verifiedProviderPnL(tt.provider, slots, tt.exchange, tt.asset, tt.price)
			if got != tt.wantPnL || ok != tt.wantOK {
				t.Fatalf("verifiedProviderPnL() = %v, %v; want %v, %v", got, ok, tt.wantPnL, tt.wantOK)
			}
		})
	}
}
