package backtest

import (
	"strings"
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
	// 驗證期末強制平倉：有剩餘倉位時應產生帶 [期末强制平仓] 的 CompletedTrade
	for _, ct := range result.CompletedTrades {
		if containsForceCloseMarker(ct.Strategy) {
			// 強制平倉交易應有正確的 PnL
			if ct.PnL == 0 && ct.Size > 0 {
				t.Logf("force close trade: size=%.4f, pnl=%.4f (expected non-zero pnl if position had unrealized gain/loss)", ct.Size, ct.PnL)
			}
			break
		}
	}
	// 驗證權益曲線最後一點等於最終權益（期末強制平倉後追加的終點）
	if len(result.EquityCurve) > 0 && result.FinalEquity > 0 {
		lastEquity := result.EquityCurve[len(result.EquityCurve)-1].Equity
		if lastEquity != result.FinalEquity {
			t.Errorf("expected last equity curve point %.2f to equal FinalEquity %.2f", lastEquity, result.FinalEquity)
		}
	}
}

func containsForceCloseMarker(s string) bool {
	return strings.Contains(s, "期末强制平仓")
}

// TestRunMultiStrategyTaskDirectionLongOnly 驗證單向做多時不產生 SHORT 平倉記錄
func TestRunMultiStrategyTaskDirectionLongOnly(t *testing.T) {
	task := &BacktestTask{
		Mode:         TaskModeBotStrategies,
		Symbol:       "BTCUSDT",
		TotalCapital: 1000,
		Strategies: []TaskStrategy{
			{
				Type:   "grid",
				Weight: 1,
				Config: map[string]interface{}{
					"grid_count":    6,
					"grid_spacing":  0.01,
					"direction":     "LONG",
				},
			},
		},
	}

	candles := make([]*exchange.Candle, 0, 20)
	baseTs := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC).UnixMilli()
	// 價格上下波動，觸發網格買賣
	prices := []float64{100, 99, 101, 100, 102, 101, 103, 102, 104, 103, 105, 104, 103, 102, 101, 100, 99, 100, 101, 100}
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

	// 單向做多：CompletedTrades 中不應出現 side="short"
	for _, ct := range result.CompletedTrades {
		if ct.Side == "short" {
			t.Errorf("單向做多模式下不應有 SHORT 平倉記錄，got: strategy=%s side=%s size=%.4f",
				ct.Strategy, ct.Side, ct.Size)
		}
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
