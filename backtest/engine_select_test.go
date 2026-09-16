package backtest

import (
	"os"
	"path/filepath"
	"testing"

	"quantmesh/exchange"
)

func writeTestKlineFile(t *testing.T, dir, name string) {
	t.Helper()
	csv := "timestamp,open,high,low,close,volume\n" +
		"1735689600000,100,101,99,100,1000\n" +
		"1735689660000,100,102,99,101,1000\n" +
		"1735689720000,101,103,100,102,1000\n"
	if err := os.WriteFile(filepath.Join(dir, name), []byte(csv), 0644); err != nil {
		t.Fatalf("write kline file: %v", err)
	}
}

func TestNormalizeEngine(t *testing.T) {
	cases := []struct {
		in, want string
		wantErr  bool
	}{
		{"", EngineLegacy, false},
		{"legacy", EngineLegacy, false},
		{" Replay ", EngineReplay, false},
		{"magic", "", true},
	}
	for _, tc := range cases {
		got, err := NormalizeEngine(tc.in)
		if (err != nil) != tc.wantErr || got != tc.want {
			t.Errorf("NormalizeEngine(%q)=%q,%v want %q,err=%v", tc.in, got, err, tc.want, tc.wantErr)
		}
	}
}

func TestTaskManager_GridEngineReplayDispatch(t *testing.T) {
	tempDir := t.TempDir()
	const klineFile = "1m_binance_BTCUSDT_20250101.csv"
	writeTestKlineFile(t, tempDir, klineFile)

	newTask := func(engine string) *BacktestTask {
		params := map[string]interface{}{"grid_spacing": 1.0, "order_quantity": 10.0}
		if engine != "" {
			params[ParamKeyEngine] = engine
		}
		return &BacktestTask{ID: "bt_engine_" + engine, Strategy: "grid", Symbol: "BTCUSDT", TotalCapital: 1000,
			DataSource: "kline_file", KlineFile: klineFile, Params: params}
	}

	// 未註冊回放引擎時 engine=replay 失敗
	store := &fakeTaskStore{task: newTask(EngineReplay)}
	manager := NewTaskManager(store, nil, tempDir)
	manager.SetOutputDirs(filepath.Join(tempDir, "results"), filepath.Join(tempDir, "reports"))
	if err := manager.RunTask(store.task.ID); err != nil {
		t.Fatalf("run task: %v", err)
	}
	if store.task.Status != "failed" {
		t.Fatalf("replay without runner should fail, got %q", store.task.Status)
	}

	// 註冊後分派到回放引擎，legacy 默認路徑不調用回放
	calls := 0
	manager.SetReplayRunner(func(task *BacktestTask, candles []*exchange.Candle) (*BacktestResult, interface{}, error) {
		calls++
		return &BacktestResult{Symbol: task.Symbol, Strategy: "grid_replay", InitialCapital: task.TotalCapital, FinalCapital: task.TotalCapital},
			map[string]interface{}{"fills": len(candles)}, nil
	})
	store.task = newTask(EngineReplay)
	if err := manager.RunTask(store.task.ID); err != nil {
		t.Fatalf("run replay task: %v", err)
	}
	if store.task.Status != "completed" || calls != 1 {
		t.Fatalf("replay dispatch: status=%q err=%q calls=%d", store.task.Status, store.task.Error, calls)
	}
	payload, err := LoadResult(manager.resultsDir, store.task.ID)
	if err != nil {
		t.Fatalf("load result: %v", err)
	}
	if payload.ReplayMetrics == nil || payload.Comparison != nil {
		t.Fatalf("replay payload should carry replay_metrics and no legacy comparison: %+v", payload)
	}

	store.task = newTask("")
	if err := manager.RunTask(store.task.ID); err != nil {
		t.Fatalf("run legacy task: %v", err)
	}
	if store.task.Status != "completed" || calls != 1 {
		t.Fatalf("legacy default must not use replay: status=%q err=%q calls=%d", store.task.Status, store.task.Error, calls)
	}

	store.task = newTask("bogus")
	_ = manager.RunTask(store.task.ID)
	if store.task.Status != "failed" {
		t.Fatalf("unknown engine should fail task, got %q", store.task.Status)
	}
}
