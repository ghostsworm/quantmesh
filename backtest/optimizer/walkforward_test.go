package optimizer

import (
	"context"
	"errors"
	"math"
	"strings"
	"sync"
	"testing"

	"quantmesh/backtest"
	"quantmesh/exchange"
)

const (
	wfTestHourMs  = int64(60 * 60 * 1000)
	wfTestDays    = 100
	wfTestCapital = 10000.0
)

func hourlyCandles(days int) []*exchange.Candle {
	n := days * 24
	out := make([]*exchange.Candle, n)
	for i := 0; i < n; i++ {
		p := 100 + float64(i%24)
		out[i] = &exchange.Candle{Timestamp: int64(i) * wfTestHourMs, Open: p, High: p + 1, Low: p - 1, Close: p, Volume: 1}
	}
	return out
}

func equityOver(candles []*exchange.Candle, start, end float64) []backtest.EquityPoint {
	eq := make([]backtest.EquityPoint, len(candles))
	for i, c := range candles {
		frac := float64(i) / float64(len(candles)-1)
		eq[i] = backtest.EquityPoint{Timestamp: c.Timestamp, Equity: start + (end-start)*frac}
	}
	return eq
}

func TestBuildWalkForwardWindows_DefaultsAndNoOverlap(t *testing.T) {
	windows, err := BuildWalkForwardWindows(hourlyCandles(wfTestDays), WalkForwardConfig{Enabled: true})
	if err != nil {
		t.Fatalf("build windows: %v", err)
	}
	// 100 天數據、60/15/15：起點 0、15 天兩折（第三折測試窗口到 105 天，超出數據）
	if len(windows) != 2 {
		t.Fatalf("want 2 folds, got %d", len(windows))
	}
	for i, w := range windows {
		if w.TrainEnd != w.TestStart || w.TrainEnd-w.TrainStart != 60*24*wfTestHourMs || w.TestEnd-w.TestStart != 15*24*wfTestHourMs {
			t.Fatalf("fold %d window lengths wrong: train=[%d,%d) test=[%d,%d)", i, w.TrainStart, w.TrainEnd, w.TestStart, w.TestEnd)
		}
		for _, c := range w.Train {
			if c.Timestamp >= w.TestStart {
				t.Fatalf("fold %d train contains test-period candle ts=%d", i, c.Timestamp)
			}
		}
		if i > 0 && w.TestStart < windows[i-1].TestEnd {
			t.Fatalf("test windows overlap: fold %d starts %d before previous end %d", i, w.TestStart, windows[i-1].TestEnd)
		}
	}
	for _, stepDays := range []float64{5, 20} {
		if _, err := BuildWalkForwardWindows(hourlyCandles(wfTestDays), WalkForwardConfig{Enabled: true, TestDays: 15, StepDays: stepDays}); !errors.Is(err, errWalkForwardInvalidWindow) {
			t.Fatalf("step_days=%v must be rejected when it differs from test_days, got %v", stepDays, err)
		}
	}
}

func TestBuildWalkForwardWindowsRejectsIrregularOrInvalidCandles(t *testing.T) {
	tests := []struct {
		name   string
		mutate func([]*exchange.Candle)
	}{
		{name: "duplicate timestamp", mutate: func(cs []*exchange.Candle) { cs[10].Timestamp = cs[9].Timestamp }},
		{name: "missing interval", mutate: func(cs []*exchange.Candle) {
			for i := 100; i < len(cs); i++ {
				cs[i].Timestamp += wfTestHourMs
			}
		}},
		{name: "nil candle", mutate: func(cs []*exchange.Candle) { cs[10] = nil }},
		{name: "invalid OHLC", mutate: func(cs []*exchange.Candle) { cs[10].High = cs[10].Close - 1 }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			candles := hourlyCandles(wfTestDays)
			tt.mutate(candles)
			if _, err := BuildWalkForwardWindows(candles, WalkForwardConfig{Enabled: true}); !errors.Is(err, errWalkForwardInvalidCandles) {
				t.Fatalf("BuildWalkForwardWindows() error = %v, want invalid candle error", err)
			}
		})
	}
}

