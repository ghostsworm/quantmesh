package backtest

import (
	"testing"
	"time"

	"quantmesh/exchange"
)

func TestNormalizeTaskStrategiesDefaultsAndScalesWeights(t *testing.T) {
	strategies := NormalizeTaskStrategies([]TaskStrategy{
		{Type: "grid", Weight: 0},
		{Type: "trend_following", Weight: 2},
		{Type: "momentum", Weight: -1},
	})
	if len(strategies) != 3 {
		t.Fatalf("expected 3 strategies, got %d", len(strategies))
	}
	total := 0.0
	for _, strategy := range strategies {
		if strategy.Weight <= 0 {
			t.Fatalf("expected normalized weight > 0, got %f", strategy.Weight)
		}
		total += strategy.Weight
	}
	if total < 0.999 || total > 1.001 {
		t.Fatalf("expected normalized weights to sum to 1, got %f", total)
	}
}

func TestCreateTaskBacktestStrategySupportsTrendFollowing(t *testing.T) {
	strategy, err := CreateTaskBacktestStrategy(
		TaskStrategy{
			Type:   "trend_following",
			Weight: 1,
			Config: map[string]interface{}{
				"fast_period": 12,
				"slow_period": 26,
			},
		},
		StrategyExecutionContext{
			Symbol:       "BTCUSDT",
			TotalCapital: 1000,
		},
	)
	if err != nil {
		t.Fatalf("expected trend_following to be supported, got error: %v", err)
	}
	if strategy == nil {
		t.Fatal("expected strategy instance, got nil")
	}
}

func TestRunMultiStrategyTaskReturnsCombinedResult(t *testing.T) {
	task := &BacktestTask{
		Mode:         TaskModeBotStrategies,
		Symbol:       "BTCUSDT",
		TotalCapital: 1000,
		Strategies: []TaskStrategy{
			{Type: "grid", Weight: 0.5, Config: map[string]interface{}{"grid_count": 4, "grid_spacing": 0.01}},
			{Type: "trend_following", Weight: 0.5, Config: map[string]interface{}{"fast_period": 3, "slow_period": 5}},
		},
	}

	candles := make([]*exchange.Candle, 0, 12)
	baseTs := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC).UnixMilli()
	prices := []float64{100, 101, 102, 101, 103, 104, 105, 104, 106, 107, 108, 109}
	for i, price := range prices {
		candles = append(candles, &exchange.Candle{
			Symbol:    "BTCUSDT",
			Open:      price - 0.5,
			High:      price + 1,
			Low:       price - 1,
			Close:     price,
			Volume:    1000,
			Timestamp: baseTs + int64(i)*60_000,
			IsClosed:  true,
		})
	}

	result, err := RunMultiStrategyTask(task, candles)
	if err != nil {
		t.Fatalf("expected multi-strategy task to run successfully, got error: %v", err)
	}
	if result == nil {
		t.Fatal("expected result, got nil")
	}
	if len(result.StatsByStrategy) != 2 {
		t.Fatalf("expected 2 strategy stats, got %d", len(result.StatsByStrategy))
	}
}

func TestRunMultiStrategyTaskReturnsAuditableStrategyResults(t *testing.T) {
	task := &BacktestTask{
		Mode:         TaskModeBotStrategies,
		Symbol:       "BTCUSDT",
		TotalCapital: 1000,
		Strategies: []TaskStrategy{
			{
				ID:     "grid-primary",
				Name:   "Grid Primary",
				Type:   "grid",
				Weight: 0.25,
				Config: map[string]interface{}{"grid_count": 4, "grid_spacing": 0.01},
			},
			{
				ID:     "grid-secondary",
				Name:   "Grid Secondary",
				Type:   "grid",
				Weight: 0.75,
				Config: map[string]interface{}{"grid_count": 6, "grid_spacing": 0.015},
			},
		},
	}

	candles := make([]*exchange.Candle, 0, 16)
	baseTs := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC).UnixMilli()
	prices := []float64{100, 99, 101, 100, 102, 101, 103, 102, 104, 103, 105, 104, 103, 102, 101, 100}
	for i, price := range prices {
		candles = append(candles, &exchange.Candle{
			Symbol:    "BTCUSDT",
			Open:      price - 0.5,
			High:      price + 1,
			Low:       price - 1,
			Close:     price,
			Volume:    1000,
			Timestamp: baseTs + int64(i)*60_000,
			IsClosed:  true,
		})
	}

	result, err := RunMultiStrategyTask(task, candles)
	if err != nil {
		t.Fatalf("expected multi-strategy task to run successfully, got error: %v", err)
	}
	if result == nil {
		t.Fatal("expected result, got nil")
	}
	if len(result.StrategyResults) != 2 {
		t.Fatalf("expected 2 strategy results, got %d", len(result.StrategyResults))
	}

	totalInitialCapital := 0.0
	seenIDs := make(map[string]struct{}, len(result.StrategyResults))
	for _, strategyResult := range result.StrategyResults {
		if strategyResult.StrategyID == "" {
			t.Fatal("expected strategy result to include strategy id")
		}
		if _, exists := seenIDs[strategyResult.StrategyID]; exists {
			t.Fatalf("duplicate strategy id in result: %s", strategyResult.StrategyID)
		}
		seenIDs[strategyResult.StrategyID] = struct{}{}
		totalInitialCapital += strategyResult.InitialCapital
	}

	if totalInitialCapital < 999.99 || totalInitialCapital > 1000.01 {
		t.Fatalf("expected strategy initial capital to sum to total capital, got %.2f", totalInitialCapital)
	}
}
