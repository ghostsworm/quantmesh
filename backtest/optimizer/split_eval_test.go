package optimizer

import (
	"testing"

	"quantmesh/exchange"
)

func TestSplitCandlesForValidation_Disabled(t *testing.T) {
	candles := make([]*exchange.Candle, 100)
	for i := range candles {
		candles[i] = &exchange.Candle{Timestamp: int64(i * 60000), Open: 1, High: 1, Low: 1, Close: 1, Volume: 1}
	}
	train, val, hold, err := SplitCandlesForValidation(candles, 0)
	if err != nil {
		t.Fatal(err)
	}
	if hold || val != nil || len(train) != 100 {
		t.Fatalf("want full train, no val; got hold=%v len(train)=%d len(val)=%v", hold, len(train), val)
	}
}

func TestSplitCandlesForValidation_Ratio020(t *testing.T) {
	var candles []*exchange.Candle
	for i := 0; i < 100; i++ {
		candles = append(candles, &exchange.Candle{Timestamp: int64(i * 60000), Open: 1, High: 1, Low: 1, Close: 1, Volume: 1})
	}
	train, val, hold, err := SplitCandlesForValidation(candles, 0.2)
	if err != nil {
		t.Fatal(err)
	}
	if !hold || len(val) < minValBars || len(train) < minTrainBars {
		t.Fatalf("train=%d val=%d hold=%v err=%v", len(train), len(val), hold, err)
	}
}

func TestValidateOptimConfig_ZeroAllowed(t *testing.T) {
	if err := ValidateOptimConfig(OptimConfig{ValidationRatio: 0}); err != nil {
		t.Fatal(err)
	}
	if err := ValidateOptimConfig(OptimConfig{ValidationRatio: 0.5}); err == nil {
		t.Fatal("expected error for ratio >= 0.5")
	}
}