func TestValidateOptimConfigRejectsNonFiniteNumbers(t *testing.T) {
	for name, cfg := range map[string]OptimConfig{
		"lambda":           {Lambda: math.NaN()},
		"fee rate":         {FeeRate: math.Inf(1)},
		"slippage":         {SlippageRatio: math.Inf(-1)},
		"validation ratio": {ValidationRatio: math.NaN()},
	} {
		t.Run(name, func(t *testing.T) {
			if err := ValidateOptimConfig(cfg); !errors.Is(err, errInvalidOptimizerConfig) {
				t.Fatalf("expected non-finite config rejection, got %v", err)
			}
		})
	}
	for _, cfg := range []WalkForwardConfig{
		{Enabled: true, TrainDays: math.NaN()},
		{Enabled: true, TestDays: math.Inf(1)},
		{Enabled: true, StepDays: math.Inf(-1)},
		{Enabled: true, TrainDays: 1e-20, TestDays: 1e-20, StepDays: 1e-20},
	} {
		if _, err := BuildWalkForwardWindows(hourlyCandles(wfTestDays), cfg); !errors.Is(err, errInvalidOptimizerConfig) {
			t.Fatalf("expected non-finite walk-forward window rejection, got %v", err)
		}
	}
	if _, err := runWalkForward(context.Background(), nil, "BTCUSDT", nil, []backtest.GridBacktestParams{{}}, WalkForwardConfig{}, math.NaN(), wfTestCapital); !errors.Is(err, errInvalidOptimizerConfig) {
		t.Fatalf("expected direct walk-forward to reject non-finite lambda, got %v", err)
	}
}

func TestRunWalkForwardSkipsNonFiniteTrainingScores(t *testing.T) {
	candles := hourlyCandles(wfTestDays)
	fake := func(_ string, cs []*exchange.Candle, p backtest.GridBacktestParams, capital float64) (*backtest.BacktestResult, error) {
		if len(cs) > 0 && cs[len(cs)-1].Timestamp < 60*24*wfTestHourMs {
			if p.GridCount == 1 {
				return &backtest.BacktestResult{Equity: equityOver(cs, capital, math.MaxFloat64)}, nil
			}
			return &backtest.BacktestResult{Equity: equityOver(cs, capital, capital)}, nil
		}
		return &backtest.BacktestResult{Equity: equityOver(cs, capital, capital), Metrics: backtest.Metrics{}}, nil
	}
	result, err := runWalkForward(context.Background(), fake, "BTCUSDT", candles,
		[]backtest.GridBacktestParams{{GridCount: 1}, {GridCount: 2}}, WalkForwardConfig{Enabled: true}, 0.5, wfTestCapital)
	if err != nil {
		t.Fatalf("walk-forward: %v", err)
	}
	if result.Folds[0].BestParams.GridCount != 2 {
		t.Fatalf("non-finite training score selected params: %+v", result.Folds[0].BestParams)
	}
}

func TestRunWalkForwardRejectsMissingOutOfSampleEquity(t *testing.T) {
	candles := hourlyCandles(wfTestDays)
	run := func(_ string, cs []*exchange.Candle, _ backtest.GridBacktestParams, capital float64) (*backtest.BacktestResult, error) {
		if cs[0].Timestamp < 60*24*wfTestHourMs {
			return &backtest.BacktestResult{Equity: equityOver(cs, capital, capital)}, nil
		}
		return &backtest.BacktestResult{Metrics: backtest.Metrics{AnnualizedReturn: 10}}, nil
	}
	_, err := runWalkForward(context.Background(), run, "BTCUSDT", candles,
		[]backtest.GridBacktestParams{{GridCount: 1}}, WalkForwardConfig{Enabled: true}, 0.5, wfTestCapital)
	if err == nil || !strings.Contains(err.Error(), "test result") {
		t.Fatalf("walk-forward accepted missing out-of-sample equity curve: %v", err)
	}
}

func TestMetricsFromWalkForwardResultRecomputesSlippageFromTrades(t *testing.T) {
	candles := hourlyCandles(1)[:2]
	result := &backtest.BacktestResult{
		Equity:  equityOver(candles, wfTestCapital, wfTestCapital),
		Trades:  []backtest.Trade{{Timestamp: candles[0].Timestamp, Type: "buy", Price: 100, Quantity: 1, Fee: 0.1, SlippageLoss: 0.25}},
		Metrics: backtest.Metrics{TotalSlippageLoss: 999},
	}
	metrics, err := metricsFromWalkForwardResult(result, candles, wfTestCapital)
	if err != nil {
		t.Fatalf("metricsFromWalkForwardResult() error = %v", err)
	}
	if metrics.TotalSlippageLoss != 0.25 {
		t.Fatalf("total slippage = %v, want per-trade evidence sum 0.25", metrics.TotalSlippageLoss)
	}
}

