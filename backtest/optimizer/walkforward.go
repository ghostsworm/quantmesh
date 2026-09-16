package optimizer

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"

	"quantmesh/backtest"
	"quantmesh/exchange"
)

const (
	// DefaultWalkForwardTrainDays 默認訓練窗口（天）
	DefaultWalkForwardTrainDays = 60.0
	// DefaultWalkForwardTestDays 默認測試窗口（天）
	DefaultWalkForwardTestDays = 15.0
	// msPerDay 一天毫秒數
	msPerDay = float64(24 * 60 * 60 * 1000)
)

var (
	errWalkForwardInvalidWindow = errors.New("optimizer: walk-forward windows must be positive and step >= test window")
	errWalkForwardNoFolds       = errors.New("optimizer: not enough candles for any walk-forward fold")
	errWalkForwardNoCandidates  = errors.New("optimizer: walk-forward needs at least one parameter set")
	errWalkForwardGridOnly      = errors.New("optimizer: walk-forward is only supported by grid search")
)

// WalkForwardConfig 滾動 walk-forward 配置：在訓練窗口上選參，在緊隨其後的測試窗口上評估，
// 最終得分與報告只由各測試窗口拼接而成。
type WalkForwardConfig struct {
	Enabled bool `json:"enabled"`
	// TrainDays 訓練窗口長度（天）；<=0 用默認 60
	TrainDays float64 `json:"train_days"`
	// TestDays 測試窗口長度（天）；<=0 用默認 15
	TestDays float64 `json:"test_days"`
	// StepDays 每折前移步長（天）；<=0 等於 TestDays。必須 >= TestDays，保證測試窗口互不重疊（拼接不重複計算）
	StepDays float64 `json:"step_days"`
}

func (c WalkForwardConfig) windowsMs() (train, test, step int64, err error) {
	trainDays, testDays, stepDays := c.TrainDays, c.TestDays, c.StepDays
	if trainDays <= 0 {
		trainDays = DefaultWalkForwardTrainDays
	}
	if testDays <= 0 {
		testDays = DefaultWalkForwardTestDays
	}
	if stepDays <= 0 {
		stepDays = testDays
	}
	if stepDays < testDays {
		return 0, 0, 0, fmt.Errorf("step_days=%.2f < test_days=%.2f: %w", stepDays, testDays, errWalkForwardInvalidWindow)
	}
	return int64(trainDays * msPerDay), int64(testDays * msPerDay), int64(stepDays * msPerDay), nil
}

// WalkForwardWindow 一折的時間窗口（毫秒，左閉右開）
type WalkForwardWindow struct {
	Index      int                `json:"index"`
	TrainStart int64              `json:"train_start"`
	TrainEnd   int64              `json:"train_end"`
	TestStart  int64              `json:"test_start"`
	TestEnd    int64              `json:"test_end"`
	Train      []*exchange.Candle `json:"-"`
	Test       []*exchange.Candle `json:"-"`
}

// BuildWalkForwardWindows 按時間構建滾動窗口：train=[s, s+train)，test=[s+train, s+train+test)，s 每折前移 step。
// 只保留測試窗口完整落在數據範圍內、且訓練/測試 K 線數分別不少於 minTrainBars/minValBars 的折。
func BuildWalkForwardWindows(candles []*exchange.Candle, cfg WalkForwardConfig) ([]WalkForwardWindow, error) {
	trainMs, testMs, stepMs, err := cfg.windowsMs()
	if err != nil {
		return nil, err
	}
	sorted := make([]*exchange.Candle, 0, len(candles))
	for _, c := range candles {
		if c != nil {
			sorted = append(sorted, c)
		}
	}
	if len(sorted) == 0 {
		return nil, errWalkForwardNoFolds
	}
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Timestamp < sorted[j].Timestamp })
	dataStart := sorted[0].Timestamp
	dataEnd := sorted[len(sorted)-1].Timestamp // 最後一根 K 線的時間戳（包含）

	var windows []WalkForwardWindow
	for start := dataStart; start+trainMs+testMs <= dataEnd+1; start += stepMs {
		w := WalkForwardWindow{
			TrainStart: start,
			TrainEnd:   start + trainMs,
			TestStart:  start + trainMs,
			TestEnd:    start + trainMs + testMs,
		}
		w.Train = sliceByTime(sorted, w.TrainStart, w.TrainEnd)
		w.Test = sliceByTime(sorted, w.TestStart, w.TestEnd)
		if len(w.Train) < minTrainBars || len(w.Test) < minValBars {
			continue
		}
		w.Index = len(windows)
		windows = append(windows, w)
	}
	if len(windows) == 0 {
		return nil, fmt.Errorf("%d candles spanning %.1f days, train=%.1fd test=%.1fd: %w",
			len(sorted), float64(dataEnd-dataStart)/msPerDay, float64(trainMs)/msPerDay, float64(testMs)/msPerDay, errWalkForwardNoFolds)
	}
	return windows, nil
}

