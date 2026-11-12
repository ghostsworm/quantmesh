package optimrun

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"quantmesh/backtest"
	"quantmesh/backtest/optimizer/replayfixture"
	"quantmesh/backtest/visionfeed"
	"quantmesh/exchange"
)

func validReplayTask() *backtest.OptimTask {
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	return &backtest.OptimTask{
		Strategy: "grid", Symbol: "BTCUSDT", Interval: "1h", TotalCapital: replayfixture.Capital,
		StartTime: start, EndTime: start.Add(5 * 24 * time.Hour),
		SearchSpace: backtest.OptimSearchSpace{Strategy: "grid", ReplayWalkForward: replayfixture.Config(), Ranges: map[string]backtest.OptimParamRange{
			"grid_spacing": {Min: 20, Max: 40, Step: 20},
		}},
	}
}

func TestReplayAdmissionRejectsBeforePersistenceAndDownload(t *testing.T) {
	cases := []struct {
		name string
		edit func(*backtest.OptimTask)
	}{
		{"missing opt-in", func(task *backtest.OptimTask) { task.SearchSpace.ReplayWalkForward = nil }},
		{"strategy mismatch", func(task *backtest.OptimTask) { task.Strategy = "momentum" }},
		{"non-grid replay", func(task *backtest.OptimTask) { task.Strategy = "momentum"; task.SearchSpace.Strategy = "momentum" }},
		{"negative capital", func(task *backtest.OptimTask) { task.TotalCapital = -1 }},
		{"non-finite capital", func(task *backtest.OptimTask) { task.TotalCapital = math.NaN() }},
		{"equal time", func(task *backtest.OptimTask) { task.EndTime = task.StartTime }},
		{"future end", func(task *backtest.OptimTask) { task.EndTime = time.Now().Add(time.Hour) }},
		{"unsupported interval", func(task *backtest.OptimTask) { task.Interval = "aggtrade" }},
		{"invalid symbol", func(task *backtest.OptimTask) { task.Symbol = "../BTCUSDT" }},
		{"candidate limit", func(task *backtest.OptimTask) {
			task.SearchSpace.Ranges["grid_spacing"] = backtest.OptimParamRange{Min: 1, Max: 33, Step: 1}
		}},
		{"fold limit", func(task *backtest.OptimTask) { task.EndTime = task.StartTime.Add(16 * 24 * time.Hour) }},
		{"candle limit", func(task *backtest.OptimTask) {
			task.Interval = "1m"
			task.EndTime = task.StartTime.Add(100 * 24 * time.Hour)
		}},
		{"evaluation budget", func(task *backtest.OptimTask) {
			task.Interval = "1m"
			task.EndTime = task.StartTime.Add(43 * 24 * time.Hour)
			task.SearchSpace.ReplayWalkForward.TrainDays = 30
			task.SearchSpace.ReplayWalkForward.TestDays = 2
			task.SearchSpace.ReplayWalkForward.StepDays = 2
			task.SearchSpace.Ranges["grid_spacing"] = backtest.OptimParamRange{Min: 1, Max: 32, Step: 1}
		}},
		{"short train", func(task *backtest.OptimTask) { task.SearchSpace.ReplayWalkForward.TrainDays = 1 }},
		{"no full fold", func(task *backtest.OptimTask) { task.EndTime = task.StartTime.Add(3 * 24 * time.Hour) }},
		{"unknown fixed config", func(task *backtest.OptimTask) { task.SearchSpace.ReplayWalkForward.BaseParams["funding_signal"] = 1.0 }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := &fakeOptimStore{}
			manager := NewOptimTaskManager(store, nil, WithHistoricalDataLoader(func(context.Context, string, string, time.Time, time.Time) ([]*exchange.Candle, error) {
				t.Error("invalid request reached loader")
				return nil, nil
			}))
			task := validReplayTask()
			tc.edit(task)
			if err := manager.CreateAndRun(task); !errors.Is(err, ErrInvalidOptimTask) {
				t.Fatalf("error = %v, want invalid task", err)
			}
			if store.task != nil || len(manager.running) != 0 {
				t.Fatal("invalid request reached persistence or reserved capacity")
			}
		})
	}
}

