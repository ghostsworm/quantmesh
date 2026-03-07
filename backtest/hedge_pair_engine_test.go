package backtest

import (
	"testing"
	"time"

	"quantmesh/exchange"
)

func TestRunHedgePairTaskBasic(t *testing.T) {
	baseTs := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC).UnixMilli()
	makeCandles := func(symbol string, prices []float64) []*exchange.Candle {
		out := make([]*exchange.Candle, 0, len(prices))
		for i, price := range prices {
			out = append(out, &exchange.Candle{
				Symbol:    symbol,
				Open:      price - 0.5,
				High:      price + 1,
				Low:       price - 1,
				Close:     price,
				Volume:    1000,
				Timestamp: baseTs + int64(i)*60_000,
				IsClosed:  true,
			})
		}
		return out
	}

	task := &BacktestTask{
		Mode:          TaskModeHedgeGroup,
		Symbol:        "BTCUSDT",
		TotalCapital:  1000,
		Params: map[string]interface{}{
			"hedge_ratio":         1.0,
			"rebalance_threshold": 0.05,
			"rebalance_interval":  2,
			"leg_b_symbol":        "ETHUSDT",
		},
	}
	legA := makeCandles("BTCUSDT", []float64{100, 102, 101, 103, 104, 102})
	legB := makeCandles("ETHUSDT", []float64{50, 49, 50, 48, 47, 49})

	result, err := RunHedgePairTask(task, legA, legB)
	if err != nil {
		t.Fatalf("expected hedge task to run, got error: %v", err)
	}
	if result == nil {
		t.Fatal("expected result, got nil")
	}
	if result.AlignedPoints < 2 {
		t.Fatalf("expected aligned points >= 2, got %d", result.AlignedPoints)
	}
	if len(result.EquityCurve) == 0 {
		t.Fatal("expected non-empty equity curve")
	}
}
