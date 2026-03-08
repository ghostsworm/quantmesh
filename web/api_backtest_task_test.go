package web

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"quantmesh/backtest"
	"quantmesh/config"
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

func TestDeriveHedgeGroupTaskDefaultsFromConfig(t *testing.T) {
	cfg := &config.Config{
		Bots: []config.BotConfig{
			{ID: "bot_fut", Symbol: "BTCUSDT", Exchange: "binance", MarketType: "futures"},
			{ID: "bot_spot", Symbol: "BTCUSDC", Exchange: "binance", MarketType: "spot"},
		},
		BotGroups: []config.BotGroup{
			{
				ID:     "grp_1",
				Name:   "hedge-1",
				BotIDs: []string{"bot_fut", "bot_spot"},
				HedgeConfig: config.HedgeConfig{
					HedgeRatio:        0.8,
					MaxDrawdown:       12,
					RebalanceInterval: 900,
				},
			},
		},
	}

	primaryBotID, symbol, params, err := deriveHedgeGroupTaskDefaults(cfg, "grp_1", "", nil)
	if err != nil {
		t.Fatalf("derive defaults failed: %v", err)
	}
	if primaryBotID != "bot_fut" {
		t.Fatalf("expected primary bot id bot_fut, got %s", primaryBotID)
	}
	if symbol != "BTCUSDT" {
		t.Fatalf("expected primary symbol BTCUSDT, got %s", symbol)
	}
	if params["leg_b_symbol"] != "BTCUSDC" {
		t.Fatalf("expected leg_b_symbol BTCUSDC, got %#v", params["leg_b_symbol"])
	}
	if params["hedge_ratio"] != 0.8 {
		t.Fatalf("expected hedge_ratio 0.8, got %#v", params["hedge_ratio"])
	}
	if params["rebalance_interval"] != 900 {
		t.Fatalf("expected rebalance_interval 900, got %#v", params["rebalance_interval"])
	}
	if params["rebalance_threshold"] != 0.12 {
		t.Fatalf("expected rebalance_threshold 0.12, got %#v", params["rebalance_threshold"])
	}
}

func TestDeriveHedgeGroupTaskDefaultsKeepsProvidedParams(t *testing.T) {
	cfg := &config.Config{
		Bots: []config.BotConfig{
			{ID: "bot_a", Symbol: "ETHUSDT", Exchange: "binance", MarketType: "futures"},
			{ID: "bot_b", Symbol: "ETHUSDC", Exchange: "binance", MarketType: "spot"},
		},
		BotGroups: []config.BotGroup{
			{
				ID:     "grp_keep",
				BotIDs: []string{"bot_a", "bot_b"},
				HedgeConfig: config.HedgeConfig{
					HedgeRatio:        0.7,
					MaxDrawdown:       10,
					RebalanceInterval: 600,
				},
			},
		},
	}

	inputParams := map[string]interface{}{
		"leg_b_symbol":        "MANUALB",
		"hedge_ratio":         1.3,
		"rebalance_threshold": 0.2,
		"rebalance_interval":  120,
	}
	_, _, params, err := deriveHedgeGroupTaskDefaults(cfg, "grp_keep", "ETHUSDT", inputParams)
	if err != nil {
		t.Fatalf("derive defaults failed: %v", err)
	}

	if params["leg_b_symbol"] != "MANUALB" {
		t.Fatalf("expected leg_b_symbol to be preserved, got %#v", params["leg_b_symbol"])
	}
	if params["hedge_ratio"] != 1.3 {
		t.Fatalf("expected hedge_ratio to be preserved, got %#v", params["hedge_ratio"])
	}
	if params["rebalance_threshold"] != 0.2 {
		t.Fatalf("expected rebalance_threshold to be preserved, got %#v", params["rebalance_threshold"])
	}
	if params["rebalance_interval"] != 120 {
		t.Fatalf("expected rebalance_interval to be preserved, got %#v", params["rebalance_interval"])
	}
}

func TestGetBacktestTaskTradesExport_SingleStrategy(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tempDir := t.TempDir()
	resultsDir := filepath.Join(tempDir, "backtest", "results")
	if err := os.MkdirAll(resultsDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	taskID := "bt_export_test_single"
	resultJSON := `{
		"task_id": "` + taskID + `",
		"result": {
			"trades": [
				{"timestamp": 1735689600, "type": "buy", "price": 100, "quantity": 1, "fee": 0.1, "pnl": 0},
				{"timestamp": 1735689660, "type": "sell", "price": 101, "quantity": 1, "fee": 0.1, "pnl": 0.9}
			]
		}
	}`
	resultPath := filepath.Join(resultsDir, taskID+".json")
	if err := os.WriteFile(resultPath, []byte(resultJSON), 0644); err != nil {
		t.Fatalf("write result: %v", err)
	}

	origWd, _ := os.Getwd()
	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(origWd) })

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Params = gin.Params{{Key: "id", Value: taskID}}
	c.Request = httptest.NewRequest(http.MethodGet, "/api/backtest/tasks/"+taskID+"/trades/export?format=csv", nil)

	getBacktestTaskTradesExport(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, "Timestamp,Type,Price,Quantity,Fee,PnL") {
		preview := body
		if len(preview) > 80 {
			preview = preview[:80]
		}
		t.Errorf("expected CSV header, got: %s", preview)
	}
	if !strings.Contains(body, "buy") || !strings.Contains(body, "sell") {
		t.Errorf("expected trade rows, got: %s", body)
	}
}

