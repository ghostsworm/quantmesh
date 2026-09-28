package optimizer

import (
	"context"
	"math"
	"sync/atomic"
	"testing"

	"quantmesh/backtest"
	"quantmesh/exchange"
)

type optimizerCancelCheckpoint struct {
	context.Context
	cancel context.CancelFunc
	checks atomic.Int32
}

func (c *optimizerCancelCheckpoint) Err() error {
	if c.checks.Add(1) == 15 {
		c.cancel()
	}
	return c.Context.Err()
}

func TestAllOptimizersPropagateMidEvaluationCancellation(t *testing.T) {
	for name, opt := range map[string]Optimizer{"grid": &GridSearchOptimizer{}, "bayesian": NewBayesianOptimizer(), "genetic": NewGeneticOptimizer()} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			checkpoint := &optimizerCancelCheckpoint{Context: ctx, cancel: cancel}
			result, err := opt.Run(checkpoint, "BTCUSDT", optimizerCandles(100), tinySearchSpace(), DefaultOptimConfig(), 1000)
			if result != nil || err != context.Canceled || checkpoint.checks.Load() < 15 {
				t.Fatalf("cancellation returned success: result=%v err=%v checks=%d", result, err, checkpoint.checks.Load())
			}
		})
	}
}

func TestEvalParamSetCancellationIsNotAnOrdinaryFailedSample(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err := EvalParamSetContext(ctx, "BTCUSDT", nil, nil, false, backtest.GridBacktestParams{}, 0.5, 1000)
	if err != context.Canceled || !math.IsInf(result.Score, -1) {
		t.Fatalf("result=%v err=%v", result, err)
	}
}

func TestWalkForwardCancellationDoesNotSelectOrTestPartialWinner(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := 0
	run := func(string, []*exchange.Candle, backtest.GridBacktestParams, float64) (*backtest.BacktestResult, error) {
		calls++
		cancel()
		return &backtest.BacktestResult{}, nil
	}
	result, err := runWalkForward(ctx, run, "BTCUSDT", hourlyCandles(wfTestDays), []backtest.GridBacktestParams{{GridCount: 2}, {GridCount: 3}}, WalkForwardConfig{Enabled: true}, 0.5, 1000)
	if result != nil || err != context.Canceled || calls != 1 {
		t.Fatalf("result=%v err=%v calls=%d", result, err, calls)
	}
}
