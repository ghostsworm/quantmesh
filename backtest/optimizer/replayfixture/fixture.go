// Package replayfixture supplies deterministic, isolated OHLCV experiments.
// These are synthetic engineering samples, never evidence of market profitability.
package replayfixture

import (
	"math"
	"time"

	"quantmesh/backtest"
	"quantmesh/exchange"
)

const (
	Recipe   = "replay-wf-synthetic-v1"
	Interval = "1h"
	Bars     = 5 * 24
	Capital  = 10000.0
)

var Regimes = []string{"rising", "sideways", "falling"}

func Candles(regime string) []*exchange.Candle {
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC).UnixMilli()
	out := make([]*exchange.Candle, Bars)
	for i := 0; i < Bars; i++ {
		drift := 0.0
		switch regime {
		case "rising":
			drift = 2
		case "falling":
			drift = -2
		}
		open := fixturePrice(drift, i)
		close := fixturePrice(drift, i+1)
		out[i] = &exchange.Candle{Timestamp: start + int64(i)*3600000, Open: open, Close: close, High: math.Max(open, close) + 15, Low: math.Min(open, close) - 15, Volume: 100}
	}
	return out
}

// fixturePrice combines a 12-hour primary cycle with a 37-hour secondary
// cycle. The latter is deliberately incommensurate with the 24-hour walk
// step, so no test window is a phase-for-phase replica of a training window.
func fixturePrice(drift float64, i int) float64 {
	return 2000 + drift*float64(i) +
		30*math.Sin(float64(i)*math.Pi/6) +
		8*math.Sin(float64(i)*2*math.Pi/37)
}

func Config() *backtest.ReplayWalkForwardConfig {
	return &backtest.ReplayWalkForwardConfig{TrainDays: 3, TestDays: 1, StepDays: 1, Leverage: 3, BaseParams: map[string]interface{}{
		"grid_spacing": 20.0, "order_quantity": 100.0, "buy_window_size": 3.0, "sell_window_size": 3.0,
		"price_decimals": 2.0, "quantity_decimals": 4.0, "intrabar_steps_per_leg": 1.0,
		"maker_fee_rate": 0.0002, "taker_fee_rate": 0.0004, "funding_enabled": true, "funding_rate": 0.0001,
		"regime_filter": map[string]interface{}{
			"enabled": true, "kline_interval": "1h", "adx_period": 3.0, "ema_period": 6.0,
			"ema_slope_lookback": 2.0, "atr_period": 3.0, "atr_percentile_lookback": 6.0,
			"bootstrap_bars": 24.0, "min_dwell_bars": 1.0,
		},
	}}
}
