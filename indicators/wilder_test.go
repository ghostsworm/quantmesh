package indicators

import (
	"math"
	"testing"
)

const wilderTestEps = 1e-9

func approxEqual(a, b, eps float64) bool {
	return math.Abs(a-b) <= eps
}

// linearCandles 生成每根 K 線按 step 平移、振幅固定为 2 的序列（收盘价在中点）
func linearCandles(n int, start, step float64) []Candle {
	out := make([]Candle, n)
	for i := 0; i < n; i++ {
		low := start + float64(i)*step
		out[i] = Candle{Time: int64(i), Open: low + 1, High: low + 2, Low: low, Close: low + 1}
	}
	return out
}

func TestRMA(t *testing.T) {
	tests := []struct {
		name   string
		values []float64
		period int
		want   []float64
	}{
		{name: "hand computed", values: []float64{1, 2, 3, 4, 5}, period: 3, want: []float64{2, 8.0 / 3, 31.0 / 9}},
		{name: "period equals length", values: []float64{2, 4}, period: 2, want: []float64{3}},
		{name: "insufficient data", values: []float64{1, 2}, period: 3, want: nil},
		{name: "zero period", values: []float64{1, 2}, period: 0, want: nil},
		{name: "negative period", values: []float64{1, 2}, period: -1, want: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RMA(tt.values, tt.period)
			if len(got) != len(tt.want) {
				t.Fatalf("len=%d want %d (%v)", len(got), len(tt.want), got)
			}
			for i := range got {
				if !approxEqual(got[i], tt.want[i], wilderTestEps) {
					t.Errorf("[%d]=%v want %v", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestWilderATR(t *testing.T) {
	tests := []struct {
		name     string
		candles  []Candle
		period   int
		wantLen  int
		wantLast float64
	}{
		{name: "constant range", candles: linearCandles(30, 100, 0), period: 14, wantLen: 16, wantLast: 2},
		{name: "trending constant true range", candles: linearCandles(30, 100, 1), period: 14, wantLen: 16, wantLast: 2},
		{name: "insufficient", candles: linearCandles(14, 100, 0), period: 14, wantLen: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := WilderATR(tt.candles, tt.period)
			if len(got) != tt.wantLen {
				t.Fatalf("len=%d want %d", len(got), tt.wantLen)
			}
			if tt.wantLen > 0 && !approxEqual(got[len(got)-1], tt.wantLast, wilderTestEps) {
				t.Errorf("last=%v want %v", got[len(got)-1], tt.wantLast)
			}
		})
	}
}

func TestWilderADX(t *testing.T) {
	zigzag := make([]Candle, 60)
	for i := range zigzag {
		low := 100.0
		if i%2 == 1 {
			low = 101
		}
		zigzag[i] = Candle{Time: int64(i), Open: low + 1, High: low + 2, Low: low, Close: low + 1}
	}

	tests := []struct {
		name      string
		candles   []Candle
		period    int
		wantNil   bool
		wantLen   int
		checkLast func(adx, pdi, mdi float64) bool
	}{
		{
			name: "pure uptrend", candles: linearCandles(60, 100, 1), period: 14, wantLen: 60 - 28 + 1,
			checkLast: func(adx, pdi, mdi float64) bool {
				return approxEqual(adx, 100, 1e-6) && approxEqual(pdi, 50, 1e-6) && mdi == 0
			},
		},
		{
			name: "pure downtrend", candles: linearCandles(60, 200, -1), period: 14, wantLen: 60 - 28 + 1,
			checkLast: func(adx, pdi, mdi float64) bool {
				return approxEqual(adx, 100, 1e-6) && pdi == 0 && approxEqual(mdi, 50, 1e-6)
			},
		},
		{
			name: "zigzag range", candles: zigzag, period: 14, wantLen: 60 - 28 + 1,
			checkLast: func(adx, pdi, mdi float64) bool { return adx < 20 },
		},
		{name: "minimum length", candles: linearCandles(28, 100, 1), period: 14, wantLen: 1},
		{name: "insufficient", candles: linearCandles(27, 100, 1), period: 14, wantNil: true},
		{name: "invalid period", candles: linearCandles(27, 100, 1), period: 0, wantNil: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			adx, pdi, mdi := WilderADX(tt.candles, tt.period)
			if tt.wantNil {
				if adx != nil || pdi != nil || mdi != nil {
					t.Fatalf("want nil, got len %d", len(adx))
				}
				return
			}
			if len(adx) != tt.wantLen || len(pdi) != tt.wantLen || len(mdi) != tt.wantLen {
				t.Fatalf("len adx=%d pdi=%d mdi=%d want %d", len(adx), len(pdi), len(mdi), tt.wantLen)
			}
			n := tt.wantLen - 1
			if tt.checkLast != nil && !tt.checkLast(adx[n], pdi[n], mdi[n]) {
				t.Errorf("unexpected last values adx=%v +di=%v -di=%v", adx[n], pdi[n], mdi[n])
			}
		})
	}
}

func TestPercentileRank(t *testing.T) {
	tests := []struct {
		name   string
		values []float64
		x      float64
		want   float64
	}{
		{name: "middle", values: []float64{1, 2, 3, 4}, x: 3, want: 62.5},
		{name: "max unique", values: []float64{1, 2, 3, 4}, x: 5, want: 100},
		{name: "min below", values: []float64{1, 2, 3, 4}, x: 0, want: 0},
		{name: "all equal", values: []float64{2, 2, 2}, x: 2, want: 50},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := PercentileRank(tt.values, tt.x); !approxEqual(got, tt.want, wilderTestEps) {
				t.Errorf("got %v want %v", got, tt.want)
			}
		})
	}
	if !math.IsNaN(PercentileRank(nil, 1)) {
		t.Error("empty values should yield NaN")
	}
	if !math.IsNaN(PercentileRank([]float64{1}, math.NaN())) {
		t.Error("NaN x should yield NaN")
	}
}
