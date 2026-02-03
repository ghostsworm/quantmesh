package optimizer

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"runtime"
	"sort"
	"sync"
	"time"

	"quantmesh/backtest"
	"quantmesh/exchange"
)

// ParamRange 参数取值范围
type ParamRange struct {
	Min  float64 `json:"min"`
	Max  float64 `json:"max"`
	Step float64 `json:"step"`
}

// UniversalSearchSpace 通用参数搜索空间
type UniversalSearchSpace struct {
	Strategy string                `json:"strategy"`
	Ranges   map[string]ParamRange `json:"ranges"`
}

// UniversalParamResult 单组参数结果
type UniversalParamResult struct {
	Params      map[string]interface{} `json:"params"`
	Metrics     backtest.Metrics       `json:"metrics"`
	TotalReturn float64                `json:"total_return"`
	MaxDrawdown float64                `json:"max_drawdown"`
	SharpeRatio float64                `json:"sharpe_ratio"`
	WinRate     float64                `json:"win_rate"`
	TotalTrades int                    `json:"total_trades"`
}

// UniversalOptimResult 通用优化结果
type UniversalOptimResult struct {
	TaskID       string                 `json:"task_id"`
	Strategy     string                 `json:"strategy"`
	AllResults   []UniversalParamResult `json:"all_results"`
	BestByReturn *UniversalParamResult  `json:"best_by_return,omitempty"`
	BestBySharpe *UniversalParamResult  `json:"best_by_sharpe,omitempty"`
	Elapsed      time.Duration          `json:"elapsed"`
	TotalCombos  int                    `json:"total_combos"`
	Completed    int                    `json:"completed"`
}

// UniversalOptimizer 通用多策略优化器
type UniversalOptimizer struct{}

// EnumerateParamCombos 枚举搜索空间内的所有参数组合
func (u *UniversalOptimizer) EnumerateParamCombos(space UniversalSearchSpace) []map[string]interface{} {
	if space.Ranges == nil || len(space.Ranges) == 0 {
		return []map[string]interface{}{{}} // 无范围时返回默认一组
	}

	// 收集每个参数的可选值
	paramKeys := make([]string, 0, len(space.Ranges))
	for k := range space.Ranges {
		paramKeys = append(paramKeys, k)
	}
	sort.Strings(paramKeys)

	var valueLists [][]float64
	for _, k := range paramKeys {
		r := space.Ranges[k]
		vals := stepsRange(r.Min, r.Max, r.Step)
		valueLists = append(valueLists, vals)
	}

	// 笛卡尔积
	var combos []map[string]interface{}
	u.cartesian(paramKeys, valueLists, 0, map[string]interface{}{}, &combos)
	return combos
}

func (u *UniversalOptimizer) cartesian(keys []string, valueLists [][]float64, idx int, current map[string]interface{}, out *[]map[string]interface{}) {
	if idx >= len(keys) {
		// 复制 current 到结果
		cpy := make(map[string]interface{}, len(current))
		for k, v := range current {
			cpy[k] = v
		}
		*out = append(*out, cpy)
		return
	}
	key := keys[idx]
	vals := valueLists[idx]
	for _, v := range vals {
		current[key] = v
		u.cartesian(keys, valueLists, idx+1, current, out)
	}
}

func stepsRange(min, max, step float64) []float64 {
	if step <= 0 {
		return []float64{min}
	}
	var s []float64
	for v := min; v <= max+1e-9; v += step {
		s = append(s, math.Round(v*1e9)/1e9) // 避免浮点误差
	}
	if len(s) == 0 {
		s = []float64{min}
	}
	return s
}

// RunOne 执行单次回测（供外部调用）
func (u *UniversalOptimizer) RunOne(ctx context.Context, symbol, interval string, candles []*exchange.Candle, strategy string, params map[string]interface{}, totalCapital float64) (*backtest.BacktestResult, error) {
	switch strategy {
	case "grid":
		return u.runGrid(ctx, symbol, candles, params, totalCapital)
	case "dca":
		return u.runDCA(symbol, interval, candles, params, totalCapital)
	case "martingale":
		return u.runMartingale(symbol, interval, candles, params, totalCapital)
	case "momentum", "mean_reversion", "trend_following":
		return u.runIndicatorStrategy(symbol, candles, strategy, params, totalCapital)
	default:
		return nil, fmt.Errorf("不支援的策略: %s", strategy)
	}
}

