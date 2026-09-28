package optimizer

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestUniversalCanceledDuringProgressNeverReturnsRankings(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	space := UniversalSearchSpace{Strategy: "unsupported", Ranges: map[string]ParamRange{
		"x": {Min: 1, Max: 1000, Step: 1},
	}}
	last := 0
	result, err := (&UniversalOptimizer{}).Run(ctx, "id", "BTCUSDT", "1h", nil, space, 1000, func(done, total int) {
		if done != last+1 || total != 1000 {
			t.Errorf("unordered progress: %d after %d, total %d", done, last, total)
		}
		last = done
		cancel()
	})
	if err != context.Canceled || result != nil || last == 0 {
		t.Fatalf("canceled run returned rankings: result=%v err=%v progress=%d", result, err, last)
	}
}

func TestUniversalAllFailedIsNotSuccessfulEmptyOptimization(t *testing.T) {
	space := UniversalSearchSpace{Strategy: "unsupported", Ranges: map[string]ParamRange{
		"x": {Min: 1, Max: 4, Step: 1},
	}}
	last := 0
	result, err := (&UniversalOptimizer{}).Run(context.Background(), "id", "BTCUSDT", "1h", nil, space, 1000, func(done, total int) {
		if done != last+1 || total != 4 {
			t.Errorf("progress = %d after %d", done, last)
		}
		last = done
	})
	if result != nil || err == nil || !strings.Contains(err.Error(), "4 failed") || last != 4 {
		t.Fatalf("failure presented as success: %v %v progress=%d", result, err, last)
	}
}

func TestUniversalSuccessfulResultsHaveExecutedParameters(t *testing.T) {
	space := UniversalSearchSpace{Strategy: "dca", Ranges: map[string]ParamRange{
		"interval_days": {Min: 1, Max: 3, Step: 1},
	}}
	result, err := (&UniversalOptimizer{}).Run(context.Background(), "id", "BTCUSDT", "1h", optimizerCandles(80), space, 1000, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Completed != 3 || result.Failed != 0 || len(result.AllResults) != 3 || result.BestByReturn == nil {
		t.Fatalf("incomplete successful result: %+v", result)
	}
	for _, candidate := range result.AllResults {
		if candidate.Params == nil {
			t.Fatal("unexecuted zero value entered rankings")
		}
	}
	if _, err := json.Marshal(result); err != nil {
		t.Fatalf("result cannot be persisted: %v", err)
	}
}
