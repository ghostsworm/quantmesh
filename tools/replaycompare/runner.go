package main

import (
	"fmt"
	"math"
	"sort"
	"sync"
	"time"

	"quantmesh/backtest"
	"quantmesh/backtest/replay"
	"quantmesh/exchange"
)

// annualizationDays Sharpe-like 比值的年化天數（加密市場全年交易）
const annualizationDays = 365.0

// Segment 回放時間段 [StartMs, EndMs)
type Segment struct {
	Name    string `json:"name"`
	StartMs int64  `json:"start_ms"`
	EndMs   int64  `json:"end_ms"`
	Start   string `json:"start"`
	End     string `json:"end"` // 不含
}

// SplitSegments 返回全段 + 連續 n 段（最後一段吸收餘數）
func SplitSegments(startMs, endMs int64, segDays int, n int) []Segment {
	segs := []Segment{newSegment("full", startMs, endMs)}
	for i := 0; i < n; i++ {
		s := startMs + int64(i*segDays)*DayMs
		e := s + int64(segDays)*DayMs
		if i == n-1 || e > endMs {
			e = endMs
		}
		if s >= e {
			break
		}
		segs = append(segs, newSegment(fmt.Sprintf("seg%d", i+1), s, e))
	}
	return segs
}

func newSegment(name string, s, e int64) Segment {
	return Segment{Name: name, StartMs: s, EndMs: e,
		Start: time.UnixMilli(s).UTC().Format("2006-01-02"), End: time.UnixMilli(e).UTC().Format("2006-01-02")}
}

// RunSummary 單次運行的報告指標
type RunSummary struct {
	Profile  string  `json:"profile"`
	Symbol   string  `json:"symbol"`
	Base     string  `json:"base"`
	Segment  string  `json:"segment"`
	Variant  string  `json:"variant"`
	Interval float64 `json:"price_interval"`
	Blocked  string  `json:"blocked,omitempty"`
	Error    string  `json:"error,omitempty"`

	StartPrice float64 `json:"start_price"`
	EndPrice   float64 `json:"end_price"`

	NetPnL           float64 `json:"net_pnl"`
	RealizedPnL      float64 `json:"realized_pnl"`
	UnrealizedPnL    float64 `json:"unrealized_pnl"`
	FeesMaker        float64 `json:"fees_maker"`
	FeesTaker        float64 `json:"fees_taker"`
	FundingPaid      float64 `json:"funding_paid"`
	MaxDrawdownAbs   float64 `json:"max_drawdown_abs"`
	MaxDrawdownPct   float64 `json:"max_drawdown_pct"`
	Fills            int     `json:"fills"`
	MakerRatio       float64 `json:"maker_ratio"`
	ClosedGrids      int     `json:"closed_grids"`
	NetProfitPerGrid float64 `json:"net_profit_per_grid"`
	ProfitToFee      float64 `json:"grid_net_profit_to_fee_ratio"`
	TimeInMarketPct  float64 `json:"time_in_market_pct"`
	MaxAbsNotional   float64 `json:"max_abs_notional"`
	AvgAbsNotional   float64 `json:"time_weighted_avg_abs_notional"`
	DailySharpeLike  float64 `json:"daily_sharpe_like"`
	TradingDays      int     `json:"trading_days"`
	PostOnlyRejects  int     `json:"post_only_rejects"`
	MarginRejects    int     `json:"margin_rejects"`
	OrdersPlaced     int     `json:"orders_placed"`
	OrdersCanceled   int     `json:"orders_canceled"`
	// OrderCleanerRuns 模擬時間上執行訂單清理的輪數
	OrderCleanerRuns int `json:"order_cleaner_runs"`
	// 以下僅對注入 regime 檢測器的變體有值：檢測器拉取失敗次數（段首預熱期）、間隔被修改次數、
	// 每 30s 模擬時間採樣的「當前間隔/base」平均值、有效狀態占比（%）
	RegimeRefreshErrors  int                `json:"regime_refresh_errors,omitempty"`
	IntervalChanges      int                `json:"interval_changes,omitempty"`
	MeanIntervalMultiple float64            `json:"mean_interval_multiple,omitempty"`
	RegimeSharePct       map[string]float64 `json:"regime_share_pct,omitempty"`
	// MaxNoFillGapHours 最長無成交間隔（小時），識別網格停擺
	MaxNoFillGapHours float64 `json:"max_no_fill_gap_hours"`
	Ticks             int     `json:"ticks"`
	RuntimeSec        float64 `json:"runtime_sec"`
}

