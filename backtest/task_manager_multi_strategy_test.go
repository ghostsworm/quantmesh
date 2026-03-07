package backtest

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

type fakeTaskStore struct {
	task *BacktestTask
}

func (s *fakeTaskStore) CreateBacktestTask(task *BacktestTask) error {
	s.task = task
	return nil
}

func (s *fakeTaskStore) GetBacktestTask(id string) (*BacktestTask, error) {
	return s.task, nil
}

func (s *fakeTaskStore) ListBacktestTasks(limit, offset int) ([]*BacktestTask, error) {
	return []*BacktestTask{s.task}, nil
}

func (s *fakeTaskStore) UpdateBacktestTaskStatus(id, status string, progress int, startedAt, completedAt *time.Time, errMsg, resultPath, reportPath string) error {
	if s.task == nil {
		return nil
	}
	s.task.Status = status
	s.task.Progress = progress
	s.task.Error = errMsg
	s.task.ResultPath = resultPath
	s.task.ReportPath = reportPath
	s.task.StartedAt = startedAt
	s.task.CompletedAt = completedAt
	return nil
}

func (s *fakeTaskStore) DeleteBacktestTask(id string) error {
	return nil
}

func TestTaskManagerRunTaskSupportsStrategiesArray(t *testing.T) {
	tempDir := t.TempDir()
	klineFile := "1m_binance_BTCUSDT_20250101.csv"
	klinePath := filepath.Join(tempDir, klineFile)
	csv := "timestamp,open,high,low,close,volume\n" +
		"1735689600000,100,101,99,100,1000\n" +
		"1735689660000,100,102,99,101,1000\n" +
		"1735689720000,101,103,100,102,1000\n" +
		"1735689780000,102,103,100,101,1000\n" +
		"1735689840000,101,104,100,103,1000\n" +
		"1735689900000,103,105,102,104,1000\n" +
		"1735689960000,104,106,103,105,1000\n"
	if err := os.WriteFile(klinePath, []byte(csv), 0644); err != nil {
		t.Fatalf("failed to write kline file: %v", err)
	}

	store := &fakeTaskStore{
		task: &BacktestTask{
			ID:           "bt_test_multi",
			Mode:         TaskModeBotStrategies,
			Status:       "pending",
			Symbol:       "BTCUSDT",
			TotalCapital: 1000,
			DataSource:   "kline_file",
			KlineFile:    klineFile,
			Strategies: []TaskStrategy{
				{ID: "grid-primary", Name: "Grid Primary", Type: "grid", Weight: 0.5, Config: map[string]interface{}{"grid_count": 4, "grid_spacing": 0.01}},
				{ID: "trend-secondary", Name: "Trend Secondary", Type: "trend_following", Weight: 0.5, Config: map[string]interface{}{"fast_period": 2, "slow_period": 3}},
			},
		},
	}

	manager := NewTaskManager(store, nil, tempDir)
	manager.resultsDir = filepath.Join(tempDir, "results")
	manager.reportsDir = filepath.Join(tempDir, "reports")

	if err := manager.RunTask(store.task.ID); err != nil {
		t.Fatalf("expected task manager to run without error, got: %v", err)
	}
	if store.task.Status != "completed" {
		t.Fatalf("expected task status to be completed, got %q (error=%q)", store.task.Status, store.task.Error)
	}
	if store.task.ResultPath == "" {
		t.Fatal("expected result path to be populated")
	}

	payload, err := LoadResult(manager.resultsDir, store.task.ID)
	if err != nil {
		t.Fatalf("expected task result to be readable, got: %v", err)
	}
	if payload.MultiResult == nil {
		t.Fatal("expected multi strategy result to be persisted")
	}
	if len(payload.MultiResult.StrategyResults) != 2 {
		t.Fatalf("expected 2 persisted strategy results, got %d", len(payload.MultiResult.StrategyResults))
	}
}
