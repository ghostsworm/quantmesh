package main

import (
	"math"
	"testing"
)

func TestCapStrategyCapitalLimit(t *testing.T) {
	tests := []struct {
		name       string
		configured float64
		available  float64
		want       float64
		wantErr    bool
	}{
		{name: "configured below available", configured: 200, available: 500, want: 200},
		{name: "configured above available", configured: 5000, available: 200, want: 200},
		{name: "zero configuration uses verified available", configured: 0, available: 200, want: 200},
		{name: "no available funds", configured: 200, available: 0, wantErr: true},
		{name: "negative configured cap", configured: -1, available: 200, wantErr: true},
		{name: "non-finite configured cap", configured: math.NaN(), available: 200, wantErr: true},
		{name: "non-finite available balance", configured: 200, available: math.Inf(1), wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := capStrategyCapitalLimit(tt.configured, tt.available)
			if (err != nil) != tt.wantErr {
				t.Fatalf("error = %v, wantErr %v", err, tt.wantErr)
			}
			if err == nil && got != tt.want {
				t.Fatalf("capStrategyCapitalLimit() = %v, want %v", got, tt.want)
			}
		})
	}
}