// DailySharpeLike 以 UTC 日末權益計算日收益，mean/std×sqrt(365)；少於 2 個日收益或 std=0 時返回 0
func DailySharpeLike(curve []backtest.EquityPoint) (float64, int) {
	if len(curve) < 2 {
		return 0, 0
	}
	var dayEquity []float64
	curDay := int64(math.MinInt64)
	for _, p := range curve {
		d := p.Timestamp - p.Timestamp%DayMs
		if d != curDay {
			dayEquity = append(dayEquity, p.Equity)
			curDay = d
		} else {
			dayEquity[len(dayEquity)-1] = p.Equity
		}
	}
	// 首日起點權益作為第 0 天基準
	base := append([]float64{curve[0].Equity}, dayEquity...)
	rets := make([]float64, 0, len(base))
	for i := 1; i < len(base); i++ {
		if base[i-1] > 0 {
			rets = append(rets, base[i]/base[i-1]-1)
		}
	}
	if len(rets) < 2 {
		return 0, len(rets)
	}
	mean, sq := 0.0, 0.0
	for _, r := range rets {
		mean += r
	}
	mean /= float64(len(rets))
	for _, r := range rets {
		sq += (r - mean) * (r - mean)
	}
	std := math.Sqrt(sq / float64(len(rets)-1))
	if std == 0 {
		return 0, len(rets)
	}
	return mean / std * math.Sqrt(annualizationDays), len(rets)
}

// sliceCandles 取 [s, e) 內的 K 線（輸入升序）
func sliceCandles(c []*exchange.Candle, s, e int64) []*exchange.Candle {
	i := sort.Search(len(c), func(k int) bool { return c[k].Timestamp >= s })
	j := sort.Search(len(c), func(k int) bool { return c[k].Timestamp >= e })
	return c[i:j]
}

// Job 一次運行
type Job struct {
	Profile Profile
	Spec    SymbolSpec
	Base    BaseSpec
	Segment Segment
	Variant Variant
	Candles []*exchange.Candle // 該段 1m K 線
	// Hourly 全部數據聚合的 1h K 線（含段前歷史，供 regime 檢測器預熱；K 線源按模擬時間截斷）
	Hourly  []*exchange.Candle
	Funding []replay.FundingPoint
	Steps   int
}

