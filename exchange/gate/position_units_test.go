package gate

import (
	"math"
	"testing"
)

func TestGateBaseQuantityFromContracts(t *testing.T) {
	tests := []struct {
		name       string
		contracts  int64
		multiplier float64
		want       float64
		wantErr    bool
	}{
		{name: "long contracts", contracts: 1200, multiplier: 0.0001, want: 0.12},
		{name: "short contracts", contracts: -1200, multiplier: 0.0001, want: -0.12},
		{name: "missing multiplier", contracts: 1, wantErr: true},
		{name: "non-finite multiplier", contracts: 1, multiplier: math.Inf(1), wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := gateBaseQuantityFromContracts(tc.contracts, tc.multiplier)
			if (err != nil) != tc.wantErr {
				t.Fatalf("error = %v, wantErr %t", err, tc.wantErr)
			}
			if err == nil && math.Abs(got-tc.want) > 1e-12 {
				t.Fatalf("base quantity = %.12g, want %.12g", got, tc.want)
			}
		})
	}
}

func TestGateContractsFromBaseQuantityNeverRoundsUp(t *testing.T) {
	tests := []struct {
		name       string
		quantity   float64
		multiplier float64
		want       int64
		wantErr    bool
	}{
		{name: "exact contracts", quantity: 0.12, multiplier: 0.0001, want: 1200},
		{name: "fractional contract floors", quantity: 0.12009, multiplier: 0.0001, want: 1200},
		{name: "less than one contract rejected", quantity: 0.00009, multiplier: 0.0001, wantErr: true},
		{name: "invalid multiplier rejected", quantity: 1, wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := gateContractsFromBaseQuantity(tc.quantity, tc.multiplier)
			if (err != nil) != tc.wantErr {
				t.Fatalf("error = %v, wantErr %t", err, tc.wantErr)
			}
			if err == nil && got != tc.want {
				t.Fatalf("contracts = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestCalculateDecimalPlacesHandlesNonPowerOfTenSteps(t *testing.T) {
	for value, want := range map[float64]int{0.0001: 4, 0.025: 3, 2.5: 1, 10: 0} {
		if got := calculateDecimalPlaces(value); got != want {
			t.Errorf("calculateDecimalPlaces(%v) = %d, want %d", value, got, want)
		}
	}
}
