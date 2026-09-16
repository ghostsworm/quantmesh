package regime

import (
	"math"
	"testing"
)

const intervalTestEps = 1e-9

func TestAdaptiveInterval(t *testing.T) {
	inf := math.Inf(1)
	tests := []struct {
		name                       string
		atr, k, base, lo, hi, want float64
	}{
		{name: "exact multiple", atr: 40, k: 0.5, base: 5, lo: 5, hi: 100, want: 20},
		{name: "rounds down to nearest multiple", atr: 44, k: 0.5, base: 5, lo: 5, hi: 100, want: 20},
		{name: "rounds up to nearest multiple", atr: 46, k: 0.5, base: 5, lo: 5, hi: 100, want: 25},
		{name: "clamped to min", atr: 1, k: 0.5, base: 5, lo: 15, hi: 100, want: 15},
		{name: "clamped to max", atr: 1000, k: 0.5, base: 5, lo: 5, hi: 40, want: 40},
		{name: "min not multiple rounds up", atr: 1, k: 1, base: 5, lo: 12, hi: 100, want: 15},
		{name: "max not multiple rounds down", atr: 1000, k: 1, base: 5, lo: 5, hi: 38, want: 35},
		{name: "no multiple inside range prefers wider", atr: 13, k: 1, base: 5, lo: 11, hi: 14, want: 15},
		{name: "at least one base", atr: 1, k: 0.1, base: 5, lo: 0, hi: 0, want: 5},
		{name: "unbounded max", atr: 1000, k: 1, base: 5, lo: 0, hi: 0, want: 1000},
		{name: "max below min uses min", atr: 1000, k: 1, base: 5, lo: 20, hi: 10, want: 20},
		{name: "zero atr falls back to base", atr: 0, k: 0.5, base: 5, lo: 0, hi: 0, want: 5},
		{name: "nan atr falls back to base", atr: math.NaN(), k: 0.5, base: 5, lo: 10, hi: 0, want: 10},
		{name: "inf atr falls back to base", atr: inf, k: 0.5, base: 5, lo: 0, hi: 0, want: 5},
		{name: "zero multiplier falls back", atr: 50, k: 0, base: 5, lo: 0, hi: 0, want: 5},
		{name: "invalid base", atr: 50, k: 1, base: 0, lo: 0, hi: 0, want: 0},
		{name: "nan base", atr: 50, k: 1, base: math.NaN(), lo: 0, hi: 0, want: 0},
		{name: "fractional base float noise", atr: 0.3, k: 1, base: 0.1, lo: 0.1, hi: 1, want: 0.3},
		{name: "fractional base min exact", atr: 0.01, k: 1, base: 0.1, lo: 0.3, hi: 1, want: 0.3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := AdaptiveInterval(tt.atr, tt.k, tt.base, tt.lo, tt.hi)
			if math.Abs(got-tt.want) > intervalTestEps {
				t.Fatalf("got %v want %v", got, tt.want)
			}
			if tt.base > 0 && got > 0 {
				n := got / tt.base
				if math.Abs(n-math.Round(n)) > 1e-6 {
					t.Fatalf("result %v is not a multiple of base %v", got, tt.base)
				}
			}
		})
	}
}

func TestQuantizeInterval(t *testing.T) {
	tests := []struct {
		name                 string
		interval, base, want float64
	}{
		{name: "scale 1.5 of 20 by 5", interval: 30, base: 5, want: 30},
		{name: "rounds", interval: 32.4, base: 5, want: 30},
		{name: "below base", interval: 1, base: 5, want: 5},
		{name: "invalid interval", interval: -1, base: 5, want: 5},
		{name: "invalid base", interval: 10, base: 0, want: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := QuantizeInterval(tt.interval, tt.base); math.Abs(got-tt.want) > intervalTestEps {
				t.Fatalf("got %v want %v", got, tt.want)
			}
		})
	}
}

func TestShouldChangeInterval(t *testing.T) {
	tests := []struct {
		name                     string
		current, proposed, ratio float64
		want                     bool
	}{
		{name: "equal", current: 20, proposed: 20, ratio: 0.2, want: false},
		{name: "below threshold", current: 20, proposed: 23, ratio: 0.2, want: false},
		{name: "exactly threshold", current: 20, proposed: 24, ratio: 0.2, want: true},
		{name: "above threshold down", current: 20, proposed: 15, ratio: 0.2, want: true},
		{name: "unset current", current: 0, proposed: 15, ratio: 0.2, want: true},
		{name: "invalid proposed", current: 20, proposed: 0, ratio: 0.2, want: false},
		{name: "nan proposed", current: 20, proposed: math.NaN(), ratio: 0.2, want: false},
		{name: "zero ratio any change", current: 20, proposed: 20.5, ratio: 0, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ShouldChangeInterval(tt.current, tt.proposed, tt.ratio); got != tt.want {
				t.Fatalf("got %v want %v", got, tt.want)
			}
		})
	}
}

func TestAdaptiveIntervalConfigNext(t *testing.T) {
	enabled := AdaptiveIntervalConfig{Enabled: true, ATRMultiplier: 0.5, ChangeThresholdRatio: 0.25}
	tests := []struct {
		name                     string
		cfg                      AdaptiveIntervalConfig
		current, atr, base, want float64
	}{
		{name: "disabled returns base", cfg: AdaptiveIntervalConfig{ATRMultiplier: 0.5}, current: 40, atr: 100, base: 5, want: 5},
		{name: "first set", cfg: enabled, current: 0, atr: 40, base: 5, want: 20},
		{name: "same quantized value", cfg: enabled, current: 20, atr: 44, base: 5, want: 20},
		{name: "change at threshold applied", cfg: enabled, current: 20, atr: 46, base: 5, want: 25},
		{name: "small change ignored", cfg: AdaptiveIntervalConfig{Enabled: true, ATRMultiplier: 0.5, ChangeThresholdRatio: 0.3}, current: 20, atr: 46, base: 5, want: 20},
		{name: "large change applied", cfg: enabled, current: 20, atr: 60, base: 5, want: 30},
		{name: "default max is 8x base", cfg: enabled, current: 20, atr: 10000, base: 5, want: 40},
		{name: "explicit bounds", cfg: AdaptiveIntervalConfig{Enabled: true, ATRMultiplier: 1, MinInterval: 10, MaxInterval: 100, ChangeThresholdRatio: 0.1}, current: 50, atr: 1, base: 5, want: 10},
		{name: "invalid base", cfg: enabled, current: 20, atr: 40, base: 0, want: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cfg.Next(tt.current, tt.atr, tt.base); math.Abs(got-tt.want) > intervalTestEps {
				t.Fatalf("got %v want %v", got, tt.want)
			}
		})
	}
}

func TestAlignToAnchor(t *testing.T) {
	tests := []struct {
		name                          string
		price, anchor, interval, want float64
	}{
		{name: "on slot", price: 3010, anchor: 3000, interval: 10, want: 3010},
		{name: "between slots", price: 3017, anchor: 3000, interval: 10, want: 3010},
		{name: "below anchor", price: 2985, anchor: 3000, interval: 10, want: 2980},
		{name: "float slot", price: 0.3, anchor: 0, interval: 0.1, want: 0.3},
		{name: "invalid interval", price: 3017, anchor: 3000, interval: 0, want: 3017},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := AlignToAnchor(tt.price, tt.anchor, tt.interval); math.Abs(got-tt.want) > intervalTestEps {
				t.Fatalf("got %v want %v", got, tt.want)
			}
		})
	}
}
