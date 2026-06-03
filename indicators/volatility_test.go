package indicators

import "testing"

func TestATRBollingerAndChannels(t *testing.T) {
	candles := []Candle{
		{Open: 10, High: 12, Low: 9, Close: 11},
		{Open: 11, High: 13, Low: 10, Close: 12},
		{Open: 12, High: 14, Low: 11, Close: 13},
		{Open: 13, High: 15, Low: 12, Close: 14},
		{Open: 14, High: 16, Low: 13, Close: 15},
	}

	atr := NewATR(3)
	if atr.Name() != "ATR" || atr.Period() != 4 {
		t.Fatalf("unexpected ATR metadata: %s period=%d", atr.Name(), atr.Period())
	}
	assertFloatSlice(t, atr.Calculate(candles), []float64{3, 3})
	if got := atr.CurrentATR(candles); !almostEqual(got, 3) {
		t.Fatalf("CurrentATR() = %v, want 3", got)
	}
	if got := NewATR(10).Calculate(candles); got != nil {
		t.Fatalf("short ATR input = %v, want nil", got)
	}

	bb := NewBollingerBands(3, 2)
	bands := bb.CalculateMulti(candles)
	if bands == nil {
		t.Fatal("BollingerBands returned nil")
	}
	assertFloatSlice(t, bands["middle"], []float64{12, 13, 14})
	if len(bands["upper"]) != 3 || len(bands["lower"]) != 3 || len(bands["percent_b"]) != 3 {
		t.Fatalf("unexpected band lengths: %#v", bands)
	}
	assertFloatSlice(t, bb.Calculate(candles), bands["middle"])

	kc := NewKeltnerChannel(3, 3, 1.5)
	channel := kc.CalculateMulti(candles)
	if channel == nil {
		t.Fatal("KeltnerChannel returned nil")
	}
	assertFloatSlice(t, channel["middle"], []float64{13, 14})
	assertFloatSlice(t, channel["upper"], []float64{17.5, 18.5})
	assertFloatSlice(t, channel["lower"], []float64{8.5, 9.5})
}

func TestBollingerBandSignalBoundaries(t *testing.T) {
	bb := NewBollingerBands(3, 2)

	flatThenBreakdown := []Candle{
		{High: 10, Low: 10, Close: 10},
		{High: 10, Low: 10, Close: 10},
		{High: 10, Low: 10, Close: 10},
	}
	if got := bb.Signal(flatThenBreakdown); got != 1 {
		t.Fatalf("flat lower-band touch signal = %d, want buy", got)
	}

	trending := []Candle{
		{High: 8, Low: 8, Close: 8},
		{High: 10, Low: 10, Close: 10},
		{High: 12, Low: 12, Close: 12},
	}
	if got := bb.Signal(trending); got != 0 {
		t.Fatalf("normal trend signal = %d, want neutral", got)
	}
}
