package backtest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestTaskManagerRunTaskSupportsHedgeGroupWithKlineFiles(t *testing.T) {
	tempDir := t.TempDir()
	klineDir := filepath.Join(tempDir, "klines")
	resultsDir := filepath.Join(tempDir, "results")
	tasksDir := filepath.Join(tempDir, "tasks")
	if err := os.MkdirAll(klineDir, 0o755); err != nil {
		t.Fatalf("mkdir kline dir failed: %v", err)
	}
	if err := os.MkdirAll(resultsDir, 0o755); err != nil {
		t.Fatalf("mkdir results dir failed: %v", err)
	}
	if err := os.MkdirAll(tasksDir, 0o755); err != nil {
		t.Fatalf("mkdir tasks dir failed: %v", err)
	}

	legAFile := "btc_leg.csv"
	legBFile := "eth_leg.csv"
	writeKlineFixture(t, filepath.Join(klineDir, legAFile), []string{
		"timestamp,open,high,low,close,volume",
		"1735689600000,100,102,99,101,1000",
		"1735689660000,101,103,100,102,1000",
		"1735689720000,102,104,101,103,1000",
		"1735689780000,103,105,102,104,1000",
	})
	writeKlineFixture(t, filepath.Join(klineDir, legBFile), []string{
		"timestamp,open,high,low,close,volume",
		"1735689600000,50,51,49,50,1000",
		"1735689660000,50,52,49,51,1000",
		"1735689720000,51,52,50,50,1000",
		"1735689780000,50,51,49,49,1000",
	})

	store := &fakeTaskStore{
		task: &BacktestTask{
			ID:           "hedge-task-1",
			Mode:         TaskModeHedgeGroup,
			Symbol:       "BTCUSDT",
			DataSource:   "kline_file",
			KlineFile:    legAFile,
			Interval:     "1m",
			TotalCapital: 1000,
			Status:       "pending",
			CreatedAt:    time.Now(),
			Params: map[string]interface{}{
				"leg_b_symbol":       "ETHUSDT",
				"leg_b_kline_file":   legBFile,
				"hedge_ratio":        1.0,
				"rebalance_interval": 2,
			},
		},
	}
	m := NewTaskManager(store, nil, klineDir)
	m.resultsDir = resultsDir
	m.reportsDir = tasksDir

	if err := m.RunTask(store.task.ID); err != nil {
		t.Fatalf("run task failed: %v", err)
	}

	updated, err := store.GetBacktestTask(store.task.ID)
	if err != nil {
		t.Fatalf("load updated task failed: %v", err)
	}
	if updated.Status != "completed" {
		t.Fatalf("expected completed status, got %s", updated.Status)
	}
	if updated.ResultPath == "" {
		t.Fatal("expected result path to be set")
	}
	body, err := os.ReadFile(updated.ResultPath)
	if err != nil {
		t.Fatalf("read result file failed: %v", err)
	}
	if len(body) == 0 {
		t.Fatal("expected result payload to be non-empty")
	}
	if !strings.Contains(string(body), `"hedge_result"`) {
		t.Fatalf("expected hedge_result in payload, got: %s", string(body))
	}
}

func writeKlineFixture(t *testing.T, path string, lines []string) {
	t.Helper()
	content := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write fixture failed: %v", err)
	}
}
