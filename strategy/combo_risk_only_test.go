package strategy

import (
	"testing"

	"quantmesh/config"
)

func TestTrendFollowingRiskOnlyBlocksOpeningAndKeepsStopLossExit(t *testing.T) {
	cfg := &config.Config{}
	cfg.Trading.Symbol = "BTCUSDT"
	executor := &signalTestExecutor{}
	trend := NewTrendFollowingStrategy("trend", cfg, executor, &signalTestExchange{}, map[string]interface{}{
		"method": "ma", "short_period": 2, "long_period": 3, "stop_loss": 0.02, "take_profit": 0.5,
	})
	setTestRuntimeStateStore(t, trend)
	trend.isRunning = true
	trend.priceHistory = []float64{90, 95, 100, 105}
	if err := trend.OnPriceChangeRiskOnly(110); err != nil {
		t.Fatal(err)
	}
	if len(executor.orders) != 0 {
		t.Fatalf("uptrend opened a position in risk-only mode: %+v", executor.orders)
	}

	trend.position = &Position{Symbol: "BTCUSDT", Size: 0.1}
	trend.entryPrice = 100
	if err := trend.OnPriceChangeRiskOnly(97); err != nil {
		t.Fatal(err)
	}
	if len(executor.orders) != 1 || executor.orders[0].Side != "SELL" {
		t.Fatalf("risk-only mode failed to submit stop-loss exit: %+v", executor.orders)
	}
}

func TestMeanReversionRiskOnlyBlocksOpeningAndKeepsExitSignal(t *testing.T) {
	cfg := &config.Config{}
	cfg.Trading.Symbol = "BTCUSDT"
	executor := &signalTestExecutor{}
	mean := NewMeanReversionStrategy("mean", cfg, executor, &signalTestExchange{}, map[string]interface{}{
		"period": 2, "std_multiplier": 0.5, "reversion_threshold": 0.5,
	})
	setTestRuntimeStateStore(t, mean)
	mean.isRunning = true
	mean.priceHistory = []float64{100, 100}
	if err := mean.OnPriceChangeRiskOnly(90); err != nil {
		t.Fatal(err)
	}
	if len(executor.orders) != 0 {
		t.Fatalf("lower-band signal opened a position in risk-only mode: %+v", executor.orders)
	}

	mean.position = &Position{Symbol: "BTCUSDT", Size: 0.1}
	mean.entryPrice = 100
	mean.priceHistory = []float64{100, 100}
	if err := mean.OnPriceChangeRiskOnly(105); err != nil {
		t.Fatal(err)
	}
	if len(executor.orders) != 1 || executor.orders[0].Side != "SELL" {
		t.Fatalf("risk-only mode failed to submit existing-position exit: %+v", executor.orders)
	}
}

func TestMomentumRiskOnlyBlocksOpeningAndKeepsExitSignal(t *testing.T) {
	cfg := &config.Config{}
	cfg.Trading.Symbol = "BTCUSDT"
	executor := &signalTestExecutor{}
	momentum := NewMomentumStrategy("momentum", cfg, executor, &signalTestExchange{}, map[string]interface{}{
		"rsi_period": 2, "oversold": 30.0, "overbought": 70.0,
	})
	setTestRuntimeStateStore(t, momentum)
	momentum.isRunning = true
	momentum.priceHistory = []float64{100, 99}
	if err := momentum.OnPriceChangeRiskOnly(98); err != nil {
		t.Fatal(err)
	}
	if len(executor.orders) != 0 {
		t.Fatalf("oversold signal opened a position in risk-only mode: %+v", executor.orders)
	}

	momentum.position = &Position{Symbol: "BTCUSDT", Size: 0.1, EntryPrice: 99}
	momentum.entryPrice = 99
	momentum.priceHistory = []float64{98, 99}
	if err := momentum.OnPriceChangeRiskOnly(100); err != nil {
		t.Fatal(err)
	}
	if len(executor.orders) != 1 || executor.orders[0].Side != "SELL" {
		t.Fatalf("risk-only mode failed to submit overbought exit: %+v", executor.orders)
	}
}
