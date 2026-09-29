package web

import (
	"math"
	"testing"

	"quantmesh/exchange"
)

func TestNormalizeMarketPriceRejectsNonFiniteAndNonPositiveValues(t *testing.T) {
	tests := []struct {
		name  string
		input float64
		want  float64
	}{
		{name: "valid", input: 100, want: 100},
		{name: "zero"},
		{name: "negative", input: -1},
		{name: "nan", input: math.NaN()},
		{name: "positive infinity", input: math.Inf(1)},
		{name: "negative infinity", input: math.Inf(-1)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeMarketPrice(tt.input); got != tt.want {
				t.Fatalf("normalizeMarketPrice(%v) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestSummarizeExchangePositionsFailsClosedOnMalformedExposure(t *testing.T) {
	valid := func(size, pnl float64) *exchange.Position {
		return &exchange.Position{Size: size, EntryPrice: 100, MarkPrice: 101, UnrealizedPNL: pnl, Leverage: 5}
	}
	tests := []struct {
		name      string
		positions []*exchange.Position
		wantData  bool
		wantValid bool
	}{
		{name: "valid position", positions: []*exchange.Position{valid(2, 3)}, wantData: true, wantValid: true},
		{name: "hedge legs net to zero", positions: []*exchange.Position{valid(2, 3), valid(-2, -2)}, wantData: true, wantValid: true},
		{name: "empty snapshot", wantValid: false},
		{name: "nil position", positions: []*exchange.Position{nil}, wantData: true, wantValid: false},
		{name: "nan size", positions: []*exchange.Position{valid(math.NaN(), 3)}, wantData: true, wantValid: false},
		{name: "infinite pnl", positions: []*exchange.Position{valid(2, math.Inf(1))}, wantData: true, wantValid: false},
		{name: "invalid mark", positions: []*exchange.Position{{Size: 2, EntryPrice: 100, MarkPrice: math.NaN()}}, wantData: true, wantValid: false},
		{name: "overflow aggregate", positions: []*exchange.Position{valid(math.MaxFloat64, 1), valid(math.MaxFloat64, 1)}, wantData: true, wantValid: false},
		{name: "zero size ignored", positions: []*exchange.Position{valid(0, math.NaN())}, wantValid: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := summarizeExchangePositions(tt.positions)
			if got.HasData != tt.wantData || got.Verified != tt.wantValid {
				t.Fatalf("snapshot data/verified = %v/%v, want %v/%v", got.HasData, got.Verified, tt.wantData, tt.wantValid)
			}
			for _, value := range []float64{got.Quantity, got.UnrealizedPnL, got.MarkPrice, got.EntryPrice} {
				if !isFiniteNumber(value) {
					t.Fatalf("snapshot contains non-finite value: %+v", got)
				}
			}
		})
	}
}

func TestSlotUnrealizedPnLRespectsPositionDirection(t *testing.T) {
	tests := []struct {
		name        string
		direction   string
		leg         string
		current     float64
		entryFee    float64
		feeUnknown  bool
		pendingFees int
		want        float64
		verified    bool
	}{
		{name: "long gain", direction: "LONG", current: 110, want: 10, verified: true},
		{name: "long loss", direction: "LONG", current: 90, want: -10, verified: true},
		{name: "short gain", direction: "SHORT", current: 90, want: 10, verified: true},
		{name: "short loss", direction: "SHORT", current: 110, want: -10, verified: true},
		{name: "both short leg", direction: "BOTH", leg: "SHORT", current: 90, want: 10, verified: true},
		{name: "both long leg", direction: "BOTH", leg: "LONG", current: 90, want: -10, verified: true},
		{name: "both missing leg", direction: "BOTH", current: 90, want: 0},
		{name: "entry fee deducted", direction: "LONG", current: 110, entryFee: 0.5, want: 9.5, verified: true},
		{name: "entry fee unknown", direction: "LONG", current: 110, feeUnknown: true},
		{name: "entry fee pending", direction: "LONG", current: 110, pendingFees: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			slot := SlotInfo{PositionLeg: tt.leg, BuyFee: tt.entryFee, FeeValuationUnknown: tt.feeUnknown, PendingFeeSupplements: tt.pendingFees}
			got, verified := positionSlotUnrealizedPnL(tt.current, 100, 1, slot, tt.direction)
			if got != tt.want {
				t.Fatalf("unrealized pnl = %v, want %v", got, tt.want)
			}
			if verified != tt.verified {
				t.Fatalf("unrealized pnl verified = %v, want %v", verified, tt.verified)
			}
		})
	}
}
