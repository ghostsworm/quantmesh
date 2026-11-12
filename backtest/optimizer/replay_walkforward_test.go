package optimizer

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"quantmesh/backtest"
	"quantmesh/backtest/optimizer/replayfixture"
	"quantmesh/backtest/replay"
	"quantmesh/exchange"
)

func replaySampleSpace() UniversalSearchSpace {
	return UniversalSearchSpace{Strategy: "grid", ReplayWalkForward: replayfixture.Config(), Ranges: map[string]ParamRange{"grid_spacing": {Min: 15, Max: 30, Step: 15}}}
}

func runReplaySample(t *testing.T, candles []*exchange.Candle) *ReplayWalkForwardResult {
	t.Helper()
	r, err := (&UniversalOptimizer{}).Run(context.Background(), "isolated-sample", "BTCUSDT", replayfixture.Interval, candles, replaySampleSpace(), replayfixture.Capital, nil)
	if err != nil {
		t.Fatal(err)
	}
	if r.BestByReturn != nil || r.BestBySharpe != nil || len(r.AllResults) != 0 {
		t.Fatal("test results leaked into universal rankings")
	}
	if r.Completed != r.TotalCombos {
		t.Fatalf("incomplete evaluations: %+v", r)
	}
	if _, err := json.Marshal(r); err != nil {
		t.Fatal(err)
	}
	return r.ReplayWalkForward
}

func TestReplayWalkForwardReproducibleAcrossMarketRegimes(t *testing.T) {
	for _, regime := range replayfixture.Regimes {
		t.Run(regime, func(t *testing.T) {
			first := runReplaySample(t, replayfixture.Candles(regime))
			second := runReplaySample(t, replayfixture.Candles(regime))
			if !reflect.DeepEqual(first, second) {
				t.Fatal("repeated replay produced different protocol or results")
			}
			if len(first.Folds) != 2 {
				t.Fatalf("complete terminal candle lost: folds=%d", len(first.Folds))
			}
			for _, fold := range first.Folds {
				if fold.TrainEnd != fold.TestStart {
					t.Fatal("train/test gap or overlap")
				}
				if fold.ParamsSHA256 == "" || fold.TrainDataSHA256 == fold.TestDataSHA256 {
					t.Fatal("missing frozen evidence")
				}
				if fold.Train.Fills == 0 || fold.Train.FeesTotal <= 0 {
					t.Fatal("sample must exercise matching and fees")
				}
				if fold.TestExcessReturn != fold.Test.TotalReturn-fold.BaselineTest.TotalReturn {
					t.Fatal("baseline comparison incorrect")
				}
				for _, c := range fold.Candidates {
					if c.Train.Score > fold.Train.Score {
						t.Fatal("selection did not maximize train score")
					}
				}
			}
		})
	}
}

func TestReplayWalkForwardFuturePerturbationCannotChangeTraining(t *testing.T) {
	original := replayfixture.Candles("sideways")
	control := runReplaySample(t, original)
	mutated := replayfixture.Candles("sideways")
	boundary := control.Folds[0].TestStart
	for _, c := range mutated {
		if c.Timestamp >= boundary {
			c.Open *= 3
			c.High *= 3
			c.Low *= 3
			c.Close *= 3
			c.Volume *= 2
		}
	}
	changed := runReplaySample(t, mutated)
	a, b := control.Folds[0], changed.Folds[0]
	if a.TrainDataSHA256 != b.TrainDataSHA256 || a.ParamsSHA256 != b.ParamsSHA256 || !reflect.DeepEqual(a.Candidates, b.Candidates) || a.Train != b.Train || a.BaselineTrain != b.BaselineTrain {
		t.Fatal("future prices/volume contaminated training, warm-up or selection")
	}
	if a.TestDataSHA256 == b.TestDataSHA256 || control.DataSHA256 == changed.DataSHA256 || control.ExperimentSHA256 == changed.ExperimentSHA256 {
		t.Fatal("negative-control mutation was not detected")
	}
}

