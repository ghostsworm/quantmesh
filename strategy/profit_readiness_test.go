package strategy

import (
	"quantmesh/config"
	"quantmesh/position"
	"quantmesh/utils"
	"testing"
)

func TestAuditSignalClientIDsMustRemainUniqueAfterBrokerPrefix(t *testing.T) {
	first := utils.AddBrokerPrefix("binance", "trend_open_long_1790208000000000001")
	second := utils.AddBrokerPrefix("binance", "trend_open_long_1790208000000000002")
	if first == second {
		t.Fatal("two distinct nanosecond signal IDs collapse to the same broker-prefixed ID")
	}
}

func TestAuditNonGridStrategyMustHonorOpeningPause(t *testing.T) {
	cfg := &config.Config{}
	cfg.Trading.Symbol = "BTCUSDT"
	cfg.Trading.MarketType = "futures"
	cfg.Trading.OpenPositionControl.PauseOpening = true
	exec := &signalTestExecutor{}
	s := NewMeanReversionStrategy("mean_reversion", cfg, exec, &signalTestExchange{}, map[string]interface{}{"period": 2, "std_multiplier": 0.5, "order_amount": 100.0, "slippage": 0.001})
	if err := s.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	defer s.Stop()
	for _, price := range []float64{100, 100, 90} {
		if err := s.OnPriceChange(price); err != nil {
			t.Fatal(err)
		}
	}
	if len(exec.orders) > 0 {
		t.Fatalf("opening pause=true, yet strategy submitted %d new orders", len(exec.orders))
	}
}

func TestAuditSignalStrategiesMustRetainPartialFillOnCancel(t *testing.T) {
	cfg := &config.Config{}
	cfg.Trading.Symbol = "BTCUSDT"
	for _, kind := range []string{"trend", "mean_reversion"} {
		t.Run(kind, func(t *testing.T) {
			ord := &Order{OrderID: 42, Symbol: "BTCUSDT", Side: "BUY", Quantity: 1, Price: 100}
			partial := &position.OrderUpdate{OrderID: 42, Status: "PARTIALLY_FILLED", ExecutedQty: 0.4, AvgPrice: 100, CommissionKnown: true}
			cancelled := &position.OrderUpdate{OrderID: 42, Status: "CANCELED", ExecutedQty: 0.4, AvgPrice: 100, CommissionKnown: true}
			var holdings []*Position
			if kind == "trend" {
				s := NewTrendFollowingStrategy("trend", cfg, &signalTestExecutor{}, &signalTestExchange{}, nil)
				s.activeOrder = ord
				s.pendingAction = signalActionOpenLong
				if err := s.OnOrderUpdate(partial); err != nil {
					t.Fatal(err)
				}
				if err := s.OnOrderUpdate(cancelled); err != nil {
					t.Fatal(err)
				}
				holdings = s.GetPositions()
			} else {
				s := NewMeanReversionStrategy("mean", cfg, &signalTestExecutor{}, &signalTestExchange{}, nil)
				s.activeOrder = ord
				s.pendingAction = signalActionOpenLong
				if err := s.OnOrderUpdate(partial); err != nil {
					t.Fatal(err)
				}
				if err := s.OnOrderUpdate(cancelled); err != nil {
					t.Fatal(err)
				}
				holdings = s.GetPositions()
			}
			if len(holdings) != 1 || holdings[0].Size != 0.4 {
				t.Fatalf("0.4 filled then cancelled; strategy reports positions=%v", holdings)
			}
		})
	}
}

func TestAuditDCAMustNotBookUnfilledCloseProfit(t *testing.T) {
	s := newR3DCA(t, &hedgeOrderExecutor{}, nil)
	s.layers = []*DCALayer{{Index: 0, Price: 100, Quantity: 1, Cost: 100, Status: entryStatusFilled}}
	s.updateTotals()
	if err := s.closeAllPositions(110, "止盈"); err != nil {
		t.Fatal(err)
	}
	if s.stats.TotalPnL != 0 || s.stats.TotalTrades != 0 {
		t.Fatalf("close only submitted, no fill: realized PnL=%v, trades=%v", s.stats.TotalPnL, s.stats.TotalTrades)
	}
}
