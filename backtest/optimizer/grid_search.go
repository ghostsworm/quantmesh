package optimizer

import (
	"context"
	"math"
	"runtime"
	"sync"
	"time"

	"quantmesh/backtest"
	"quantmesh/exchange"
)

// GridSearchOptimizer 网格搜索优化器
type GridSearchOptimizer struct{}

// Run 执行网格搜索，枚举搜索空间并并行回测
func (g *GridSearchOptimizer) Run(ctx context.Context, symbol string, candles []*exchange.Candle, space OptimSearchSpace, config OptimConfig, initialCapital float64) (*OptimResult, error) {
	if err := ValidateSearchSpace(space); err != nil {
		return nil, err
	}
	if len(candles) == 0 {
		return nil, errInvalidRange
	}

	// 生成所有参数组合
	paramSets := g.enumerateParams(space, initialCapital)
	if len(paramSets) == 0 {
		return nil, errInvalidRange
	}

	parallelism := config.Parallelism
	if parallelism <= 0 {
		parallelism = runtime.NumCPU()
	}
	if parallelism > len(paramSets) {
		parallelism = len(paramSets)
	}

	start := time.Now()
	results := g.runParallel(ctx, symbol, candles, paramSets, config.Lambda, initialCapital, parallelism)
	elapsed := time.Since(start)

	// 找最优
	var best ParamResult
	bestScore := math.Inf(-1)
	for _, r := range results {
		if r.Score > bestScore {
			bestScore = r.Score
			best = r
		}
	}

	heatmap := BuildHeatmapFromResults(results, "grid_count", "price_range")
	return &OptimResult{
		BestParams:  best.Params,
		BestScore:   best.Score,
		BestMetrics: best.Metrics,
		AllResults:  results,
		HeatmapData: heatmap,
		Elapsed:     elapsed,
		Iterations:  len(results),
		Method:      "grid",
	}, nil
}

// enumerateParams 枚举搜索空间内的参数组合
func (g *GridSearchOptimizer) enumerateParams(space OptimSearchSpace, totalCapital float64) []backtest.GridBacktestParams {
	var out []backtest.GridBacktestParams
	feeRate := 0.0004
	slippage := 0.0003

	// 价格下限步进
	lowSteps := steps(space.PriceLowRange.Min, space.PriceLowRange.Max, space.PriceLowRange.Step)
	// 价格上限步进
	highSteps := steps(space.PriceHighRange.Min, space.PriceHighRange.Max, space.PriceHighRange.Step)
	// 网格数步进
	gridSteps := intSteps(space.GridCountRange.Min, space.GridCountRange.Max, space.GridCountRange.Step)
	// 单笔订单金额步进
	qtySteps := steps(space.OrderQtyRange.Min, space.OrderQtyRange.Max, space.OrderQtyRange.Step)

	for _, low := range lowSteps {
		for _, high := range highSteps {
			if high <= low {
				continue
			}
			for _, gc := range gridSteps {
				if gc <= 0 {
					continue
				}
				for _, qty := range qtySteps {
					if qty <= 0 || qty > totalCapital {
						continue
					}
					p := ParamsFromSpace(low, high, gc, qty, totalCapital, feeRate, slippage)
					out = append(out, p)
				}
			}
		}
	}
	return out
}

func steps(min, max, step float64) []float64 {
	if step <= 0 {
		return nil
	}
	var s []float64
	for v := min; v <= max; v += step {
		s = append(s, v)
	}
	return s
}

func intSteps(min, max, step int) []int {
	if step <= 0 {
		step = 1
	}
	var s []int
	for v := min; v <= max; v += step {
		s = append(s, v)
	}
	return s
}

// runParallel 使用 worker pool 并行回测
func (g *GridSearchOptimizer) runParallel(ctx context.Context, symbol string, candles []*exchange.Candle, paramSets []backtest.GridBacktestParams, lambda float64, initialCapital float64, workers int) []ParamResult {
	type job struct {
		index int
		param backtest.GridBacktestParams
	}
	type result struct {
		index int
		pr    ParamResult
	}

	jobCh := make(chan job, len(paramSets))
	resultCh := make(chan result, len(paramSets))

	// 投递任务
	go func() {
		for i, p := range paramSets {
			select {
			case <-ctx.Done():
				close(jobCh)
				return
			default:
				jobCh <- job{index: i, param: p}
			}
		}
		close(jobCh)
	}()

	// worker
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
					res, err := BacktestRunner(symbol, candles, j.param, initialCapital)
					if err != nil {
						resultCh <- result{index: j.index, pr: ParamResult{Params: j.param, Score: math.Inf(-1), Metrics: backtest.Metrics{}}}
						continue
					}
					score := CalculateScore(res.Metrics, lambda)
					resultCh <- result{
						index: j.index,
						pr: ParamResult{
							Params:  j.param,
							Score:   score,
							Metrics: res.Metrics,
						},
					}
				}
			}
		}()
	}
	go func() {
		wg.Wait()
		close(resultCh)
	}()

	// 收集结果（保持顺序可选，这里按 index 存）
	results := make([]ParamResult, len(paramSets))
	for i := range results {
		results[i] = ParamResult{Score: math.Inf(-1)}
	}
	for r := range resultCh {
		if r.index >= 0 && r.index < len(results) {
			results[r.index] = r.pr
		}
	}
	// 过滤掉未完成的（Score 为 -Inf）
	var out []ParamResult
	for _, r := range results {
		if !math.IsInf(r.Score, -1) {
			out = append(out, r)
		}
	}
	return out
}