func TestReplayWalkForwardFutureSettlementCannotBecomeSignal(t *testing.T) {
	candles := replayfixture.Candles("sideways")
	var results [2]*ReplayWalkForwardResult
	for i, rate := range []float64{0.0001, 0.9} {
		space := replaySampleSpace()
		space.ReplayWalkForward.BaseParams["funding_series"] = []map[string]interface{}{{
			"timestamp": candles[0].Timestamp + 4*24*3600000 + 8*3600000,
			"rate":      rate,
		}}
		r, err := (&UniversalOptimizer{}).Run(context.Background(), "funding-negative", "BTCUSDT", replayfixture.Interval, candles, space, replayfixture.Capital, nil)
		if err != nil {
			t.Fatal(err)
		}
		results[i] = r.ReplayWalkForward
	}
	a, b := results[0].Folds[0], results[1].Folds[0]
	if a.Train != b.Train || a.Test != b.Test || a.BaselineTrain != b.BaselineTrain || a.BaselineTest != b.BaselineTest || a.SelectedParams["grid_spacing"] != b.SelectedParams["grid_spacing"] {
		t.Fatal("future final settlement influenced earlier fills, costs, or parameter choice")
	}
	if results[0].ExperimentSHA256 == results[1].ExperimentSHA256 {
		t.Fatal("changed settlement dataset was not frozen into experiment digest")
	}
}

func TestReplayWalkForwardFreezesCallerOwnedInputs(t *testing.T) {
	candles := replayfixture.Candles("sideways")
	space := replaySampleSpace()
	control := runReplaySample(t, candles)
	mutated := false
	result, err := (&UniversalOptimizer{}).Run(context.Background(), "mutation", "BTCUSDT", replayfixture.Interval, candles, space, replayfixture.Capital, func(done, total int) {
		if mutated {
			return
		}
		mutated = true
		for _, c := range candles {
			c.Close = 1
			c.Low = 0.5
		}
		space.ReplayWalkForward.BaseParams["grid_spacing"] = 1000.0
		space.ReplayWalkForward.BaseParams["regime_filter"].(map[string]interface{})["enabled"] = false
		space.Ranges["grid_spacing"] = ParamRange{Min: 500, Max: 1000, Step: 500}
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(control, result.ReplayWalkForward) {
		t.Fatal("caller mutation changed frozen experiment")
	}
}

func TestReplayProvenanceChangesExperimentDigestOnly(t *testing.T) {
	candles := replayfixture.Candles("sideways")
	space := replaySampleSpace()
	plain, err := (&UniversalOptimizer{}).Run(context.Background(), "plain", "BTCUSDT", replayfixture.Interval, candles, space, replayfixture.Capital, nil)
	if err != nil {
		t.Fatal(err)
	}
	run := func(files []VerifiedFile) *ReplayWalkForwardResult {
		t.Helper()
		opt := &UniversalOptimizer{}
		WithReplayProvenance(ReplayProvenance{Source: "binance_vision_um_1m", Files: files})(opt)
		result, err := opt.Run(context.Background(), "verified", "BTCUSDT", replayfixture.Interval, candles, space, replayfixture.Capital, nil)
		if err != nil {
			t.Fatal(err)
		}
		return result.ReplayWalkForward
	}
	first := run([]VerifiedFile{{Name: "BTCUSDT/1m/a.csv", SHA256: "aaa"}})
	second := run([]VerifiedFile{{Name: "BTCUSDT/1m/a.csv", SHA256: "bbb"}})
	if first.ExperimentSHA256 == second.ExperimentSHA256 {
		t.Fatal("a different verified file did not change the experiment digest")
	}
	if first.BaselineSHA256 != second.BaselineSHA256 || first.BaselineSHA256 != plain.ReplayWalkForward.BaselineSHA256 {
		t.Fatal("provenance leaked into the baseline digest")
	}
	if first.ExperimentSHA256 == plain.ReplayWalkForward.ExperimentSHA256 {
		t.Fatal("verified run kept the unverified experiment digest")
	}
	if plain.ReplayWalkForward.DataSource != "" || len(plain.ReplayWalkForward.VerifiedFiles) != 0 {
		t.Fatal("a run without provenance recorded a data source")
	}
}

func TestReplayWalkForwardRejectsInvalidAdmission(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*UniversalSearchSpace)
	}{
		{"non-grid", func(s *UniversalSearchSpace) { s.Strategy = "dca" }},
		{"legacy ignored axis", func(s *UniversalSearchSpace) { s.Ranges["risk_average_window"] = ParamRange{Min: 10, Max: 10, Step: 1} }},
		{"too many candidates", func(s *UniversalSearchSpace) { s.Ranges["grid_spacing"] = ParamRange{Min: 1, Max: 33, Step: 1} }},
		{"overlap", func(s *UniversalSearchSpace) { s.ReplayWalkForward.StepDays = 0.5 }},
		{"fractional window count", func(s *UniversalSearchSpace) { s.Ranges["buy_window_size"] = ParamRange{Min: 1.5, Max: 1.5, Step: 1} }},
		{"future funding signal", func(s *UniversalSearchSpace) { s.ReplayWalkForward.BaseParams["funding_pricing"] = true }},
		{"funding object signal", func(s *UniversalSearchSpace) {
			s.ReplayWalkForward.BaseParams["funding_rate"] = map[string]interface{}{"enabled": true, "pricing_enabled": true}
		}},
		{"external ticks bypass frozen data", func(s *UniversalSearchSpace) { s.ReplayWalkForward.BaseParams["aggtrade_dir"] = "/tmp" }},
		{"wrong fee type", func(s *UniversalSearchSpace) { s.ReplayWalkForward.BaseParams["maker_fee_rate"] = "0.002" }},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			s := replaySampleSpace()
			tt.mutate(&s)
			if err := ValidateReplaySearchSpace(s); err == nil {
				t.Fatal("invalid configuration accepted")
			}
		})
	}
}

