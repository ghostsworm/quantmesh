package strategy

import (
	"math"
	"testing"
)

func TestCommissionInQuoteRejectsNonFiniteValues(t *testing.T) {
	ex := &hedgeExchange{}
	tests := []struct {
		name       string
		commission float64
		asset      string
		price      float64
	}{
		{name: "nan quote fee", commission: math.NaN(), asset: "USDT", price: 100},
		{name: "infinite quote fee", commission: math.Inf(1), asset: "USDT", price: 100},
		{name: "overflowing base fee conversion", commission: math.MaxFloat64, asset: "BTC", price: 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if converted, ok := commissionInQuote(ex, tt.commission, tt.asset, tt.price); ok || converted != 0 {
				t.Fatalf("commissionInQuote() = %v, %v; want rejected non-finite valuation", converted, ok)
			}
		})
	}
}
