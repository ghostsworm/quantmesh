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

// Run 執行网格搜索，枚举搜索空间並並行回测
func (g *GridSearchOptimizer) Run(ctx context.Context, symbol string, candles []*exchange.Candle, space OptimSearchSpace, config OptimConfig, initialCapital float64) (result *OptimResult, resultErr error) {
	defer func() {
		if err := ctx.Err(); err != nil {
			result, resultErr = nil, err
		}
	}()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := ValidateSearchSpace(space); err != nil {
		return nil, err
	}
	if err := ValidateGridSearchSize(space); err != nil {
		return nil, err
	}
	if err := ValidateOptimConfig(config); err != nil {
		return nil, err
	}
	if len(candles) == 0 {
		return nil, errInvalidRange
	}

	if config.walkForwardEnabled() {
		return g.runWalkForward(ctx, symbol, candles, space, config, initialCapital)
	}

	train, val, holdOut, err := SplitCandlesForValidation(candles, config.ValidationRatio)
	if err != nil {
		return nil, err
	}
	feeRate, slip := DefaultFeeSlippage(config)

	// 生成所有参數组合
	paramSets := g.enumerateParams(space, initialCapital, feeRate, slip)
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
	results := g.runParallel(ctx, symbol, train, val, holdOut, paramSets, config.Lambda, initialCapital, parallelism)
	elapsed := time.Since(start)

	best, ok := PickBestParamResult(results)
	if !ok {
		return nil, errNoValidOptimizationResults
	}

	heatmap := BuildHeatmapFromResults(results, "grid_count", "price_range")
	return &OptimResult{
		BestParams:     best.Params,
		BestScore:      best.Score,
		BestMetrics:    best.Metrics,
		AllResults:     results,
		HeatmapData:    heatmap,
		Elapsed:        elapsed,
		Iterations:     len(results),
		Method:         "grid",
		HoldOutEnabled: holdOut,
		FeeRateUsed:    feeRate,
		SlippageUsed:   slip,
	}, nil
}

// runWalkForward 枚舉搜索空間後執行滾動 walk-forward；結果只含測試窗口指標
func (g *GridSearchOptimizer) runWalkForward(ctx context.Context, symbol string, candles []*exchange.Candle, space OptimSearchSpace, config OptimConfig, initialCapital float64) (*OptimResult, error) {
	feeRate, slip := DefaultFeeSlippage(config)
	paramSets := g.enumerateParams(space, initialCapital, feeRate, slip)
	if len(paramSets) == 0 {
		return nil, errInvalidRange
	}
	lambda := config.Lambda
	start := time.Now()
	wf, err := RunWalkForward(ctx, symbol, candles, paramSets, *config.WalkForward, lambda, initialCapital)
	if err != nil {
		return nil, err
	}
	return &OptimResult{
		BestParams:     wf.LatestParams,
		BestScore:      wf.Score,
		BestMetrics:    wf.Metrics,
		Elapsed:        time.Since(start),
		Iterations:     len(paramSets) * len(wf.Folds),
		Method:         "grid_walk_forward",
		HoldOutEnabled: true,
		FeeRateUsed:    feeRate,
		SlippageUsed:   slip,
		WalkForward:    wf,
	}, nil
}

// enumerateParams 枚举搜索空间内的参數组合
func (g *GridSearchOptimizer) enumerateParams(space OptimSearchSpace, totalCapital float64, feeRate, slippage float64) []backtest.GridBacktestParams {
	if ValidateSearchSpace(space) != nil || ValidateGridSearchSize(space) != nil {
		return nil
	}
	var out []backtest.GridBacktestParams

	// 價格下限步進
	lowSteps := steps(space.PriceLowRange.Min, space.PriceLowRange.Max, space.PriceLowRange.Step)
	// 價格上限步進
	highSteps := steps(space.PriceHighRange.Min, space.PriceHighRange.Max, space.PriceHighRange.Step)
	// 网格數步進
	gridSteps := intSteps(space.GridCountRange.Min, space.GridCountRange.Max, space.GridCountRange.Step)
	// 單笔订單金額步進
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
	count := floatStepCount(min, max, step)
	if count == 0 {
		return nil
	}
	s := make([]float64, 0, count)
	for i := 0; i < count; i++ {
		v := min + float64(i)*step
		if v > max || len(s) > 0 && v <= s[len(s)-1] {
			break
		}
		s = append(s, v)
	}
	return s
}

func intSteps(min, max, step int) []int {
	if step <= 0 {
		step = 1
	}
	count := intStepCount(min, max, step)
	if count == 0 {
		return nil
	}
	s := make([]int, 0, count)
	for i := 0; i < count; i++ {
		s = append(s, min+i*step)
	}
	return s
}

// runParallel 使用 worker pool 並行回测
func (g *GridSearchOptimizer) runParallel(ctx context.Context, symbol string, train, val []*exchange.Candle, holdOut bool, paramSets []backtest.GridBacktestParams, lambda float64, initialCapital float64, workers int) []ParamResult {
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

	// 投遞任務
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
					pr, evalErr := EvalParamSetContext(ctx, symbol, train, val, holdOut, j.param, lambda, initialCapital)
					if evalErr != nil {
						return
					}
					resultCh <- result{index: j.index, pr: pr}
				}
			}
		}()
	}
	go func() {
		wg.Wait()
		close(resultCh)
	}()

	// 收集結果（保持顺序可選，这里按 index 存）
	results := make([]ParamResult, len(paramSets))
	for i := range results {
		results[i] = ParamResult{Score: math.Inf(-1)}
	}
	for r := range resultCh {
		if r.index >= 0 && r.index < len(results) {
			results[r.index] = r.pr
		}
	}
	// 过滤掉失败或非有限分数的候选，避免非法数值进入结果与 JSON 序列化。
	var out []ParamResult
	for _, r := range results {
		if finiteNumber(r.Score) {
			out = append(out, r)
		}
	}
	return out
}
