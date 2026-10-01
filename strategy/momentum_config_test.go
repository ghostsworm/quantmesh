package strategy

import (
	"testing"

	"quantmesh/config"
)

func TestNewMomentumStrategyReadsJSONNumericRSIPeriod(t *testing.T) {
	strategy := NewMomentumStrategy("momentum", &config.Config{}, nil, nil, map[string]interface{}{
		"rsi_period":   float64(21),
		"overbought":   75.0,
		"oversold":     25.0,
		"order_amount": 250.0,
	})
	if strategy.rsiPeriod != 21 || strategy.overbought != 75 || strategy.oversold != 25 || strategy.orderAmount != 250 {
		t.Fatalf("template config not applied: period=%d overbought=%v oversold=%v amount=%v", strategy.rsiPeriod, strategy.overbought, strategy.oversold, strategy.orderAmount)
	}
}