// TestRunWalkForward_TrainNeverLeaksIntoTestScoring 驗證：
//  1. 每次回測調用的 K 線都完整落在某一折的訓練窗口或測試窗口內（沒有跨越訓練/測試邊界的調用）；
//  2. 參數只在訓練窗口上選出（訓練表現最好的候選被選中）；
//  3. 最終得分與指標只由測試窗口構成：訓練窗口收益極高、測試窗口持平，匯總收益必須為 0。
func TestRunWalkForward_TrainNeverLeaksIntoTestScoring(t *testing.T) {
	candles := hourlyCandles(wfTestDays)
	cfg := WalkForwardConfig{Enabled: true}
	windows, err := BuildWalkForwardWindows(candles, cfg)
	if err != nil {
		t.Fatalf("build windows: %v", err)
	}
	inRange := func(cs []*exchange.Candle, from, to int64) bool {
		return len(cs) > 0 && cs[0].Timestamp >= from && cs[len(cs)-1].Timestamp < to
	}

	var mu sync.Mutex
	testCalls := make(map[int]int)
	fake := func(symbol string, cs []*exchange.Candle, p backtest.GridBacktestParams, capital float64) (*backtest.BacktestResult, error) {
		mu.Lock()
		defer mu.Unlock()
		for _, w := range windows {
			switch {
			case inRange(cs, w.TrainStart, w.TrainEnd) && len(cs) == len(w.Train):
				// 訓練窗口：網格數越多收益越高（誇張的樣本內收益）
				end := capital * (1 + float64(p.GridCount))
				return &backtest.BacktestResult{Equity: equityOver(cs, capital, end)}, nil
			case inRange(cs, w.TestStart, w.TestEnd) && len(cs) == len(w.Test):
				testCalls[w.Index]++
				if p.GridCount != 3 {
					t.Errorf("fold %d evaluated test window with params not selected on train: %+v", w.Index, p)
				}
				return &backtest.BacktestResult{Equity: equityOver(cs, capital, capital), Metrics: backtest.Metrics{AnnualizedReturn: 1e9, TotalSlippageLoss: 1e9}}, nil
			}
		}
		t.Errorf("backtest called with candles [%d,%d] (n=%d) that match no single train/test window — leakage across boundary",
			cs[0].Timestamp, cs[len(cs)-1].Timestamp, len(cs))
		return nil, errors.New("unexpected window")
	}

	candidates := []backtest.GridBacktestParams{{GridCount: 1}, {GridCount: 3}, {GridCount: 2}}
	res, err := runWalkForward(context.Background(), fake, "BTCUSDT", candles, candidates, cfg, 0.5, wfTestCapital)
	if err != nil {
		t.Fatalf("walk-forward: %v", err)
	}
	if len(res.Folds) != len(windows) {
		t.Fatalf("want %d folds, got %d", len(windows), len(res.Folds))
	}
	for _, f := range res.Folds {
		if testCalls[f.Index] != 1 {
			t.Fatalf("fold %d test window evaluated %d times, want exactly 1", f.Index, testCalls[f.Index])
		}
		if f.BestParams.GridCount != 3 {
			t.Fatalf("fold %d selected %+v, want GridCount=3 (best on train)", f.Index, f.BestParams)
		}
		if math.Abs(f.TestScore) > 1e-9 || math.Abs(f.TestMetrics.AnnualizedReturn) > 1e-9 || f.TestMetrics.TotalSlippageLoss != 0 {
			t.Fatalf("fold %d trusted fabricated runner metrics over its flat test equity: score=%v metrics=%+v", f.Index, f.TestScore, f.TestMetrics)
		}
	}
	if math.Abs(res.Metrics.TotalReturn) > 1e-9 {
		t.Fatalf("aggregated metrics must only use flat test windows, got total return %.6f%%", res.Metrics.TotalReturn)
	}
	for _, pt := range res.Equity {
		inTest := false
		for _, w := range windows {
			if pt.Timestamp >= w.TestStart && pt.Timestamp < w.TestEnd {
				inTest = true
			}
		}
		if !inTest {
			t.Fatalf("stitched equity contains non-test timestamp %d", pt.Timestamp)
		}
	}
	if res.Score >= res.Folds[0].TrainScore {
		t.Fatalf("final score %.4f must not reflect train score %.4f", res.Score, res.Folds[0].TrainScore)
	}
}

