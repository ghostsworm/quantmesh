package storage

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"quantmesh/backtest"
)

func TestConfigEntryTypedValuesAndSQLiteStorageCRUD(t *testing.T) {
	ctx := context.Background()
	store, err := NewConfigStorage(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatalf("new config storage: %v", err)
	}
	sqlStore := store.(*SQLStorage)
	t.Cleanup(func() { _ = sqlStore.db.Close() })

	stringEntry, err := NewConfigEntry(ScopeGlobal, "", "app.name", "QuantMesh", "app", "Name", "Application name")
	if err != nil {
		t.Fatalf("new string entry: %v", err)
	}
	stringEntry.Required = true
	stringEntry.ValidateRegexp = "^Quant"
	if err := store.SetConfig(ctx, stringEntry, "tester"); err != nil {
		t.Fatalf("set string config: %v", err)
	}
	loaded, err := store.GetConfig(ctx, ScopeGlobal, "", "app.name")
	if err != nil || loaded == nil {
		t.Fatalf("get string config loaded=%#v err=%v", loaded, err)
	}
	if value, err := loaded.GetTypedValue(); err != nil || value != "QuantMesh" {
		t.Fatalf("typed string value=%#v err=%v", value, err)
	}

	numberEntry, _ := NewConfigEntry(ScopeBot, "bot-1", "grid.count", int64(8), "grid", "Grid Count", "")
	boolEntry, _ := NewConfigEntry(ScopeBot, "bot-1", "enabled", true, "grid", "Enabled", "")
	jsonEntry, _ := NewConfigEntry(ScopeBot, "bot-1", "risk", map[string]interface{}{"max": 3.0}, "risk", "Risk", "")
	arrayEntry, _ := NewConfigEntry(ScopeBot, "bot-1", "symbols", []interface{}{"BTCUSDT", "ETHUSDT"}, "grid", "Symbols", "")
	for _, entry := range []*ConfigEntry{numberEntry, boolEntry, jsonEntry, arrayEntry} {
		if err := store.SetConfig(ctx, entry, "tester"); err != nil {
			t.Fatalf("set config %s: %v", entry.Key, err)
		}
	}

	byKeys, err := store.GetConfigByKeys(ctx, ScopeBot, "bot-1", []string{"grid.count", "enabled", "missing"})
	if err != nil || len(byKeys) != 2 {
		t.Fatalf("by keys len=%d err=%v", len(byKeys), err)
	}
	if empty, err := store.GetConfigByKeys(ctx, ScopeBot, "bot-1", nil); err != nil || empty != nil {
		t.Fatalf("empty keys = %#v err=%v", empty, err)
	}
	byScope, err := store.GetConfigsByScope(ctx, ScopeBot, "bot-1")
	if err != nil || len(byScope) != 4 {
		t.Fatalf("by scope len=%d err=%v", len(byScope), err)
	}
	byCategory, err := store.GetConfigsByCategory(ctx, ScopeBot, "grid")
	if err != nil || len(byCategory) != 3 {
		t.Fatalf("by category len=%d err=%v", len(byCategory), err)
	}
	all, err := store.GetAllConfigs(ctx)
	if err != nil || len(all) != 5 {
		t.Fatalf("all configs len=%d err=%v", len(all), err)
	}

	boolLoaded, err := store.GetConfig(ctx, ScopeBot, "bot-1", "enabled")
	if err != nil || boolLoaded == nil || !boolLoaded.BoolValue {
		t.Fatalf("bool loaded=%#v err=%v", boolLoaded, err)
	}
	if value, err := jsonEntry.GetTypedValue(); err != nil || value == nil {
		t.Fatalf("json typed value=%#v err=%v", value, err)
	}
	if value, err := arrayEntry.GetTypedValue(); err != nil || len(value.([]interface{})) != 2 {
		t.Fatalf("array typed value=%#v err=%v", value, err)
	}
	badJSON := &ConfigEntry{Type: TypeJSON, JSONValue: "{"}
	if _, err := badJSON.GetTypedValue(); err == nil {
		t.Fatalf("bad JSON should fail")
	}
	badArray := &ConfigEntry{Type: TypeArray, JSONValue: "{"}
	if _, err := badArray.GetTypedValue(); err == nil {
		t.Fatalf("bad array should fail")
	}
	if _, err := (&ConfigEntry{Type: ConfigType("bad")}).GetTypedValue(); err == nil {
		t.Fatalf("bad typed value should fail")
	}

	stringEntry.Value = "QuantMesh Pro"
	if err := store.SetConfig(ctx, stringEntry, "tester2"); err != nil {
		t.Fatalf("update string config: %v", err)
	}
	if _, err := store.GetConfigHistoryByKey(ctx, ScopeGlobal, "", "app.name", 10); err != nil {
		t.Fatalf("history by key: %v", err)
	}
	if _, err := store.GetConfigHistory(ctx, loaded.ID, 10); err != nil {
		t.Fatalf("history by id: %v", err)
	}

	if err := store.InitializeConfigs(ctx, []*ConfigEntry{stringEntry}); err != nil {
		t.Fatalf("initialize existing config: %v", err)
	}
	if err := store.DeleteConfig(ctx, ScopeBot, "bot-1", "enabled"); err != nil {
		t.Fatalf("delete config: %v", err)
	}
	if deleted, _ := store.GetConfig(ctx, ScopeBot, "bot-1", "enabled"); deleted != nil {
		t.Fatalf("deleted config still exists: %#v", deleted)
	}

	for _, entry := range []*ConfigEntry{
		{Type: TypeString},
		{Key: "bad.type", Type: ConfigType("bad")},
		{Key: "required", Type: TypeString, Required: true},
		{Key: "regex", Type: TypeString, Value: "abc", ValidateRegexp: "["},
		{Key: "regex", Type: TypeString, Value: "abc", ValidateRegexp: "^def$"},
	} {
		if err := store.ValidateConfig(entry); err == nil {
			t.Fatalf("invalid config should fail: %#v", entry)
		}
	}

	if _, err := NewConfigStorageByType("unknown", ""); err == nil {
		t.Fatalf("unknown config storage type should fail")
	}
	if _, err := NewConfigStorageByType("sqlite", filepath.Join(t.TempDir(), "other.db")); err != nil {
		t.Fatalf("sqlite config storage by type: %v", err)
	}
}

