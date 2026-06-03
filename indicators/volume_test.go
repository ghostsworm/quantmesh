package indicators

import "testing"

func TestVolumeIndicatorsCalculateAndSignals(t *testing.T) {
	up := volumeCandles([]float64{10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21}, []float64{100, 120, 90, 140, 80, 160, 70, 180, 60, 200, 50, 220})
	down := volumeCandles([]float64{21, 20, 19, 18, 17, 16, 15, 14, 13, 12, 11, 10}, []float64{220, 50, 200, 60, 180, 70, 160, 80, 140, 90, 120, 100})
	flat := flatIndicatorCandles(8, 100)

	tests := []struct {
		name      string
		indicator Indicator
		input     []Candle
	}{
		{"OBV", NewOBV(), up},
		{"VWAP", NewVWAP(), up},
		{"VolumeProfile", NewVolumeProfile(4, 5), up},
		{"CMF", NewCMF(4), up},
		{"ADL", NewADL(), up},
		{"ChaikinOscillator", NewChaikinOscillator(3, 6), up},
		{"ForceIndex", NewForceIndex(3), up},
		{"NVI", NewNVI(), up},
		{"PVI", NewPVI(), up},
		{"EaseOfMovement", NewEaseOfMovement(3), up},
		{"VolumeROC", NewVolumeRateOfChange(3), up},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.indicator.Name() == "" {
				t.Fatal("indicator name should not be empty")
			}
			if tt.indicator.Period() <= 0 {
				t.Fatalf("period = %d, want positive", tt.indicator.Period())
			}
			if got := tt.indicator.Calculate(tt.input); len(got) == 0 {
				t.Fatalf("%s returned empty result", tt.name)
			}
			if got := tt.indicator.Calculate(tt.input[:1]); got != nil && len(got) != 1 {
				t.Fatalf("%s short result = %v, want nil or one-point value", tt.name, got)
			}
		})
	}

	if got := NewOBV().Calculate([]Candle{
		{Close: 10, Volume: 100},
		{Close: 11, Volume: 20},
		{Close: 9, Volume: 5},
		{Close: 9, Volume: 7},
	}); got[3] != 115 {
		t.Fatalf("OBV = %v, want last 115", got)
	}
	if got := NewOBV().Signal(up[:9]); got != 0 {
		t.Fatalf("short OBV signal = %d, want 0", got)
	}
	if got := NewVWAP().Signal(up); got != 1 {
		t.Fatalf("VWAP up signal = %d, want 1", got)
	}
	if got := NewVWAP().Signal(down); got != -1 {
		t.Fatalf("VWAP down signal = %d, want -1", got)
	}
	if got := NewVolumeProfile(4, 5).Calculate(flat); got[0] != 100 {
		t.Fatalf("flat volume profile = %v, want first 100", got)
	}
	if got := NewCMF(4).Signal(cmfBiasedCandles(12, true)); got != 1 {
		t.Fatalf("CMF up signal = %d, want 1", got)
	}
	if got := NewCMF(4).Signal(cmfBiasedCandles(12, false)); got != -1 {
		t.Fatalf("CMF down signal = %d, want -1", got)
	}
	if got := NewEaseOfMovement(3).Signal(up); got != 1 {
		t.Fatalf("EOM up signal = %d, want 1", got)
	}
	if got := NewEaseOfMovement(3).Signal(down); got != -1 {
		t.Fatalf("EOM down signal = %d, want -1", got)
	}
}

func TestVolumeDefaultRegistryFactories(t *testing.T) {
	names := []string{
		"OBV",
		"VWAP",
		"VolumeProfile",
		"CMF",
		"ADL",
		"ChaikinOscillator",
		"ForceIndex",
		"NVI",
		"PVI",
		"EaseOfMovement",
		"VolumeROC",
	}

	params := map[string]interface{}{"period": 4, "bins": 5.0, "fast": 3, "slow": 6.0}
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			if indicator := GetIndicator(name, params); indicator == nil {
				t.Fatalf("GetIndicator(%s) returned nil", name)
			}
		})
	}
}

func volumeCandles(closes, volumes []float64) []Candle {
	candles := make([]Candle, len(closes))
	for i := range closes {
		close := closes[i]
		candles[i] = Candle{
			Time:   int64(i),
			Open:   close - 0.4,
			High:   close + 1,
			Low:    close - 1,
			Close:  close,
			Volume: volumes[i],
		}
	}
	return candles
}

func cmfBiasedCandles(count int, closeNearHigh bool) []Candle {
	candles := make([]Candle, count)
	for i := range candles {
		low := 90 + float64(i)
		high := low + 10
		close := high - 1
		if !closeNearHigh {
			close = low + 1
		}
		candles[i] = Candle{
			Time:   int64(i),
			Open:   (high + low) / 2,
			High:   high,
			Low:    low,
			Close:  close,
			Volume: 100 + float64(i),
		}
	}
	return candles
}
