package backtest

import (
	"context"
	"testing"

	"quantmesh/exchange"
)

// Deterministically trigger real context cancellation at a loop checkpoint,
// avoiding timing-dependent large inputs and goroutine sleeps in these tests.
type checkpointCancelContext struct {
	context.Context
	cancel context.CancelFunc
	checks int
}

func (c *checkpointCancelContext) Err() error {
	c.checks++
	if c.checks == 4 {
		c.cancel()
	}
	return c.Context.Err()
}

func TestStrategyBacktestsCancelInsideCandleLoops(t *testing.T) {
	candles := make([]*exchange.Candle, 20)
	for i := range candles {
		candles[i] = &exchange.Candle{Open: 100, High: 101, Low: 99, Close: 100, Timestamp: int64(i+1) * 86400000}
	}
	runners := map[string]func(context.Context) (*BacktestResult, error){
		"grid": func(ctx context.Context) (*BacktestResult, error) {
			return RunGridBacktestContext(ctx, "BTCUSDT", candles, GridBacktestParams{PriceLow: 90, PriceHigh: 110, GridCount: 4, OrderQuantity: 10, TotalCapital: 1000}, 1000, nil)
		},
		"dca": func(ctx context.Context) (*BacktestResult, error) {
			return RunDCABacktestContext(ctx, "BTCUSDT", "1d", candles, DCABacktestParams{IntervalDays: 1, AmountPerTrade: 10, TotalCapital: 1000}, 1000)
		},
		"martingale": func(ctx context.Context) (*BacktestResult, error) {
			return RunMartingaleBacktestContext(ctx, "BTCUSDT", "1d", candles, MartingaleBacktestParams{BaseAmount: 10, Multiplier: 2, TotalCapital: 1000, TakeProfitPct: 1, StopLossPct: 2}, 1000)
		},
	}
	for name, run := range runners {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			checked := &checkpointCancelContext{Context: ctx, cancel: cancel}
			result, err := run(checked)
			if result != nil || err != context.Canceled || checked.checks < 4 {
				t.Fatalf("result=%v err=%v checks=%d", result, err, checked.checks)
			}
			if result, err := run(context.Background()); err != nil || result == nil {
				t.Fatalf("normal execution broken: %v", err)
			}
		})
	}
}

type cancelingStrategy struct {
	cancel context.CancelFunc
	calls  int
}

func (s *cancelingStrategy) GetName() string { return "cancel-test" }
func (s *cancelingStrategy) OnCandle(*exchange.Candle) Signal {
	s.calls++
	s.cancel()
	return Signal{Action: "buy"}
}

func TestIndicatorBacktestCancellationStopsBeforeTrade(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	strategy := &cancelingStrategy{cancel: cancel}
	candles := []*exchange.Candle{{Close: 100, Timestamp: 60000}, {Close: 100, Timestamp: 120000}}
	bt := NewBacktester("BTCUSDT", candles, strategy, 1000)
	result, err := bt.RunContext(ctx)
	if result != nil || err != context.Canceled || strategy.calls != 1 || len(bt.trades) != 0 {
		t.Fatalf("canceled callback still traded: result=%v err=%v calls=%d trades=%d", result, err, strategy.calls, len(bt.trades))
	}
}
