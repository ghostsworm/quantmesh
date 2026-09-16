package replay

import (
	"testing"

	"quantmesh/backtest"
	"quantmesh/exchange"
)

func TestRunGridTask_CandleFallback(t *testing.T) {
	var candles []*exchange.Candle
	// 20 根 1 分鐘 K 線在 1980~2020 間來回，K 線內路徑穿越多個 10 USDT 網格
	for i := 0; i < 20; i++ {
		open := 2000.0
		close := 2005.0
		if i%2 == 1 {
			open, close = 2005, 2000
		}
		candles = append(candles, &exchange.Candle{Timestamp: testBaseTs + int64(i)*60000, Open: open, High: 2021, Low: 1979, Close: close, Volume: 50})
	}
	task := &backtest.BacktestTask{
		ID:           "bt_replay_candles",
		Strategy:     "grid",
		Symbol:       "ethusdt",
		TotalCapital: testCapital,
		Leverage:     2,
		Params: map[string]interface{}{
			backtest.ParamKeyEngine: backtest.EngineReplay,
			"grid_spacing":          10.0,
			"grid_count":            float64(3),
			"order_quantity":        100.0,
			"maker_fee_rate":        0.0002,
			"taker_fee_rate":        0.0005,
			"price_decimals":        float64(2),
			"quantity_decimals":     float64(3),
		},
	}
	res, extra, err := RunGridTask(task, candles)
	if err != nil {
		t.Fatalf("run grid task: %v", err)
	}
	m, ok := extra.(Metrics)
	if !ok {
		t.Fatalf("extra metrics type %T", extra)
	}
	if m.Fills == 0 || m.ClosedGrids == 0 || res.Metrics.TotalFees <= 0 {
		t.Fatalf("expected fills and closed grids on oscillating candles, got fills=%d closed=%d fees=%v", m.Fills, m.ClosedGrids, res.Metrics.TotalFees)
	}
	if res.Symbol != "ETHUSDT" || res.Strategy != "grid_replay" {
		t.Fatalf("unexpected result header: %s %s", res.Symbol, res.Strategy)
	}

	cfg, err := ConfigFromTask(&backtest.BacktestTask{Symbol: "ETHUSDT", Params: map[string]interface{}{}})
	if err == nil {
		t.Fatalf("missing grid_spacing must be rejected, got config %+v", cfg)
	}
}
