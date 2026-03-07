package storage

import (
	"os"
	"testing"
	"time"

	"quantmesh/backtest"
)

func TestBacktestTaskRoundTripPreservesMultiStrategyFields(t *testing.T) {
	dbPath := "./test_backtest_task.db"
	t.Cleanup(func() {
		_ = removeSQLiteFiles(dbPath)
	})

	st, err := NewSQLiteStorage(dbPath)
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}
	defer st.Close()

	task := &backtest.BacktestTask{
		ID:           "bt_roundtrip",
		Status:       "pending",
		Mode:         backtest.TaskModeBotStrategies,
		BotID:        "bot_1",
		GroupID:      "group_1",
		Strategy:     "grid",
		Strategies:   []backtest.TaskStrategy{{Type: "grid", Weight: 0.4}, {Type: "trend_following", Weight: 0.6}},
		Symbol:       "BTCUSDT",
		Interval:     "1m",
		StartTime:    time.Unix(1700000000, 0),
		EndTime:      time.Unix(1700003600, 0),
		Params:       map[string]interface{}{"grid_count": 10},
		TotalCapital: 1000,
		CreatedAt:    time.Now(),
		DataSource:   "kline_file",
		KlineFile:    "1m_binance_BTCUSDT_20250101.csv",
		CacheName:    "cache_a",
	}
	if err := st.CreateBacktestTask(task); err != nil {
		t.Fatalf("failed to create backtest task: %v", err)
	}

	got, err := st.GetBacktestTask(task.ID)
	if err != nil {
		t.Fatalf("failed to get backtest task: %v", err)
	}
	if got == nil {
		t.Fatal("expected task, got nil")
	}
	if got.Mode != backtest.TaskModeBotStrategies {
		t.Fatalf("expected mode %q, got %q", backtest.TaskModeBotStrategies, got.Mode)
	}
	if got.BotID != task.BotID || got.GroupID != task.GroupID {
		t.Fatalf("expected bot/group ids preserved, got bot=%q group=%q", got.BotID, got.GroupID)
	}
	if len(got.Strategies) != 2 {
		t.Fatalf("expected 2 strategies, got %d", len(got.Strategies))
	}
	if got.Strategies[1].Type != "trend_following" {
		t.Fatalf("expected second strategy type to round-trip, got %q", got.Strategies[1].Type)
	}
}

func removeSQLiteFiles(path string) error {
	_ = os.Remove(path)
	_ = os.Remove(path + "-shm")
	_ = os.Remove(path + "-wal")
	return nil
}