func TestGetBacktestTaskTradesExport_MultiStrategy(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tempDir := t.TempDir()
	resultsDir := filepath.Join(tempDir, "backtest", "results")
	if err := os.MkdirAll(resultsDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	taskID := "bt_export_test_multi"
	resultJSON := `{
		"task_id": "` + taskID + `",
		"result": null,
		"multi_result": {
			"trades": [
				{"TradeID": "T1", "Side": "buy", "Price": 100, "Size": 0.5, "Timestamp": 1735689600000, "Slippage": 0.01},
				{"TradeID": "T2", "Side": "sell", "Price": 101, "Size": 0.5, "Timestamp": 1735689660000, "Slippage": 0.01}
			]
		}
	}`
	resultPath := filepath.Join(resultsDir, taskID+".json")
	if err := os.WriteFile(resultPath, []byte(resultJSON), 0644); err != nil {
		t.Fatalf("write result: %v", err)
	}

	origWd, _ := os.Getwd()
	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(origWd) })

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Params = gin.Params{{Key: "id", Value: taskID}}
	c.Request = httptest.NewRequest(http.MethodGet, "/api/backtest/tasks/"+taskID+"/trades/export?format=json", nil)

	getBacktestTaskTradesExport(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var rows []map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &rows); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}
	if rows[0]["type"] != "buy" || rows[1]["type"] != "sell" {
		t.Errorf("expected buy/sell, got %v %v", rows[0]["type"], rows[1]["type"])
	}
}

func TestGetBacktestTaskTradesExport_MultiStrategyWithCompletedTrades(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tempDir := t.TempDir()
	resultsDir := filepath.Join(tempDir, "backtest", "results")
	if err := os.MkdirAll(resultsDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	taskID := "bt_export_test_completed"
	resultJSON := `{
		"task_id": "` + taskID + `",
		"result": null,
		"multi_result": {
			"trades": [],
			"completed_trades": [
				{"timestamp": 1735689600000, "side": "long", "entry_price": 100, "exit_price": 101, "size": 1, "pnl": 0.9, "fee": 0.1, "slippage": 0.01},
				{"timestamp": 1735689720000, "side": "long", "entry_price": 102, "exit_price": 103, "size": 0.5, "pnl": 0.45, "fee": 0.05, "slippage": 0.005}
			]
		}
	}`
	resultPath := filepath.Join(resultsDir, taskID+".json")
	if err := os.WriteFile(resultPath, []byte(resultJSON), 0644); err != nil {
		t.Fatalf("write result: %v", err)
	}

	origWd, _ := os.Getwd()
	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(origWd) })

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Params = gin.Params{{Key: "id", Value: taskID}}
	c.Request = httptest.NewRequest(http.MethodGet, "/api/backtest/tasks/"+taskID+"/trades/export?format=json", nil)

	getBacktestTaskTradesExport(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var rows []map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &rows); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows from completed_trades, got %d", len(rows))
	}
	// 驗證 PnL 正確導出（修復前為 0）
	if pnl, ok := rows[0]["pnl"].(float64); !ok || pnl != 0.9 {
		t.Errorf("expected first row pnl=0.9, got %v (type %T)", rows[0]["pnl"], rows[0]["pnl"])
	}
	if pnl, ok := rows[1]["pnl"].(float64); !ok || pnl != 0.45 {
		t.Errorf("expected second row pnl=0.45, got %v (type %T)", rows[1]["pnl"], rows[1]["pnl"])
	}
	if fee, ok := rows[0]["fee"].(float64); !ok || fee != 0.1 {
		t.Errorf("expected first row fee=0.1, got %v", rows[0]["fee"])
	}
}

func TestGetBacktestTaskTradesExport_NoTrades(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tempDir := t.TempDir()
	resultsDir := filepath.Join(tempDir, "backtest", "results")
	if err := os.MkdirAll(resultsDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	taskID := "bt_export_test_empty"
	resultJSON := `{"task_id": "` + taskID + `", "result": {"trades": []}, "multi_result": null}`
	if err := os.WriteFile(filepath.Join(resultsDir, taskID+".json"), []byte(resultJSON), 0644); err != nil {
		t.Fatalf("write result: %v", err)
	}

	origWd, _ := os.Getwd()
	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(origWd) })

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Params = gin.Params{{Key: "id", Value: taskID}}
	c.Request = httptest.NewRequest(http.MethodGet, "/api/backtest/tasks/"+taskID+"/trades/export?format=csv", nil)

	getBacktestTaskTradesExport(c)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for no trades, got %d", w.Code)
	}
}
