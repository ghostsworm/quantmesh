package strategy

import (
	"context"
	"testing"

	"quantmesh/config"
)

// S5：震荡判定（TrendSide）时也必须执行止损止盈
func TestTrendFollowingStopLossRunsInSidewaysTrend(t *testing.T) {
	cases := []struct {
		name  string
		price float64
	}{
		{name: "stop loss", price: 90},    // -10% <= -2%
		{name: "take profit", price: 110}, // +10% >= 5%
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &config.Config{}
			cfg.Trading.Symbol = "BTCUSDT"
			cfg.Trading.MarketType = "futures"
			executor := &signalTestExecutor{}
			s := NewTrendFollowingStrategy("trend", cfg, executor, &signalTestExchange{}, map[string]interface{}{"order_amount": 100.0})
			setTestRuntimeStateStore(t, s)
			if err := s.Start(context.Background()); err != nil {
				t.Fatalf("start: %v", err)
			}
			s.mu.Lock()
			s.position = &Position{Symbol: "BTCUSDT", Size: 1, EntryPrice: 100}
			s.entryPrice = 100
			s.mu.Unlock()

			// 价格历史不足长周期均线 → detectTrend 返回 TrendSide
			if trend := s.detectTrend(); trend != TrendSide {
				t.Fatalf("precondition: trend=%v want sideways", trend)
			}
			if err := s.OnPriceChange(tc.price); err != nil {
				t.Fatalf("OnPriceChange: %v", err)
			}
			if len(executor.orders) != 1 {
				t.Fatalf("orders=%d, want close order in sideways trend", len(executor.orders))
			}
			if o := executor.orders[0]; o.Side != "SELL" || !o.ReduceOnly {
				t.Fatalf("close order side=%s reduceOnly=%v", o.Side, o.ReduceOnly)
			}
		})
	}
}
