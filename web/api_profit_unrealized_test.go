package web

import (
	"math"
	"testing"
)

type verifiedProfitPosition struct {
	providerInfraPosition
	pnl      float64
	verified bool
}

func (p *verifiedProfitPosition) GetVerifiedUnrealizedPnL(float64) (float64, bool) {
	return p.pnl, p.verified
}

func TestVerifiedProviderPnLRequiresScopedEvidence(t *testing.T) {
	slots := []SlotInfo{{Exchange: "binance", Symbol: "BTCUSDT"}}
	tests := []struct {
		name     string
		provider PositionManagerProvider
		exchange string
		price    float64
		wantPnL  float64
		wantOK   bool
	}{
		{name: "verified scoped value", provider: &verifiedProfitPosition{pnl: 12.5, verified: true}, exchange: "BINANCE", price: 101, wantPnL: 12.5, wantOK: true},
		{name: "legacy provider has no verification contract", provider: &providerInfraPosition{}, exchange: "binance", price: 101},
		{name: "exchange scope mismatch", provider: &verifiedProfitPosition{pnl: 12.5, verified: true}, exchange: "okx", price: 101},
		{name: "invalid market price", provider: &verifiedProfitPosition{pnl: 12.5, verified: true}, exchange: "binance", price: math.NaN()},
		{name: "provider says unverified", provider: &verifiedProfitPosition{pnl: 12.5}, exchange: "binance", price: 101},
		{name: "provider returns nonfinite pnl", provider: &verifiedProfitPosition{pnl: math.Inf(1), verified: true}, exchange: "binance", price: 101},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := verifiedProviderPnL(tt.provider, slots, tt.exchange, tt.price)
			if got != tt.wantPnL || ok != tt.wantOK {
				t.Fatalf("verifiedProviderPnL() = %v, %v; want %v, %v", got, ok, tt.wantPnL, tt.wantOK)
			}
		})
	}
}