func (u *UniversalOptimizer) runGrid(ctx context.Context, symbol string, candles []*exchange.Candle, params map[string]interface{}, totalCapital float64) (*backtest.BacktestResult, error) {
	gridParams := backtest.GridBacktestParams{
		PriceLow:      0,
		PriceHigh:     0,
		GridCount:     getIntParam(params, "grid_count", 20),
		GridSpacing:   getFloatParam(params, "grid_spacing", 0),
		OrderQuantity: getFloatParam(params, "order_quantity", 100),
		TotalCapital:  totalCapital,
		FeeRate:       getFloatParam(params, "fee_rate", 0.0004),
		SlippageRatio: 0.0003,
	}
	riskVM := getFloatParam(params, "risk_volume_multiplier", 3.0)
	riskAW := getIntParam(params, "risk_average_window", 20)
	var riskSim *backtest.RiskSimulator
	if riskVM > 0 && riskAW > 0 {
		riskSim = backtest.NewRiskSimulator(&backtest.RiskSimulatorConfig{
			VolumeMultiplier: riskVM,
			AverageWindow:    riskAW,
		})
	}
	return backtest.RunGridBacktest(symbol, candles, gridParams, totalCapital, riskSim)
}

func (u *UniversalOptimizer) runDCA(symbol, interval string, candles []*exchange.Candle, params map[string]interface{}, totalCapital float64) (*backtest.BacktestResult, error) {
	p := backtest.DCABacktestParams{
		IntervalDays:   getIntParam(params, "interval_days", 7),
		AmountPerTrade: getFloatParam(params, "amount_per_trade", 100),
		TotalCapital:   totalCapital,
		FeeRate:        getFloatParam(params, "fee_rate", 0.0004),
	}
	return backtest.RunDCABacktest(symbol, interval, candles, p, totalCapital)
}

func (u *UniversalOptimizer) runMartingale(symbol, interval string, candles []*exchange.Candle, params map[string]interface{}, totalCapital float64) (*backtest.BacktestResult, error) {
	p := backtest.MartingaleBacktestParams{
		BaseAmount:    getFloatParam(params, "base_amount", 100),
		Multiplier:    getFloatParam(params, "multiplier", 2),
		TotalCapital:  totalCapital,
		FeeRate:       getFloatParam(params, "fee_rate", 0.0004),
		TakeProfitPct: 1,
		StopLossPct:   2,
	}
	return backtest.RunMartingaleBacktest(symbol, interval, candles, p, totalCapital)
}

func (u *UniversalOptimizer) runIndicatorStrategy(symbol string, candles []*exchange.Candle, strategy string, params map[string]interface{}, totalCapital float64) (*backtest.BacktestResult, error) {
	var adapter backtest.StrategyAdapter
	switch strategy {
	case "momentum":
		adapter = backtest.NewMomentumAdapterWithParams(params)
	case "mean_reversion":
		adapter = backtest.NewMeanReversionAdapterWithParams(params)
	case "trend_following":
		adapter = backtest.NewTrendFollowingAdapterWithParams(params)
	default:
		return nil, fmt.Errorf("不支援的策略: %s", strategy)
	}
	bt := backtest.NewBacktester(symbol, candles, adapter, totalCapital)
	return bt.Run()
}

func getFloatParam(m map[string]interface{}, key string, def float64) float64 {
	if m == nil {
		return def
	}
	v, ok := m[key]
	if !ok {
		return def
	}
	switch t := v.(type) {
	case float64:
		return t
	case int:
		return float64(t)
	case int64:
		return float64(t)
	case json.Number:
		f, _ := t.Float64()
		return f
	}
	return def
}

func getIntParam(m map[string]interface{}, key string, def int) int {
	if m == nil {
		return def
	}
	v, ok := m[key]
	if !ok {
		return def
	}
	switch t := v.(type) {
	case float64:
		return int(t)
	case int:
		return t
	case int64:
		return int(t)
	case json.Number:
		f, _ := t.Float64()
		return int(f)
	}
	return def
}

