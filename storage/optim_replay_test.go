package storage

import (
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"quantmesh/backtest"
)

func TestOptimReplayProtocolPersistsAcrossSQLiteReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "optimizer.db")
	store, err := NewSQLStorage(path)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &backtest.ReplayWalkForwardConfig{TrainDays: 3, TestDays: 1, StepDays: 1, Leverage: 3, BaseParams: map[string]interface{}{
		"grid_spacing": 20.0, "maker_fee_rate": 0.0002, "regime_filter": map[string]interface{}{"enabled": true, "kline_interval": "1h"},
	}}
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	task := &backtest.OptimTask{ID: "replay_protocol", Status: "pending", Strategy: "grid", Symbol: "BTCUSDT", Interval: "1h", StartTime: start, EndTime: start.Add(5 * 24 * time.Hour), TotalCapital: 10000, CreatedAt: start,
		SearchSpace: backtest.OptimSearchSpace{Strategy: "grid", ReplayWalkForward: cfg, Ranges: map[string]backtest.OptimParamRange{"grid_spacing": {Min: 15, Max: 30, Step: 15}}}, TotalCombos: 10}
	if err := store.CreateOptimTask(task); err != nil {
		store.Close()
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := NewSQLStorage(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	loaded, err := reopened.GetOptimTask(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded == nil || !reflect.DeepEqual(loaded.SearchSpace, task.SearchSpace) {
		t.Fatalf("protocol changed across restart: %+v", loaded)
	}
	list, err := reopened.ListOptimTasks(10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || !reflect.DeepEqual(list[0].SearchSpace.ReplayWalkForward, cfg) {
		t.Fatalf("list lost frozen protocol: %+v", list)
	}
}

func TestOptimReplayLegacyJSONRemainsReadable(t *testing.T) {
	var old backtest.OptimTask
	if err := old.SearchSpaceFromJSON(`{"strategy":"grid","ranges":{"grid_spacing":{"min":100,"max":200,"step":100}}}`); err != nil {
		t.Fatal(err)
	}
	if old.SearchSpace.ReplayWalkForward != nil {
		t.Fatal("legacy request reinterpreted as out-of-sample evidence")
	}
}