func TestSQLStorageBacktestAndOptimTaskStores(t *testing.T) {
	store, err := NewSQLStorage(filepath.Join(t.TempDir(), "main.db"))
	if err != nil {
		t.Fatalf("new sql storage: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	now := time.Now().Truncate(time.Millisecond)
	started := now.Add(time.Second)
	completed := now.Add(2 * time.Second)
	task := &backtest.BacktestTask{
		ID:           "bt-1",
		Status:       "pending",
		Mode:         backtest.TaskModeSingleStrategy,
		BotID:        "bot-1",
		Strategy:     "grid",
		Symbol:       "BTCUSDT",
		Interval:     "1m",
		StartTime:    now.Add(-time.Hour),
		EndTime:      now,
		Params:       map[string]interface{}{"grid_count": float64(5)},
		TotalCapital: 1000,
		Progress:     10,
		CreatedAt:    now,
		StartedAt:    &started,
	}
	if err := store.CreateBacktestTask(task); err != nil {
		t.Fatalf("create backtest task: %v", err)
	}
	loaded, err := store.GetBacktestTask("bt-1")
	if err != nil || loaded == nil || loaded.Params["grid_count"].(float64) != 5 {
		t.Fatalf("loaded backtest task=%#v err=%v", loaded, err)
	}
	if missing, err := store.GetBacktestTask("missing"); err != nil || missing != nil {
		t.Fatalf("missing backtest task=%#v err=%v", missing, err)
	}
	task.Status = "completed"
	task.Progress = 100
	task.CompletedAt = &completed
	task.ResultPath = "/tmp/result.json"
	task.ReportPath = "/tmp/report.md"
	if err := store.UpdateBacktestTask(task); err != nil {
		t.Fatalf("update backtest task: %v", err)
	}
	if err := store.UpdateBacktestTaskStatus("bt-1", "failed", 90, &started, &completed, "boom", "r", "p"); err != nil {
		t.Fatalf("update backtest task status: %v", err)
	}
	tasks, err := store.ListBacktestTasks(10, 0)
	if err != nil || len(tasks) != 1 || tasks[0].Status != "failed" {
		t.Fatalf("list backtest tasks=%#v err=%v", tasks, err)
	}
	if store.GetBacktestTaskStore() == nil {
		t.Fatalf("backtest task store should be available")
	}
	if err := store.DeleteBacktestTask("bt-1"); err != nil {
		t.Fatalf("delete backtest task: %v", err)
	}

	optim := &backtest.OptimTask{
		ID:           "opt-1",
		Status:       "pending",
		Strategy:     "grid",
		Symbol:       "BTCUSDT",
		Interval:     "1m",
		StartTime:    now.Add(-time.Hour),
		EndTime:      now,
		TotalCapital: 1000,
		SearchSpace: backtest.OptimSearchSpace{
			Strategy: "grid",
			Ranges: map[string]backtest.OptimParamRange{
				"grid_count": {Min: 2, Max: 4, Step: 1},
			},
		},
		Progress:    1,
		TotalCombos: 3,
		CreatedAt:   now,
	}
	if err := store.CreateOptimTask(optim); err != nil {
		t.Fatalf("create optim task: %v", err)
	}
	loadedOptim, err := store.GetOptimTask("opt-1")
	if err != nil || loadedOptim == nil || loadedOptim.SearchSpace.Strategy != "grid" {
		t.Fatalf("loaded optim task=%#v err=%v", loadedOptim, err)
	}
	if missing, err := store.GetOptimTask("missing"); err != nil || missing != nil {
		t.Fatalf("missing optim task=%#v err=%v", missing, err)
	}
	if err := store.UpdateOptimTaskProgress("opt-1", 2, 66); err != nil {
		t.Fatalf("update optim progress: %v", err)
	}
	if err := store.UpdateOptimTaskStatus("opt-1", "completed", &started, &completed, "", "/tmp/optim.json"); err != nil {
		t.Fatalf("update optim status: %v", err)
	}
	optimTasks, err := store.ListOptimTasks(10, 0)
	if err != nil || len(optimTasks) != 1 || optimTasks[0].CompletedCombos != 2 || optimTasks[0].Status != "completed" {
		t.Fatalf("list optim tasks=%#v err=%v", optimTasks, err)
	}
	if store.GetOptimTaskStore() == nil {
		t.Fatalf("optim task store should be available")
	}
	if err := store.DeleteOptimTask("opt-1"); err != nil {
		t.Fatalf("delete optim task: %v", err)
	}

	if nilInt64(nil) != nil || nilInt64Opt(nil) != nil {
		t.Fatalf("nil time helpers should return nil")
	}
}
