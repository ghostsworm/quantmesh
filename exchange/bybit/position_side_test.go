package bybit

import (
	"math"
	"testing"
)

func TestNormalizeBybitPositionSide(t *testing.T) {
	tests := []struct {
		name      string
		index     string
		side      string
		size      float64
		wantSize  float64
		wantSide  string
		wantError bool
	}{
		{name: "one-way long", index: "0", side: "Buy", size: 0.4, wantSize: 0.4, wantSide: "NET"},
		{name: "one-way short is signed negative", index: "0", side: "Sell", size: 0.3, wantSize: -0.3, wantSide: "NET"},
		{name: "empty one-way position", index: "0", size: 0, wantSide: "NET"},
		{name: "hedge long leg", index: "1", side: "Buy", size: 0.4, wantSize: 0.4, wantSide: "LONG"},
		{name: "empty hedge long leg", index: "1", size: 0, wantSide: "LONG"},
		{name: "hedge short leg", index: "2", side: "Sell", size: 0.3, wantSize: 0.3, wantSide: "SHORT"},
		{name: "one-way side missing", index: "0", size: 0.1, wantError: true},
		{name: "long index with sell side", index: "1", side: "Sell", size: 0.1, wantError: true},
		{name: "short index with buy side", index: "2", side: "Buy", size: 0.1, wantError: true},
		{name: "unsupported index", index: "3", size: 0.1, wantError: true},
		{name: "non-finite size", index: "0", side: "Buy", size: math.NaN(), wantError: true},
		{name: "negative raw size", index: "0", side: "Sell", size: -0.1, wantError: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			gotSize, gotSide, err := normalizeBybitPositionSide(test.index, test.side, test.size, "BTCUSDT")
			if (err != nil) != test.wantError {
				t.Fatalf("normalizeBybitPositionSide() error = %v, wantError %v", err, test.wantError)
			}
			if err != nil {
				return
			}
			if gotSize != test.wantSize || gotSide != test.wantSide {
				t.Fatalf("normalizeBybitPositionSide() = (%v, %q), want (%v, %q)", gotSize, gotSide, test.wantSize, test.wantSide)
			}
		})
	}
}