// sliceByTime 返回 [from, to) 內的 K 線（輸入已排序）
func sliceByTime(sorted []*exchange.Candle, from, to int64) []*exchange.Candle {
	lo := sort.Search(len(sorted), func(i int) bool { return sorted[i].Timestamp >= from })
	hi := sort.Search(len(sorted), func(i int) bool { return sorted[i].Timestamp >= to })
	if lo >= hi {
		return nil
	}
	return sorted[lo:hi]
}

// WalkForwardFold 單折結果：參數只由訓練窗口選出，TestScore/TestMetrics 只來自測試窗口
type WalkForwardFold struct {
	WalkForwardWindow
	BestParams   backtest.GridBacktestParams `json:"best_params"`
	TrainScore   float64                     `json:"train_score"`
	TrainMetrics backtest.Metrics            `json:"train_metrics"`
	TestScore    float64                     `json:"test_score"`
	TestMetrics  backtest.Metrics            `json:"test_metrics"`
}

// WalkForwardResult walk-forward 匯總（Score/Metrics 只由測試窗口拼接計算）
type WalkForwardResult struct {
	Folds   []WalkForwardFold `json:"folds"`
	Score   float64           `json:"score"`
	Metrics backtest.Metrics  `json:"metrics"`
	// Equity 測試窗口按複利拼接的權益曲線
	Equity []backtest.EquityPoint `json:"equity"`
	// LatestParams 最後一折選出的參數（最接近當下、可用於實盤的參數）
	LatestParams backtest.GridBacktestParams `json:"latest_params"`
}

// backtestFunc 單次回測函數（測試可注入）
type backtestFunc func(symbol string, candles []*exchange.Candle, params backtest.GridBacktestParams, initialCapital float64) (*backtest.BacktestResult, error)

// RunWalkForward 對候選參數集執行滾動 walk-forward
func RunWalkForward(ctx context.Context, symbol string, candles []*exchange.Candle, candidates []backtest.GridBacktestParams, cfg WalkForwardConfig, lambda, initialCapital float64) (*WalkForwardResult, error) {
	return runWalkForward(ctx, BacktestRunner, symbol, candles, candidates, cfg, lambda, initialCapital)
}

func runWalkForward(ctx context.Context, run backtestFunc, symbol string, candles []*exchange.Candle, candidates []backtest.GridBacktestParams, cfg WalkForwardConfig, lambda, initialCapital float64) (*WalkForwardResult, error) {
	if len(candidates) == 0 {
		return nil, errWalkForwardNoCandidates
	}
	if initialCapital <= 0 {
		return nil, fmt.Errorf("walk-forward: initial capital must be positive, got %.4f", initialCapital)
	}
	windows, err := BuildWalkForwardWindows(candles, cfg)
	if err != nil {
		return nil, err
	}
	out := &WalkForwardResult{}
	var stitchedTrades []backtest.Trade
	capital := initialCapital
	lastPrice := 0.0
	for _, w := range windows {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("walk-forward fold %d: %w: %v", w.Index, errStopped, err)
		}
		fold := WalkForwardFold{WalkForwardWindow: w, TrainScore: math.Inf(-1)}
		found := false
		for _, p := range candidates {
			res, runErr := run(symbol, w.Train, p, initialCapital)
			if runErr != nil || res == nil {
				continue
			}
			s := CalculateScore(res.Metrics, lambda)
			if math.IsNaN(s) {
				continue
			}
			if !found || s > fold.TrainScore {
				fold.TrainScore, fold.TrainMetrics, fold.BestParams, found = s, res.Metrics, p, true
			}
		}
		if !found {
			return nil, fmt.Errorf("walk-forward fold %d [%d,%d): all %d candidates failed on train window", w.Index, w.TrainStart, w.TrainEnd, len(candidates))
		}
		testRes, runErr := run(symbol, w.Test, fold.BestParams, initialCapital)
		if runErr != nil || testRes == nil {
			return nil, fmt.Errorf("walk-forward fold %d test [%d,%d): %v", w.Index, w.TestStart, w.TestEnd, runErr)
		}
		fold.TestMetrics = testRes.Metrics
		fold.TestScore = CalculateScore(testRes.Metrics, lambda)

		// 複利拼接：本折權益按 capital/initialCapital 縮放
		scale := capital / initialCapital
		for _, pt := range testRes.Equity {
			out.Equity = append(out.Equity, backtest.EquityPoint{Timestamp: pt.Timestamp, Equity: pt.Equity * scale})
		}
		for _, tr := range testRes.Trades {
			stitchedTrades = append(stitchedTrades, backtest.Trade{
				Timestamp: tr.Timestamp, Type: tr.Type, Price: tr.Price,
				Quantity: tr.Quantity * scale, Fee: tr.Fee * scale, PnL: tr.PnL * scale,
			})
		}
		if len(testRes.Equity) > 0 {
			capital = testRes.Equity[len(testRes.Equity)-1].Equity * scale
		}
		if n := len(w.Test); n > 0 {
			lastPrice = w.Test[n-1].Close
		}
		out.Folds = append(out.Folds, fold)
	}
	out.Metrics = backtest.CalculateMetricsWithPrice(out.Equity, stitchedTrades, initialCapital, 0, lastPrice)
	out.Score = CalculateScore(out.Metrics, lambda)
	out.LatestParams = out.Folds[len(out.Folds)-1].BestParams
	return out, nil
}