func TestRunWalkForwardUsesCompoundedCapitalForEachFoldWithoutRescaling(t *testing.T) {
	candles := hourlyCandles(wfTestDays)
	windows, err := BuildWalkForwardWindows(candles, WalkForwardConfig{Enabled: true})
	if err != nil || len(windows) != 2 {
		t.Fatalf("build two-fold fixture: windows=%d err=%v", len(windows), err)
	}
	trainCapital := make([][]float64, len(windows))
	testCapital := make([]float64, len(windows))
	fake := func(_ string, cs []*exchange.Candle, _ backtest.GridBacktestParams, capital float64) (*backtest.BacktestResult, error) {
		for _, window := range windows {
			if len(cs) == len(window.Train) && cs[0].Timestamp == window.TrainStart {
				trainCapital[window.Index] = append(trainCapital[window.Index], capital)
				return &backtest.BacktestResult{Equity: equityOver(cs, capital, capital+1000)}, nil
			}
			if len(cs) == len(window.Test) && cs[0].Timestamp == window.TestStart {
				testCapital[window.Index] = capital
				return &backtest.BacktestResult{Equity: equityOver(cs, capital, capital+1000)}, nil
			}
		}
		return nil, errors.New("runner received candles outside walk-forward windows")
	}
	result, err := runWalkForward(context.Background(), fake, "BTCUSDT", candles,
		[]backtest.GridBacktestParams{{GridCount: 1}}, WalkForwardConfig{Enabled: true}, 0.5, wfTestCapital)
	if err != nil {
		t.Fatalf("run walk-forward: %v", err)
	}
	if testCapital[0] != wfTestCapital || testCapital[1] != wfTestCapital+1000 {
		t.Fatalf("test fold starting balances=%v, want [%v %v]", testCapital, wfTestCapital, wfTestCapital+1000)
	}
	if len(trainCapital[1]) != 1 || trainCapital[1][0] != wfTestCapital+1000 {
		t.Fatalf("second fold training balance=%v, want compounded %v", trainCapital[1], wfTestCapital+1000)
	}
	if got := result.Equity[len(result.Equity)-1].Equity; got != wfTestCapital+2000 {
		t.Fatalf("stitched ending equity=%v, want sequential fixed gains at actual balances (%v)", got, wfTestCapital+2000)
	}
	if math.Abs(result.Metrics.TotalReturn-20) > 1e-9 {
		t.Fatalf("stitched total return=%v%%, want 20%%", result.Metrics.TotalReturn)
	}
}

func TestGridSearch_WalkForwardPathAndSingleSplitStillAvailable(t *testing.T) {
	candles := hourlyCandles(wfTestDays)
	space := OptimSearchSpace{
		PriceLowRange:  Range{Min: 90, Max: 95, Step: 5},
		PriceHighRange: Range{Min: 130, Max: 135, Step: 5},
		GridCountRange: IntRange{Min: 5, Max: 10, Step: 5},
		OrderQtyRange:  Range{Min: 100, Max: 100, Step: 1},
	}
	g := &GridSearchOptimizer{}
	cfg := DefaultOptimConfig()
	cfg.Parallelism = 1
	cfg.WalkForward = &WalkForwardConfig{Enabled: true}
	wf, err := g.Run(context.Background(), "TESTUSDT", candles, space, cfg, wfTestCapital)
	if err != nil {
		t.Fatalf("walk-forward grid search: %v", err)
	}
	if wf.WalkForward == nil || len(wf.WalkForward.Folds) != 2 || wf.Method != "grid_walk_forward" {
		t.Fatalf("walk-forward result missing: %+v", wf)
	}

	cfg.WalkForward = nil
	cfg.ValidationRatio = 0.2
	single, err := g.Run(context.Background(), "TESTUSDT", candles, space, cfg, wfTestCapital)
	if err != nil {
		t.Fatalf("single split grid search: %v", err)
	}
	if single.WalkForward != nil || !single.HoldOutEnabled || single.Method != "grid" {
		t.Fatalf("single split path changed: %+v", single)
	}

	b := &BayesianOptimizer{}
	cfg.WalkForward = &WalkForwardConfig{Enabled: true}
	if _, err := b.Run(context.Background(), "TESTUSDT", candles, space, cfg, wfTestCapital); !errors.Is(err, errWalkForwardGridOnly) {
		t.Fatalf("bayesian with walk-forward should be rejected, got %v", err)
	}
}

func TestCalculateScore_FeeEfficiencyTerm(t *testing.T) {
	base := backtest.Metrics{AnnualizedReturn: 10, MaxDrawdown: 4, SharpeRatio: 1}
	noFees := CalculateScore(base, 0.5)

	efficient := base
	efficient.TotalFees, efficient.GridNetProfitToFeeRatio = 10, 2
	eaten := base
	eaten.TotalFees, eaten.GridNetProfitToFeeRatio = 10, -0.5
	extreme := base
	extreme.TotalFees, extreme.GridNetProfitToFeeRatio = 1e-6, 1e9

	if got := CalculateScore(efficient, 0.5) - noFees; math.Abs(got-2*GridNetProfitFeeWeight) > 1e-9 {
		t.Fatalf("efficient grid bonus=%v want %v", got, 2*GridNetProfitFeeWeight)
	}
	if CalculateScore(eaten, 0.5) >= noFees {
		t.Fatal("grids whose profit is eaten by fees must score lower")
	}
	if got := CalculateScore(extreme, 0.5) - noFees; math.Abs(got-gridNetProfitFeeRatioCap*GridNetProfitFeeWeight) > 1e-9 {
		t.Fatalf("fee ratio term must be capped, got %v", got)
	}
}