func TestReplayPlanCountsBaselineAndTestRuns(t *testing.T) {
	task := validReplayTask()
	total, err := ValidateTask(task)
	if err != nil || total != 10 { // Two folds, each 2 candidates + 3 evaluations.
		t.Fatalf("total=%d error=%v", total, err)
	}
	converted := toOptimizerSearchSpace(task.SearchSpace)
	if converted.ReplayWalkForward != task.SearchSpace.ReplayWalkForward {
		t.Fatal("replay configuration lost during conversion")
	}
	task.Strategy, task.SearchSpace.Strategy = "momentum", "momentum"
	task.SearchSpace.ReplayWalkForward = nil
	if total, err := ValidateTask(task); err != nil || total != 2 {
		t.Fatalf("legacy non-grid path: total=%d error=%v", total, err)
	}
}

func TestReplayCoverageDropsCacheOutsideRequestedWindow(t *testing.T) {
	task := validReplayTask()
	candles := replayfixture.Candles("sideways")
	before, after := *candles[0], *candles[len(candles)-1]
	before.Timestamp -= time.Hour.Milliseconds()
	after.Timestamp += time.Hour.Milliseconds()
	loaded, err := replayCandlesForTask(task, append(append([]*exchange.Candle{&before}, candles...), &after))
	if err != nil || len(loaded) != len(candles) {
		t.Fatalf("coverage length=%d error=%v", len(loaded), err)
	}
	if _, err := replayCandlesForTask(task, candles[1:]); err == nil {
		t.Fatal("accepted incomplete coverage")
	}
	candles[1] = candles[0]
	if _, err := replayCandlesForTask(task, candles); err == nil {
		t.Fatal("accepted duplicate timestamps")
	}
}

func TestReloadPersistedReplayExecutesWithExactTotal(t *testing.T) {
	task := validReplayTask()
	task.ID = "reloaded"
	encoded, err := task.SearchSpaceToJSON()
	if err != nil {
		t.Fatal(err)
	}
	task.SearchSpace = backtest.OptimSearchSpace{}
	if err := task.SearchSpaceFromJSON(encoded); err != nil {
		t.Fatal(err)
	}
	task.TotalCombos, err = ValidateTask(task)
	if err != nil {
		t.Fatal(err)
	}
	store := &fakeOptimStore{task: task}
	dir := t.TempDir()
	manager := NewOptimTaskManager(store, nil, WithResultsDir(dir), WithReplayVisionLoader(func(context.Context, visionfeed.LoadRequest) (visionfeed.LoadResult, error) {
		return visionResult(task, false), nil
	}))
	if err := manager.RunTask(task.ID); err != nil {
		t.Fatal(err)
	}
	if store.progressDone != task.TotalCombos || len(store.statusCalls) != 2 || store.statusCalls[1] != "completed" {
		t.Fatalf("reloaded execution: progress=%d total=%d status=%v", store.progressDone, task.TotalCombos, store.statusCalls)
	}
	result, err := LoadOptimResult(dir, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if result.ReplayWalkForward == nil || len(result.ReplayWalkForward.Folds) != 2 || result.Completed != task.TotalCombos {
		t.Fatalf("reloaded task lost replay configuration: %+v", result)
	}
}

func TestReloadLegacyGridCannotFallBackToLegacyEngine(t *testing.T) {
	task := validReplayTask()
	task.ID = "legacy"
	task.SearchSpace.ReplayWalkForward = nil
	store := &fakeOptimStore{task: task}
	manager := NewOptimTaskManager(store, nil, WithHistoricalDataLoader(func(context.Context, string, string, time.Time, time.Time) ([]*exchange.Candle, error) {
		t.Error("legacy grid reload reached loader")
		return nil, nil
	}))
	if err := manager.RunTask(task.ID); err != nil {
		t.Fatal(err)
	}
	if len(store.statusCalls) != 1 || store.statusCalls[0] != "failed" {
		t.Fatalf("legacy reload status=%v", store.statusCalls)
	}
}
