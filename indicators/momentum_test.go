package indicators

import "testing"

func TestMomentumIndicatorsCalculateAndSignal(t *testing.T) {
	up := indicatorCandles(60, 100, 1.5, 0.8)
	down := indicatorCandles(60, 160, -1.5, 0.8)
	flat := flatIndicatorCandles(20, 100)

	tests := []struct {
		name      string
		indicator Indicator
		minLength int
	}{
		{"RSI", NewRSI(5), 1},
		{"StochasticOscillator", NewStochasticOscillator(5, 3, 2), 1},
		{"CCI", NewCCI(5), 1},
		{"WilliamsR", NewWilliamsR(5), 1},
		{"MFI", NewMFI(5), 1},
		{"ROC", NewROC(5), 1},
		{"Momentum", NewMomentum(5), 1},
		{"TRIX", NewTRIX(4), 1},
		{"UltimateOscillator", NewUltimateOscillator(3, 5, 8), 1},
		{"AwesomeOscillator", NewAwesomeOscillator(3, 8), 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.indicator.Name() == "" {
				t.Fatal("indicator name should not be empty")
			}
			if tt.indicator.Period() <= 0 {
				t.Fatalf("period = %d, want positive", tt.indicator.Period())
			}
			if got := tt.indicator.Calculate(up); len(got) < tt.minLength {
				t.Fatalf("uptrend result length = %d, want at least %d", len(got), tt.minLength)
			}
			if got := tt.indicator.Calculate(down); len(got) < tt.minLength {
				t.Fatalf("downtrend result length = %d, want at least %d", len(got), tt.minLength)
			}
			if got := tt.indicator.Calculate(up[:1]); got != nil {
				t.Fatalf("short input result = %v, want nil", got)
			}
		})
	}

	if got := NewRSI(5).Signal(up); got != -1 {
		t.Fatalf("RSI uptrend signal = %d, want -1", got)
	}
	if got := NewRSI(5).Signal(down); got != 1 {
		t.Fatalf("RSI downtrend signal = %d, want 1", got)
	}
	if got := NewStochasticOscillator(5, 3, 2).CalculateMulti(flat); got["k"][0] != 50 {
		t.Fatalf("flat stochastic k = %v, want first value 50", got["k"])
	}
	if got := NewWilliamsR(5).Signal(up); got != -1 {
		t.Fatalf("WilliamsR uptrend signal = %d, want -1", got)
	}
	if got := NewWilliamsR(5).Signal(down); got != 1 {
		t.Fatalf("WilliamsR downtrend signal = %d, want 1", got)
	}
	if got := NewMFI(5).Signal(up); got != -1 {
		t.Fatalf("MFI uptrend signal = %d, want -1", got)
	}
	if got := NewMFI(5).Signal(down); got != 1 {
		t.Fatalf("MFI downtrend signal = %d, want 1", got)
	}
	if got := NewAwesomeOscillator(3, 8).Signal(up); got != 1 {
		t.Fatalf("AwesomeOscillator uptrend signal = %d, want 1", got)
	}
	if got := NewAwesomeOscillator(3, 8).Signal(down); got != 0 {
		t.Fatalf("AwesomeOscillator smooth downtrend signal = %d, want 0", got)
	}
}

func TestMomentumMultiValueIndicatorsAlignOutputs(t *testing.T) {
	candles := indicatorCandles(40, 100, 0.7, 1.2)

	stochastic := NewStochasticOscillator(5, 3, 2)
	stochValues := stochastic.CalculateMulti(candles)
	if len(stochValues["k"]) != len(stochValues["d"]) {
		t.Fatalf("stochastic lengths differ: k=%d d=%d", len(stochValues["k"]), len(stochValues["d"]))
	}
	if stochastic.Calculate(candles)[0] != stochValues["k"][0] {
		t.Fatal("Calculate should return stochastic k series")
	}
	if stochastic.Signal(candles[:3]) != 0 {
		t.Fatal("short stochastic signal should be neutral")
	}

	uo := NewUltimateOscillator(3, 5, 8)
	if got := uo.Signal(flatIndicatorCandles(9, 100)); got != 1 {
		t.Fatalf("flat ultimate oscillator signal = %d, want 1", got)
	}
}

func TestMomentumDefaultRegistryFactories(t *testing.T) {
	factories := []string{
		"RSI",
		"StochasticOscillator",
		"CCI",
		"WilliamsR",
		"MFI",
		"ROC",
		"Momentum",
		"TRIX",
		"UltimateOscillator",
		"AwesomeOscillator",
	}

	for _, name := range factories {
		t.Run(name, func(t *testing.T) {
			indicator := GetIndicator(name, map[string]interface{}{
				"period":   5.0,
				"k_period": 5,
				"d_period": 3.0,
				"slowing":  2,
				"period1":  3,
				"period2":  5,
				"period3":  8,
				"fast":     3,
				"slow":     8,
			})
			if indicator == nil {
				t.Fatalf("GetIndicator(%s) returned nil", name)
			}
		})
	}
}

func indicatorCandles(count int, start, drift, spread float64) []Candle {
	candles := make([]Candle, count)
	price := start
	for i := range candles {
		price += drift
		if i%3 == 0 {
			price += spread * 0.2
		}
		open := price - drift*0.25
		high := maxIndicatorFloat(open, price) + spread
		low := minIndicatorFloat(open, price) - spread
		candles[i] = Candle{
			Time:   int64(i),
			Open:   open,
			High:   high,
			Low:    low,
			Close:  price,
			Volume: 1000 + float64(i*7),
		}
	}
	return candles
}

func flatIndicatorCandles(count int, price float64) []Candle {
	candles := make([]Candle, count)
	for i := range candles {
		candles[i] = Candle{
			Time:   int64(i),
			Open:   price,
			High:   price,
			Low:    price,
			Close:  price,
			Volume: 1000,
		}
	}
	return candles
}

func maxIndicatorFloat(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

func minIndicatorFloat(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
