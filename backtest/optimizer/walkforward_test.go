package optimizer

import (
	"context"
	"errors"
	"math"
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
	if _, err := BuildWalkForwardWindows(hourlyCandles(wfTestDays), WalkForwardConfig{Enabled: true, TestDays: 15, StepDays: 5}); !errors.Is(err, errWalkForwardInvalidWindow) {
		t.Fatalf("step < test must be rejected, got %v", err)
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
				return &backtest.BacktestResult{Equity: equityOver(cs, capital, end), Metrics: backtest.Metrics{AnnualizedReturn: 1000 * float64(p.GridCount)}}, nil
			case inRange(cs, w.TestStart, w.TestEnd) && len(cs) == len(w.Test):
				testCalls[w.Index]++
				if p.GridCount != 3 {
					t.Errorf("fold %d evaluated test window with params not selected on train: %+v", w.Index, p)
				}
				return &backtest.BacktestResult{Equity: equityOver(cs, capital, capital)}, nil
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
