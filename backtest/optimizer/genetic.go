package optimizer

import (
	"context"
	"math"
	"math/rand"
	"sort"
	"time"

	"quantmesh/backtest"
	"quantmesh/exchange"
)

// GeneticOptimizer 遗傳算法优化器
type GeneticOptimizer struct {
	PopulationSize int
	Generations    int
	EliteRatio     float64 // 精英保留比例
	MutationRate   float64
	randSrc        *rand.Rand
}

// NewGeneticOptimizer 創建遗傳算法优化器
func NewGeneticOptimizer() *GeneticOptimizer {
	return &GeneticOptimizer{
		PopulationSize: 20,
		Generations:    20,
		EliteRatio:     0.2,
		MutationRate:   0.15,
		randSrc:        rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

// individual 個体：一组参數编碼（归一化 [0,1] 便於交叉变异）
type individual struct {
	genes []float64 // [priceLowNorm, priceHighNorm, gridCountNorm, orderQtyNorm]
	score float64
	params backtest.GridBacktestParams
	metrics backtest.Metrics
}

// Run 執行遗傳算法优化
func (g *GeneticOptimizer) Run(ctx context.Context, symbol string, candles []*exchange.Candle, space OptimSearchSpace, config OptimConfig, initialCapital float64) (*OptimResult, error) {
	if err := ValidateSearchSpace(space); err != nil {
		return nil, err
	}
	if len(candles) == 0 {
		return nil, errInvalidRange
	}

	popSize := g.PopulationSize
	generations := g.Generations
	if config.MaxIterations > 0 {
		generations = config.MaxIterations
	}
	if popSize < 4 {
		popSize = 4
	}
	lambda := config.Lambda
	if lambda <= 0 {
		lambda = 0.5
	}

	bounds := g.spaceBounds(space)
	// 初始化种群
	pop := make([]individual, popSize)
	for i := 0; i < popSize; i++ {
		p := g.sampleParams(space, initialCapital)
		score, metrics, err := g.evalOne(ctx, symbol, candles, p, lambda, initialCapital)
		if err != nil {
			score = math.Inf(-1)
			metrics = backtest.Metrics{}
		}
		pop[i] = individual{
			genes:   g.paramsToVec(p, bounds),
			score:   score,
			params:  p,
			metrics: metrics,
		}
	}

	var allResults []ParamResult
	for _, ind := range pop {
		allResults = append(allResults, ParamResult{Params: ind.params, Score: ind.score, Metrics: ind.metrics})
	}

	start := time.Now()
	for gen := 0; gen < generations; gen++ {
		select {
		case <-ctx.Done():
			return g.buildResultFromPopulation(pop, allResults, time.Since(start), "genetic")
		default:
		}

		// 按适应度排序（降序）
		sort.Slice(pop, func(i, j int) bool { return pop[i].score > pop[j].score })

		// 精英保留
		eliteCount := int(float64(popSize) * g.EliteRatio)
		if eliteCount < 1 {
			eliteCount = 1
		}
		newPop := make([]individual, popSize)
		for i := 0; i < eliteCount; i++ {
			newPop[i] = pop[i]
		}

		// 锦標赛选擇 + 交叉 + 变异 填充剩餘
		for i := eliteCount; i < popSize; i++ {
			parent1 := g.tournamentSelect(pop, 3)
			parent2 := g.tournamentSelect(pop, 3)
			childGenes := g.crossover(parent1.genes, parent2.genes)
			g.mutate(childGenes)
			childParams := g.vecToParams(childGenes, bounds, space, initialCapital)
			score, metrics, err := g.evalOne(ctx, symbol, candles, childParams, lambda, initialCapital)
			if err != nil {
				score = math.Inf(-1)
				metrics = backtest.Metrics{}
			}
			newPop[i] = individual{
				genes:   childGenes,
				score:   score,
				params:  childParams,
				metrics: metrics,
			}
			allResults = append(allResults, ParamResult{Params: childParams, Score: score, Metrics: metrics})
		}
		pop = newPop
	}

	return g.buildResultFromPopulation(pop, allResults, time.Since(start), "genetic")
}

func (g *GeneticOptimizer) spaceBounds(space OptimSearchSpace) (bounds [4][2]float64) {
	bounds[0] = [2]float64{space.PriceLowRange.Min, space.PriceLowRange.Max}
	bounds[1] = [2]float64{space.PriceHighRange.Min, space.PriceHighRange.Max}
	bounds[2] = [2]float64{float64(space.GridCountRange.Min), float64(space.GridCountRange.Max)}
	bounds[3] = [2]float64{space.OrderQtyRange.Min, space.OrderQtyRange.Max}
	return bounds
}

func (g *GeneticOptimizer) paramsToVec(p backtest.GridBacktestParams, bounds [4][2]float64) []float64 {
	x := make([]float64, 4)
	x[0] = (p.PriceLow - bounds[0][0]) / (bounds[0][1] - bounds[0][0])
	x[1] = (p.PriceHigh - bounds[1][0]) / (bounds[1][1] - bounds[1][0])
	x[2] = (float64(p.GridCount) - bounds[2][0]) / (bounds[2][1] - bounds[2][0])
	x[3] = (p.OrderQuantity - bounds[3][0]) / (bounds[3][1] - bounds[3][0])
	for i := range x {
		if math.IsNaN(x[i]) || math.IsInf(x[i], 0) {
			x[i] = 0.5
		}
		if x[i] < 0 {
			x[i] = 0
		}
		if x[i] > 1 {
			x[i] = 1
		}
	}
	return x
}

func (g *GeneticOptimizer) vecToParams(genes []float64, bounds [4][2]float64, space OptimSearchSpace, totalCapital float64) backtest.GridBacktestParams {
	low := bounds[0][0] + genes[0]*(bounds[0][1]-bounds[0][0])
	high := bounds[1][0] + genes[1]*(bounds[1][1]-bounds[1][0])
	if high <= low {
		high = low + (bounds[1][1]-bounds[1][0])*0.1
	}
	gc := int(bounds[2][0] + genes[2]*(bounds[2][1]-bounds[2][0]))
	if gc <= 0 {
		gc = 10
	}
	qty := bounds[3][0] + genes[3]*(bounds[3][1]-bounds[3][0])
	if qty <= 0 || qty > totalCapital {
		qty = totalCapital * 0.02
	}
	return ParamsFromSpace(low, high, gc, qty, totalCapital, 0.0004, 0.0003)
}

func (g *GeneticOptimizer) sampleParams(space OptimSearchSpace, totalCapital float64) backtest.GridBacktestParams {
	low := space.PriceLowRange.Min + (space.PriceLowRange.Max-space.PriceLowRange.Min)*g.randSrc.Float64()
	high := space.PriceHighRange.Min + (space.PriceHighRange.Max-space.PriceHighRange.Min)*g.randSrc.Float64()
	if high <= low {
		high = low + space.PriceHighRange.Step
		if high > space.PriceHighRange.Max {
			high = space.PriceHighRange.Max
		}
	}
	gc := space.GridCountRange.Min
	if space.GridCountRange.Max > space.GridCountRange.Min {
		gc = space.GridCountRange.Min + g.randSrc.Intn(space.GridCountRange.Max-space.GridCountRange.Min+1)
	}
	if gc <= 0 {
		gc = 10
	}
	qty := space.OrderQtyRange.Min + (space.OrderQtyRange.Max-space.OrderQtyRange.Min)*g.randSrc.Float64()
	if qty <= 0 || qty > totalCapital {
		qty = totalCapital * 0.02
	}
	return ParamsFromSpace(low, high, gc, qty, totalCapital, 0.0004, 0.0003)
}

func (g *GeneticOptimizer) evalOne(ctx context.Context, symbol string, candles []*exchange.Candle, p backtest.GridBacktestParams, lambda, initialCapital float64) (float64, backtest.Metrics, error) {
	res, err := BacktestRunner(symbol, candles, p, initialCapital)
	if err != nil {
		return math.Inf(-1), backtest.Metrics{}, err
	}
	return CalculateScore(res.Metrics, lambda), res.Metrics, nil
}

func (g *GeneticOptimizer) tournamentSelect(pop []individual, k int) individual {
	best := pop[g.randSrc.Intn(len(pop))]
	for i := 0; i < k-1; i++ {
		cand := pop[g.randSrc.Intn(len(pop))]
		if cand.score > best.score {
			best = cand
		}
	}
	return best
}

func (g *GeneticOptimizer) crossover(a, b []float64) []float64 {
	child := make([]float64, len(a))
	for i := range a {
		if g.randSrc.Float64() < 0.5 {
			child[i] = a[i]
		} else {
			child[i] = b[i]
		}
	}
	return child
}

func (g *GeneticOptimizer) mutate(genes []float64) {
	for i := range genes {
		if g.randSrc.Float64() < g.MutationRate {
			// 高斯扰动
			genes[i] += (g.randSrc.NormFloat64() * 0.2)
			if genes[i] < 0 {
				genes[i] = 0
			}
			if genes[i] > 1 {
				genes[i] = 1
			}
		}
	}
}

func (g *GeneticOptimizer) buildResultFromPopulation(pop []individual, allResults []ParamResult, elapsed time.Duration, method string) (*OptimResult, error) {
	if len(pop) == 0 {
		return &OptimResult{Method: method, Elapsed: elapsed}, nil
	}
	sort.Slice(pop, func(i, j int) bool { return pop[i].score > pop[j].score })
	best := pop[0]
	heatmap := BuildHeatmapFromResults(allResults, "grid_count", "price_range")
	return &OptimResult{
		BestParams:  best.params,
		BestScore:   best.score,
		BestMetrics: best.metrics,
		AllResults:  allResults,
		HeatmapData: heatmap,
		Elapsed:     elapsed,
		Iterations:  len(allResults),
		Method:      method,
	}, nil
}