func TestReplayWalkForwardPreservesDerivedSpacing(t *testing.T) {
	p, err := normalizedReplayParams(map[string]interface{}{"price_low": 1000.0, "price_high": 1200.0, "grid_count": 10.0})
	if err != nil {
		t.Fatal(err)
	}
	if _, present := p["grid_spacing"]; present {
		t.Fatal("default spacing overwrote derived-grid semantics")
	}
	for _, count := range []float64{10, 20} {
		p["grid_count"] = count
		if err := validateReplayParams(p, 3); err != nil {
			t.Fatal(err)
		}
		cfg, err := replay.ConfigFromTask(&backtest.BacktestTask{Symbol: "BTCUSDT", TotalCapital: 10000, Leverage: 3, Params: p})
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Bot.Trading.PriceInterval != 200/count {
			t.Fatalf("grid_count=%v did not change derived spacing: %v", count, cfg.Bot.Trading.PriceInterval)
		}
	}
}

func TestReplayWalkForwardRejectsIntervalOverflow(t *testing.T) {
	for _, interval := range []string{"9007199254740992m", "9223372036854775807h", "99999999999999999999d"} {
		if _, _, err := freezeReplayCandles(replayfixture.Candles("sideways"), interval); err == nil {
			t.Fatalf("overflow interval %q accepted", interval)
		}
	}
}

func TestReplayWalkForwardRejectsDataLeaksAndCancellation(t *testing.T) {
	for _, name := range []string{"duplicate", "gap", "crossing candle", "interval mismatch", "unaligned candle"} {
		t.Run(name, func(t *testing.T) {
			candles := replayfixture.Candles("sideways")
			space := replaySampleSpace()
			interval := replayfixture.Interval
			switch name {
			case "duplicate":
				candles[10].Timestamp = candles[9].Timestamp
			case "gap":
				candles = append(candles[:10], candles[11:]...)
			case "crossing candle":
				space.ReplayWalkForward.TrainDays = 3.01
			case "interval mismatch":
				interval = "15m"
			case "unaligned candle":
				for _, c := range candles {
					c.Timestamp += 1800000
				}
			}
			if _, err := (&UniversalOptimizer{}).Run(context.Background(), "invalid", "BTCUSDT", interval, candles, space, replayfixture.Capital, nil); err == nil {
				t.Fatal("unsafe window data accepted")
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r, err := (&UniversalOptimizer{}).Run(ctx, "cancel", "BTCUSDT", replayfixture.Interval, replayfixture.Candles("sideways"), replaySampleSpace(), replayfixture.Capital, func(int, int) { cancel() })
	if !errors.Is(err, context.Canceled) || r != nil {
		t.Fatalf("canceled optimization returned rankings: %v %v", r, err)
	}
}