// Run 执行完整优化（阻塞），支持进度回调
func (u *UniversalOptimizer) Run(ctx context.Context, taskID, symbol, interval string, candles []*exchange.Candle, space UniversalSearchSpace, totalCapital float64, onProgress func(completed, total int)) (*UniversalOptimResult, error) {
	combos := u.EnumerateParamCombos(space)
	if len(combos) == 0 {
		return nil, fmt.Errorf("搜索空间为空")
	}

	workers := runtime.NumCPU()
	if workers > len(combos) {
		workers = len(combos)
	}

	start := time.Now()
	results := make([]UniversalParamResult, len(combos))
	var mu sync.Mutex
	completed := 0

	type job struct {
		idx   int
		param map[string]interface{}
	}
	jobCh := make(chan job, len(combos))
	for i, p := range combos {
		jobCh <- job{idx: i, param: p}
	}
	close(jobCh)

	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobCh {
				select {
				case <-ctx.Done():
					return
				default:
				}
				res, err := u.RunOne(ctx, symbol, interval, candles, space.Strategy, j.param, totalCapital)
				pr := UniversalParamResult{Params: j.param}
				if err != nil || res == nil {
					pr.TotalReturn = math.Inf(-1)
					pr.MaxDrawdown = 100
					pr.SharpeRatio = math.Inf(-1)
				} else {
					pr.Metrics = res.Metrics
					pr.TotalReturn = res.Metrics.TotalReturn
					pr.MaxDrawdown = res.Metrics.MaxDrawdown
					pr.SharpeRatio = res.Metrics.SharpeRatio
					pr.WinRate = res.Metrics.WinRate
					pr.TotalTrades = res.Metrics.TotalTrades
				}
				mu.Lock()
				results[j.idx] = pr
				completed++
				curCompleted := completed
				mu.Unlock()
				if onProgress != nil {
					onProgress(curCompleted, len(combos))
				}
			}
		}()
	}
	wg.Wait()
	elapsed := time.Since(start)

	// 过滤有效结果
	var valid []UniversalParamResult
	for _, r := range results {
		if !math.IsInf(r.TotalReturn, -1) {
			valid = append(valid, r)
		}
	}

	// 找最佳
	var bestByReturn, bestBySharpe *UniversalParamResult
	if len(valid) > 0 {
		byReturn := make([]UniversalParamResult, len(valid))
		copy(byReturn, valid)
		sort.Slice(byReturn, func(i, j int) bool { return byReturn[i].TotalReturn > byReturn[j].TotalReturn })
		bestByReturn = &byReturn[0]

		bySharpe := make([]UniversalParamResult, len(valid))
		copy(bySharpe, valid)
		sort.Slice(bySharpe, func(i, j int) bool { return bySharpe[i].SharpeRatio > bySharpe[j].SharpeRatio })
		bestBySharpe = &bySharpe[0]
	}

	return &UniversalOptimResult{
		TaskID:       taskID,
		Strategy:     space.Strategy,
		AllResults:   valid,
		BestByReturn: bestByReturn,
		BestBySharpe: bestBySharpe,
		Elapsed:      elapsed,
		TotalCombos:  len(combos),
		Completed:    len(valid),
	}, nil
}

// GetDefaultSearchSpace 获取策略的默认搜索空间
func GetDefaultSearchSpace(strategy string) UniversalSearchSpace {
	switch strategy {
	case "grid":
		return UniversalSearchSpace{
			Strategy: "grid",
			Ranges: map[string]ParamRange{
				"grid_spacing":           {Min: 100, Max: 500, Step: 50},
				"order_quantity":         {Min: 100, Max: 500, Step: 50},
				"risk_volume_multiplier": {Min: 2, Max: 5, Step: 0.5},
				"risk_average_window":    {Min: 10, Max: 50, Step: 5},
			},
		}
	case "momentum":
		return UniversalSearchSpace{
			Strategy: "momentum",
			Ranges: map[string]ParamRange{
				"rsi_period": {Min: 7, Max: 21, Step: 1},
			},
		}
	case "mean_reversion":
		return UniversalSearchSpace{
			Strategy: "mean_reversion",
			Ranges: map[string]ParamRange{
				"period": {Min: 10, Max: 50, Step: 5},
			},
		}
	case "trend_following":
		return UniversalSearchSpace{
			Strategy: "trend_following",
			Ranges: map[string]ParamRange{
				"fast_period": {Min: 5, Max: 20, Step: 1},
				"slow_period": {Min: 20, Max: 50, Step: 5},
			},
		}
	case "dca":
		return UniversalSearchSpace{
			Strategy: "dca",
			Ranges: map[string]ParamRange{
				"interval_days":    {Min: 1, Max: 14, Step: 1},
				"amount_per_trade": {Min: 50, Max: 500, Step: 50},
			},
		}
	case "martingale":
		return UniversalSearchSpace{
			Strategy: "martingale",
			Ranges: map[string]ParamRange{
				"base_amount": {Min: 50, Max: 500, Step: 25},
				"multiplier":  {Min: 1.5, Max: 3, Step: 0.5},
			},
		}
	default:
		return UniversalSearchSpace{Strategy: strategy, Ranges: map[string]ParamRange{}}
	}
}
