package strategy

import (
	"testing"

	"quantmesh/config"
)

func TestSignalStrategiesReadJSONNumericPeriods(t *testing.T) {
	trend := NewTrendFollowingStrategy("trend", &config.Config{}, nil, nil, map[string]interface{}{
		"short_period": float64(8),
		"long_period":  float64(34),
	})
	if trend.shortPeriod != 8 || trend.longPeriod != 34 {
		t.Errorf("trend periods = (%d, %d), want (8, 34)", trend.shortPeriod, trend.longPeriod)
	}

	mean := NewMeanReversionStrategy("mean_reversion", &config.Config{}, nil, nil, map[string]interface{}{
		"period": float64(28),
	})
	if mean.period != 28 {
		t.Errorf("mean reversion period = %d, want 28", mean.period)
	}
}

func TestSignalStrategyIntRejectsNonIntegerAndUnboundedValues(t *testing.T) {
	for _, value := range []float64{-1, 4.5, 1000001} {
		if got := signalStrategyInt(map[string]interface{}{"period": value}, "period", 20); got != 20 {
			t.Errorf("signalStrategyInt(%v) = %d, want default 20", value, got)
		}
	}
}
