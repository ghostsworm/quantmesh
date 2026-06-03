package indicators

import "testing"

func TestTrendIndicatorsCalculateMultiAndSignals(t *testing.T) {
	up := indicatorCandles(80, 100, 1.2, 1.0)
	down := indicatorCandles(80, 180, -1.2, 1.0)

	macd := NewMACD(5, 10, 4)
	if macd.Name() != "MACD" || macd.Period() != 14 {
		t.Fatalf("unexpected MACD metadata: %s period=%d", macd.Name(), macd.Period())
	}
	macdValues := macd.CalculateMulti(up)
	assertAlignedSeries(t, macdValues, "macd", "signal", "histogram")
	if macd.Calculate(up)[0] != macdValues["macd"][0] {
		t.Fatal("MACD Calculate should return macd series")
	}
	if got := macd.CalculateMulti(up[:5]); got != nil {
		t.Fatalf("short MACD result = %v, want nil", got)
	}
	if got := macd.Signal(up[:5]); got != 0 {
		t.Fatalf("short MACD signal = %d, want 0", got)
	}

	adx := NewADX(5)
	if adx.Name() != "ADX" || adx.Period() != 11 {
		t.Fatalf("unexpected ADX metadata: %s period=%d", adx.Name(), adx.Period())
	}
	adxUp := adx.CalculateMulti(up)
	assertAlignedSeries(t, adxUp, "adx", "plus_di", "minus_di")
	if got := adx.Signal(up); got != 1 {
		t.Fatalf("ADX uptrend signal = %d, want 1", got)
	}
	if got := adx.Signal(down); got != -1 {
		t.Fatalf("ADX downtrend signal = %d, want -1", got)
	}
	if got := adx.Calculate(up[:5]); got != nil {
		t.Fatalf("short ADX result = %v, want nil", got)
	}

	aroon := NewAroon(5)
	aroonUp := aroon.CalculateMulti(up)
	assertAlignedSeries(t, aroonUp, "aroon_up", "aroon_down", "oscillator")
	if got := aroon.Signal(up); got != 1 {
		t.Fatalf("Aroon uptrend signal = %d, want 1", got)
	}
	if got := aroon.Signal(down); got != -1 {
		t.Fatalf("Aroon downtrend signal = %d, want -1", got)
	}

	ichimoku := NewIchimoku(4, 7, 12, 3)
	ichimokuValues := ichimoku.CalculateMulti(up)
	if ichimokuValues == nil {
		t.Fatal("Ichimoku should calculate values")
	}
	if len(ichimoku.Calculate(up)) != len(ichimokuValues["tenkan"]) {
		t.Fatal("Ichimoku Calculate should return tenkan series")
	}
	if got := ichimoku.CalculateMulti(up[:8]); got != nil {
		t.Fatalf("short Ichimoku result = %v, want nil", got)
	}

	superTrend := NewSuperTrend(5, 2.0)
	superTrendValues := superTrend.CalculateMulti(up)
	assertAlignedSeries(t, superTrendValues, "supertrend", "direction", "upper_band", "lower_band")
	if len(superTrend.Calculate(up)) != len(superTrendValues["supertrend"]) {
		t.Fatal("SuperTrend Calculate should return supertrend series")
	}
	if got := superTrend.Signal(up[:3]); got != 0 {
		t.Fatalf("short SuperTrend signal = %d, want 0", got)
	}
}

func TestParabolicSARHandlesTrendTransitions(t *testing.T) {
	upThenDown := append(indicatorCandles(18, 100, 1.5, 0.8), indicatorCandles(18, 130, -2.5, 0.8)...)
	downThenUp := append(indicatorCandles(18, 130, -1.5, 0.8), indicatorCandles(18, 100, 2.5, 0.8)...)

	psar := NewParabolicSAR(0.02, 0.02, 0.2)
	if psar.Name() != "Parabolic SAR" || psar.Period() != 2 {
		t.Fatalf("unexpected PSAR metadata: %s period=%d", psar.Name(), psar.Period())
	}
	if got := psar.Calculate(upThenDown); len(got) != len(upThenDown) {
		t.Fatalf("PSAR length = %d, want %d", len(got), len(upThenDown))
	}
	if got := psar.Calculate(upThenDown[:1]); got != nil {
		t.Fatalf("short PSAR result = %v, want nil", got)
	}
	if got := psar.Signal(upThenDown[:2]); got != 0 {
		t.Fatalf("short PSAR signal = %d, want 0", got)
	}
	_ = psar.Signal(upThenDown)
	_ = psar.Signal(downThenUp)
}

func TestTrendDefaultRegistryFactories(t *testing.T) {
	factories := []string{"MACD", "ADX", "ParabolicSAR", "Ichimoku", "Aroon", "SuperTrend"}
	params := map[string]interface{}{
		"fast":         5,
		"slow":         10.0,
		"signal":       4,
		"period":       5.0,
		"af_start":     0.02,
		"af_step":      0.02,
		"af_max":       0.2,
		"tenkan":       4,
		"kijun":        7.0,
		"senkou_b":     12,
		"displacement": 3,
		"multiplier":   2,
	}

	for _, name := range factories {
		t.Run(name, func(t *testing.T) {
			indicator := GetIndicator(name, params)
			if indicator == nil {
				t.Fatalf("GetIndicator(%s) returned nil", name)
			}
		})
	}
}

func assertAlignedSeries(t *testing.T, values map[string][]float64, keys ...string) {
	t.Helper()
	if values == nil {
		t.Fatal("values should not be nil")
	}
	wantLen := len(values[keys[0]])
	if wantLen == 0 {
		t.Fatalf("%s should not be empty", keys[0])
	}
	for _, key := range keys[1:] {
		if len(values[key]) != wantLen {
			t.Fatalf("%s length = %d, want %d", key, len(values[key]), wantLen)
		}
	}
}