// RunJob 執行一次回放
func RunJob(j Job) RunSummary {
	s := RunSummary{Profile: j.Profile.Name, Symbol: j.Spec.Symbol, Base: j.Base.Name, Segment: j.Segment.Name, Variant: j.Variant.Name, Blocked: j.Variant.Blocked}
	if len(j.Candles) == 0 {
		s.Error = "no candles in segment"
		return s
	}
	s.StartPrice = j.Candles[0].Open
	s.EndPrice = j.Candles[len(j.Candles)-1].Close
	cfg, interval, err := BuildConfig(j.Spec, j.Base, j.Variant, j.Profile, s.StartPrice, j.Funding)
	s.Interval = interval
	if err != nil {
		s.Error = err.Error()
		return s
	}
	if j.Variant.Blocked != "" {
		return s
	}
	ticks, err := replay.CandlesToTicks(j.Candles, replay.IntrabarOptions{Path: replay.IntrabarPathAuto, StepsPerLeg: j.Steps, IntervalMs: MinuteMs})
	if err != nil {
		s.Error = fmt.Sprintf("candles to ticks: %v", err)
		return s
	}
	t0 := time.Now()
	hooks, err := installHooks(&cfg, j.Spec.Symbol, j.Hourly, j.Funding)
	if err != nil {
		s.Error = err.Error()
		return s
	}
	res, err := replay.NewEngine(cfg).Run(ticks)
	s.RuntimeSec = time.Since(t0).Seconds()
	s.Ticks = len(ticks)
	if err != nil {
		s.Error = err.Error()
		return s
	}
	m := res.Metrics
	s.NetPnL, s.RealizedPnL, s.UnrealizedPnL = m.NetPnL, m.RealizedPnL, m.UnrealizedPnL
	s.FeesMaker, s.FeesTaker, s.FundingPaid = m.FeesMaker, m.FeesTaker, m.FundingPaid
	s.MaxDrawdownAbs, s.MaxDrawdownPct = m.MaxDrawdownAbs, m.MaxDrawdownPct
	s.Fills, s.MakerRatio, s.ClosedGrids = m.Fills, m.MakerRatio, m.ClosedGrids
	s.NetProfitPerGrid, s.ProfitToFee = m.NetProfitPerGrid, m.GridNetProfitToFeeRatio
	s.TimeInMarketPct, s.MaxAbsNotional, s.AvgAbsNotional = m.Exposure.TimeInMarketPct, m.Exposure.MaxAbsNotional, m.Exposure.TimeWeightedAvgAbsNotional
	s.PostOnlyRejects, s.MarginRejects, s.OrdersPlaced = m.PostOnlyRejects, m.MarginRejects, m.OrdersPlaced
	s.OrdersCanceled = m.OrdersCanceled
	s.OrderCleanerRuns = m.OrderCleanerRuns
	if hooks.RegimeEnabled {
		s.RegimeRefreshErrors = hooks.RefreshErrors
		s.IntervalChanges = hooks.IntervalChanges
		if hooks.Samples > 0 {
			s.MeanIntervalMultiple = hooks.MeanIntervalMultiple()
			s.RegimeSharePct = hooks.RegimeSharePct()
		}
	}
	s.DailySharpeLike, s.TradingDays = DailySharpeLike(res.Backtest.Equity)
	s.MaxNoFillGapHours = MaxNoFillGapHours(res.Backtest.Trades, m.StartTime, m.EndTime)
	return s
}

// MaxNoFillGapHours 最長無成交間隔（小時），含首筆成交前與末筆成交後；用於識別網格停擺
func MaxNoFillGapHours(trades []backtest.Trade, startMs, endMs int64) float64 {
	ts := make([]int64, 0, len(trades)+2)
	ts = append(ts, startMs)
	for _, t := range trades {
		ts = append(ts, t.Timestamp)
	}
	ts = append(ts, endMs)
	sort.Slice(ts, func(i, j int) bool { return ts[i] < ts[j] })
	gap := int64(0)
	for i := 1; i < len(ts); i++ {
		if d := ts[i] - ts[i-1]; d > gap {
			gap = d
		}
	}
	return float64(gap) / float64(hourMs)
}

// RunAll 並行執行（workers 個 goroutine），結果順序與 jobs 一致
func RunAll(jobs []Job, workers int, progress func(done, total int, s RunSummary)) []RunSummary {
	if workers < 1 {
		workers = 1
	}
	out := make([]RunSummary, len(jobs))
	idx := make(chan int)
	var wg sync.WaitGroup
	var mu sync.Mutex
	done := 0
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range idx {
				out[i] = RunJob(jobs[i])
				mu.Lock()
				done++
				if progress != nil {
					progress(done, len(jobs), out[i])
				}
				mu.Unlock()
			}
		}()
	}
	for i := range jobs {
		idx <- i
	}
	close(idx)
	wg.Wait()
	return out
}
