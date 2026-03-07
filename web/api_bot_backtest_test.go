package web

import (
	"testing"

	"quantmesh/config"
)

func TestCreateBacktestStrategySupportsTrendFollowing(t *testing.T) {
	botCfg := &config.BotConfig{
		Symbol:                "BTCUSDT",
		TotalAllocatedCapital: 1000,
		Strategies: []config.StrategyInstance{
			{
				Type:   "trend_following",
				Weight: 1,
				Config: map[string]interface{}{
					"fast_period": 12,
					"slow_period": 26,
				},
			},
		},
	}

	strategy, err := createBacktestStrategy(botCfg.Strategies[0], botCfg)
	if err != nil {
		t.Fatalf("expected trend_following to be supported, got error: %v", err)
	}
	if strategy == nil {
		t.Fatal("expected a backtest strategy instance, got nil")
	}
	if strategy.GetType() != "trend" && strategy.GetType() != "trend_following" {
		t.Fatalf("expected trend-like strategy type, got %q", strategy.GetType())
	}
}

func TestNormalizeBotStrategiesScalesMissingWeights(t *testing.T) {
	strategies := normalizeBotStrategies([]config.StrategyInstance{
		{Type: "grid", Weight: 0},
		{Type: "trend_following", Weight: 2},
	})
	if len(strategies) != 2 {
		t.Fatalf("expected 2 strategies, got %d", len(strategies))
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
