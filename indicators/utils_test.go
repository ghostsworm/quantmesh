package indicators

import (
	"math"
	"testing"
)

func TestMovingAveragesAndStatistics(t *testing.T) {
	values := []float64{1, 2, 3, 4, 5}

	assertFloatSlice(t, SMA(values, 3), []float64{2, 3, 4})
	assertFloatSlice(t, EMA(values, 3), []float64{2, 3, 4})
	assertFloatSlice(t, WMA(values, 3), []float64{2.3333333333, 3.3333333333, 4.3333333333})

	if got := Mean(values); almostEqual(got, 3) == false {
		t.Fatalf("Mean() = %v, want 3", got)
	}
	if got := Median([]float64{5, 1, 3, 2}); almostEqual(got, 2.5) == false {
		t.Fatalf("Median() = %v, want 2.5", got)
	}
	if got := Percentile([]float64{1, 2, 3, 4, 5}, 75); almostEqual(got, 4) == false {
		t.Fatalf("Percentile() = %v, want 4", got)
	}
	if got := StdDev([]float64{2, 4, 4, 4, 5, 5, 7, 9}, 8); len(got) != 1 || !almostEqual(got[0], 2) {
		t.Fatalf("StdDev() = %v, want [2]", got)
	}
}

func TestPriceSeriesHelpersAndCrossSignals(t *testing.T) {
	candles := []Candle{
		{Open: 9, High: 12, Low: 8, Close: 10, Volume: 100},
		{Open: 10, High: 15, Low: 9, Close: 14, Volume: 120},
		{Open: 14, High: 16, Low: 11, Close: 12, Volume: 90},
	}

	assertFloatSlice(t, ClosePrices(candles), []float64{10, 14, 12})
	assertFloatSlice(t, HighPrices(candles), []float64{12, 15, 16})
	assertFloatSlice(t, LowPrices(candles), []float64{8, 9, 11})
	assertFloatSlice(t, OpenPrices(candles), []float64{9, 10, 14})
	assertFloatSlice(t, Volumes(candles), []float64{100, 120, 90})
	assertFloatSlice(t, TypicalPrice(candles), []float64{10, 38.0 / 3.0, 13})
	assertFloatSlice(t, OHLC4(candles), []float64{9.75, 12, 13.25})
	assertFloatSlice(t, HL2(candles), []float64{10, 12, 13.5})
	assertFloatSlice(t, HighestHigh(candles, 2), []float64{15, 16})
	assertFloatSlice(t, LowestLow(candles, 2), []float64{8, 9})
	assertFloatSlice(t, TrueRangeSeries(candles), []float64{6, 5})

	if !CrossOver([]float64{1, 3}, []float64{2, 2}) {
		t.Fatal("expected CrossOver to detect upward cross")
	}
	if !CrossUnder([]float64{3, 1}, []float64{2, 2}) {
		t.Fatal("expected CrossUnder to detect downward cross")
	}
	if CrossOver([]float64{3}, []float64{2}) || CrossUnder([]float64{1}, []float64{2}) {
		t.Fatal("single-point series must not cross")
	}
}

func TestRateOfChangeDiffAndShift(t *testing.T) {
	values := []float64{10, 20, 40, 20}

	assertFloatSlice(t, RateOfChange(values, 1), []float64{100, 100, -50})
	assertFloatSlice(t, Diff(values, 2), []float64{30, 0})
	assertFloatSlice(t, Shift(values, 2), []float64{10, 20})

	if got := RateOfChange([]float64{1, 2}, 2); got != nil {
		t.Fatalf("RateOfChange short input = %v, want nil", got)
	}
	if got := Percentile(values, -1); got != 0 {
		t.Fatalf("invalid Percentile() = %v, want 0", got)
	}
}

func TestIndicatorRegistry(t *testing.T) {
	registry := NewIndicatorRegistry()
	registry.Register("rsi", func(params map[string]interface{}) Indicator {
		return NewRSI(int(params["period"].(float64)))
	})

	indicator := registry.Get("rsi", map[string]interface{}{"period": 14.0})
	if indicator == nil {
		t.Fatal("expected registered indicator")
	}
	if indicator.Name() != "RSI" || indicator.Period() != 15 {
		t.Fatalf("unexpected indicator metadata: %s period=%d", indicator.Name(), indicator.Period())
	}
	if registry.Get("missing", nil) != nil {
		t.Fatal("missing indicator should return nil")
	}
	if len(registry.List()) != 1 {
		t.Fatalf("registry list length = %d, want 1", len(registry.List()))
	}
}

func assertFloatSlice(t *testing.T, got, want []float64) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("length = %d, want %d; got %v", len(got), len(want), got)
	}
	for i := range want {
		if !almostEqual(got[i], want[i]) {
			t.Fatalf("index %d = %.12f, want %.12f; full got %v", i, got[i], want[i], got)
		}
	}
}

func almostEqual(a, b float64) bool {
	return math.Abs(a-b) < 1e-9
}
