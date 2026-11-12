package optimrun

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"quantmesh/backtest"
	"quantmesh/backtest/replay"
	"quantmesh/backtest/visionfeed"
	"quantmesh/exchange"
)

func TestReplayTaskUsesVerifiedVisionData(t *testing.T) {
	task := replayVisionTask(true)
	store := &fakeOptimStore{task: task}
	dir := t.TempDir()
	restCalled := false
	manager := NewOptimTaskManager(store, nil, WithResultsDir(dir),
		WithHistoricalDataLoader(func(context.Context, string, string, time.Time, time.Time) ([]*exchange.Candle, error) {
			restCalled = true
			return nil, errors.New("live exchange reached")
		}),
		WithReplayVisionLoader(func(context.Context, visionfeed.LoadRequest) (visionfeed.LoadResult, error) {
			return visionResult(task, true), nil
		}),
	)
	if err := manager.RunTask(task.ID); err != nil {
		t.Fatal(err)
	}
	if restCalled {
		t.Fatal("replay task reached the live exchange loader")
	}
	body, err := os.ReadFile(dir + "/" + task.ID + ".json")
	if err != nil {
		t.Fatal(err)
	}
	var saved struct {
		Replay struct {
			DataSource    string                   `json:"data_source"`
			VerifiedFiles []map[string]string      `json:"verified_files"`
			FundingFiles  []map[string]string      `json:"funding_files"`
			Limitations   []string                 `json:"limitations"`
			Baseline      map[string]interface{}   `json:"baseline_params"`
			Folds         []map[string]interface{} `json:"folds"`
		} `json:"replay_walk_forward"`
	}
	if err := json.Unmarshal(body, &saved); err != nil {
		t.Fatal(err)
	}
	got := saved.Replay
	if got.DataSource != visionfeed.SourceBinanceVisionUM1m || len(got.VerifiedFiles) != 1 || len(got.FundingFiles) != 1 {
		t.Fatalf("provenance missing: %+v", got)
	}
	if !strings.Contains(strings.Join(got.Limitations, ","), "binance_vision_um_1m_checksum_verified") {
		t.Fatalf("limitations = %v", got.Limitations)
	}
	series, _ := json.Marshal(got.Baseline["funding_series"])
	if !strings.Contains(string(series), "\"rate\"") || strings.Contains(string(series), "mark_price") {
		t.Fatalf("funding series = %s", series)
	}
	if got.Baseline["funding_rate"] != 0.0001 {
		t.Fatalf("fixed funding rate changed: %v", got.Baseline["funding_rate"])
	}
}

func TestReplayFundingStaysFixedWhenUnusedOrDisabled(t *testing.T) {
	for _, tc := range []struct {
		name    string
		enabled bool
		used    bool
	}{
		{"no verified funding", true, false},
		{"funding disabled", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			task := replayVisionTask(tc.enabled)
			store := &fakeOptimStore{task: task}
			dir := t.TempDir()
			manager := NewOptimTaskManager(store, nil, WithResultsDir(dir), WithReplayVisionLoader(
				func(context.Context, visionfeed.LoadRequest) (visionfeed.LoadResult, error) {
					return visionResult(task, tc.used), nil
				}))
			if err := manager.RunTask(task.ID); err != nil {
				t.Fatal(err)
			}
			body, err := os.ReadFile(dir + "/" + task.ID + ".json")
			if err != nil {
				t.Fatal(err)
			}
			var saved struct {
				Replay struct {
					Baseline map[string]interface{} `json:"baseline_params"`
				} `json:"replay_walk_forward"`
			}
			if err := json.Unmarshal(body, &saved); err != nil {
				t.Fatal(err)
			}
			baseline := saved.Replay.Baseline
			if _, present := baseline["funding_series"]; present {
				t.Fatalf("funding series injected: %+v", baseline["funding_series"])
			}
			if baseline["funding_enabled"] != tc.enabled {
				t.Fatalf("funding_enabled = %v, want %v", baseline["funding_enabled"], tc.enabled)
			}
		})
	}
}

func TestReplayVisionFailureWritesNoResult(t *testing.T) {
	task := replayVisionTask(true)
	store := &fakeOptimStore{task: task}
	dir := t.TempDir()
	restCalled := false
	manager := NewOptimTaskManager(store, nil, WithResultsDir(dir),
		WithHistoricalDataLoader(func(context.Context, string, string, time.Time, time.Time) ([]*exchange.Candle, error) {
			restCalled = true
			return nil, nil
		}),
		WithReplayVisionLoader(func(context.Context, visionfeed.LoadRequest) (visionfeed.LoadResult, error) {
			return visionfeed.LoadResult{}, errors.New("visionfeed: coverage: missing 1m file for 2025-01-02")
		}),
	)
	if err := manager.RunTask(task.ID); err != nil {
		t.Fatal(err)
	}
	if restCalled {
		t.Fatal("a failed verified load fell back to the live exchange")
	}
	if len(store.statusCalls) != 2 || store.statusCalls[1] != "failed" {
		t.Fatalf("status = %v", store.statusCalls)
	}
	if _, err := os.Stat(dir + "/" + task.ID + ".json"); !os.IsNotExist(err) {
		t.Fatalf("result file exists after failure: %v", err)
	}
}

func replayVisionTask(fundingEnabled bool) *backtest.OptimTask {
	task := validReplayTask()
	task.ID = "vision"
	task.SearchSpace.ReplayWalkForward.BaseParams["funding_enabled"] = fundingEnabled
	total, err := ValidateTask(task)
	if err != nil {
		panic(err)
	}
	task.TotalCombos = total
	return task
}

func visionResult(task *backtest.OptimTask, fundingUsed bool) visionfeed.LoadResult {
	plan, err := planReplayTask(task, 1)
	if err != nil {
		panic(err)
	}
	minutes := make([]*exchange.Candle, plan.bars*int(plan.barMs/time.Minute.Milliseconds()))
	for i := range minutes {
		stamp := plan.first + int64(i)*time.Minute.Milliseconds()
		price := 2000 + float64(i%30)
		minutes[i] = &exchange.Candle{Symbol: task.Symbol, Timestamp: stamp, Open: price, High: price + 5, Low: price - 5, Close: price, Volume: 1, IsClosed: true}
	}
	result := visionfeed.LoadResult{
		Candles: minutes,
		Provenance: visionfeed.Provenance{
			Source: visionfeed.SourceBinanceVisionUM1m,
			Files:  []visionfeed.FileDigest{{Name: "BTCUSDT/1m/BTCUSDT-1m-2025-01-01.csv", SHA256: "abc"}},
		},
	}
	if fundingUsed {
		result.FundingUsed = true
		result.Funding = []replay.FundingPoint{{Timestamp: plan.first + int64(8*time.Hour/time.Millisecond), Rate: 0.0002}}
		result.Provenance.FundingFiles = []visionfeed.FileDigest{{Name: "BTCUSDT/fundingRate/BTCUSDT-fundingRate-2025-01.csv", SHA256: "def"}}
	}
	return result
}
