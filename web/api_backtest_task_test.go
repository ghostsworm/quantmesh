package web

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"quantmesh/backtest"
)

type fakeWebTaskStore struct {
	task *backtest.BacktestTask
}

func (s *fakeWebTaskStore) CreateBacktestTask(task *backtest.BacktestTask) error {
	s.task = task
	return nil
}

func (s *fakeWebTaskStore) GetBacktestTask(id string) (*backtest.BacktestTask, error) {
	return s.task, nil
}

func (s *fakeWebTaskStore) ListBacktestTasks(limit, offset int) ([]*backtest.BacktestTask, error) {
	if s.task == nil {
		return nil, nil
	}
	return []*backtest.BacktestTask{s.task}, nil
}

func (s *fakeWebTaskStore) UpdateBacktestTaskStatus(id, status string, progress int, startedAt, completedAt *time.Time, errMsg, resultPath, reportPath string) error {
	if s.task == nil {
		return nil
	}
	s.task.Status = status
	s.task.Progress = progress
	s.task.Error = errMsg
	s.task.ResultPath = resultPath
	s.task.ReportPath = reportPath
	return nil
}

func (s *fakeWebTaskStore) DeleteBacktestTask(id string) error {
	return nil
}

func TestPostBacktestTasksAcceptsStrategiesArray(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tempDir := t.TempDir()
	klineFile := "1m_binance_BTCUSDT_20250101.csv"
	klinePath := filepath.Join(tempDir, klineFile)
	csv := "timestamp,open,high,low,close,volume\n" +
		"1735689600000,100,101,99,100,1000\n" +
		"1735689660000,100,102,99,101,1000\n" +
		"1735689720000,101,103,100,102,1000\n"
	if err := os.WriteFile(klinePath, []byte(csv), 0644); err != nil {
		t.Fatalf("failed to write kline file: %v", err)
	}

	store := &fakeWebTaskStore{}
	originManager := backtestTaskManager
	backtestTaskManager = backtest.NewTaskManager(store, nil, tempDir)
	t.Cleanup(func() {
		backtestTaskManager = originManager
	})

	reqBody := map[string]interface{}{
		"mode":          "bot_strategies",
		"bot_id":        "bot_1",
		"symbol":        "BTCUSDT",
		"interval":      "1m",
		"total_capital": 1000,
		"data_source":   "kline_file",
		"kline_file":    klineFile,
		"strategies": []map[string]interface{}{
			{"type": "grid", "weight": 0},
			{"type": "trend_following", "weight": 2, "config": map[string]interface{}{"fast_period": 2, "slow_period": 3}},
		},
	}
	body, _ := json.Marshal(reqBody)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/backtest/tasks", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	postBacktestTasks(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d with body %s", w.Code, w.Body.String())
	}
	if store.task == nil {
		t.Fatal("expected task to be stored")
	}
	if store.task.Mode != backtest.TaskModeBotStrategies {
		t.Fatalf("expected mode to be bot_strategies, got %q", store.task.Mode)
	}
	if len(store.task.Strategies) != 2 {
		t.Fatalf("expected 2 strategies, got %d", len(store.task.Strategies))
	}
}

func TestPostBacktestTasksAcceptsHedgeGroupWithoutStrategy(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tempDir := t.TempDir()
	legAFile := "1m_binance_BTCUSDT_20250101.csv"
	legBFile := "1m_binance_ETHUSDT_20250101.csv"
	csv := "timestamp,open,high,low,close,volume\n" +
		"1735689600000,100,101,99,100,1000\n" +
		"1735689660000,100,102,99,101,1000\n" +
		"1735689720000,101,103,100,102,1000\n"
	if err := os.WriteFile(filepath.Join(tempDir, legAFile), []byte(csv), 0644); err != nil {
		t.Fatalf("failed to write legA kline file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tempDir, legBFile), []byte(csv), 0644); err != nil {
		t.Fatalf("failed to write legB kline file: %v", err)
	}

	store := &fakeWebTaskStore{}
	originManager := backtestTaskManager
	backtestTaskManager = backtest.NewTaskManager(store, nil, tempDir)
	t.Cleanup(func() {
		backtestTaskManager = originManager
	})

	reqBody := map[string]interface{}{
		"mode":          "hedge_group",
		"group_id":      "group_hedge_1",
		"symbol":        "BTCUSDT",
		"interval":      "1m",
		"total_capital": 1000,
		"data_source":   "kline_file",
		"kline_file":    legAFile,
		"params": map[string]interface{}{
			"leg_b_symbol":      "ETHUSDT",
			"leg_b_kline_file":  legBFile,
			"hedge_ratio":       1.0,
			"rebalance_interval": 2,
		},
	}
	body, _ := json.Marshal(reqBody)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/backtest/tasks", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	postBacktestTasks(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d with body %s", w.Code, w.Body.String())
	}
	if store.task == nil {
		t.Fatal("expected task to be stored")
	}
	if store.task.Mode != backtest.TaskModeHedgeGroup {
		t.Fatalf("expected mode to be hedge_group, got %q", store.task.Mode)
	}
	if store.task.GroupID != "group_hedge_1" {
		t.Fatalf("expected group id to be preserved, got %q", store.task.GroupID)
	}
}
