package optimizer

import (
	"context"
	"math"
	"math/rand"
	"time"

	"quantmesh/backtest"
	"quantmesh/exchange"
)

// BayesianOptimizer 贝叶斯优化器（高斯過程 + EI 采集）
type BayesianOptimizer struct {
	lengthScale float64 // RBF 核长度尺度
	noiseVar    float64 // 观测噪声方差
	randSrc     *rand.Rand
}

// NewBayesianOptimizer 創建贝叶斯优化器
func NewBayesianOptimizer() *BayesianOptimizer {
	return &BayesianOptimizer{
		lengthScale: 0.5,
		noiseVar:    1e-4,
		randSrc:     rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

// Run 執行贝叶斯优化
func (b *BayesianOptimizer) Run(ctx context.Context, symbol string, candles []*exchange.Candle, space OptimSearchSpace, config OptimConfig, initialCapital float64) (*OptimResult, error) {
	if err := ValidateSearchSpace(space); err != nil {
		return nil, err
	}
	if err := ValidateOptimConfig(config); err != nil {
		return nil, err
	}
	if len(candles) == 0 {
		return nil, errInvalidRange
	}

	train, val, holdOut, err := SplitCandlesForValidation(candles, config.ValidationRatio)
	if err != nil {
		return nil, err
	}
	feeRate, slip := DefaultFeeSlippage(config)

	nCalls := config.MaxIterations
	if nCalls <= 0 {
		nCalls = 40
	}
	lambda := config.Lambda
	if lambda <= 0 {
		lambda = 0.5
	}

	// 將空间归一化到 [0,1]^4 用於核计算
	bounds := b.spaceBounds(space)
	// 初始随机点
	nInit := 5
	if nInit > nCalls {
		nInit = nCalls
	}
	var X [][]float64
	var y []float64

	var allParamResults []ParamResult
	for i := 0; i < nInit; i++ {
		select {
		case <-ctx.Done():
			return b.buildResultFromParamResults(allParamResults, "bayesian", holdOut, feeRate, slip)
		default:
		}
		p := b.sampleParams(space, initialCapital, feeRate, slip)
		pr := EvalParamSet(symbol, train, val, holdOut, p, lambda, initialCapital)
		if math.IsInf(pr.TrainScore, -1) {
			continue
		}
		x := b.paramsToVec(p, bounds)
		X = append(X, x)
		y = append(y, pr.TrainScore)
		allParamResults = append(allParamResults, pr)
	}

	// 迭代：用 GP 預测 EI，取最大 EI 点评估
	for iter := nInit; iter < nCalls; iter++ {
		select {
		case <-ctx.Done():
			return b.buildResultFromParamResults(allParamResults, "bayesian", holdOut, feeRate, slip)
		default:
		}

		bestObs := math.Inf(-1)
		for _, v := range y {
			if v > bestObs {
				bestObs = v
			}
		}

		// 随机采样若干候选，选 EI 最大的
		nCandidates := 50
		bestEI := math.Inf(-1)
		var nextP backtest.GridBacktestParams
		var nextX []float64
		for c := 0; c < nCandidates; c++ {
			p := b.sampleParams(space, initialCapital, feeRate, slip)
			x := b.paramsToVec(p, bounds)
			mu, sigma := b.gpPredict(X, y, x)
			ei := b.expectedImprovement(mu, sigma, bestObs)
			if ei > bestEI {
				bestEI = ei
				nextP = p
				nextX = x
			}
		}

		pr := EvalParamSet(symbol, train, val, holdOut, nextP, lambda, initialCapital)
		if math.IsInf(pr.TrainScore, -1) {
			continue
		}
		X = append(X, nextX)
		y = append(y, pr.TrainScore)
		allParamResults = append(allParamResults, pr)
	}

	return b.buildResultFromParamResults(allParamResults, "bayesian", holdOut, feeRate, slip)
}

func (b *BayesianOptimizer) spaceBounds(space OptimSearchSpace) (bounds [4][2]float64) {
	bounds[0] = [2]float64{space.PriceLowRange.Min, space.PriceLowRange.Max}
	bounds[1] = [2]float64{space.PriceHighRange.Min, space.PriceHighRange.Max}
	bounds[2] = [2]float64{float64(space.GridCountRange.Min), float64(space.GridCountRange.Max)}
	bounds[3] = [2]float64{space.OrderQtyRange.Min, space.OrderQtyRange.Max}
	return bounds
}

func (b *BayesianOptimizer) paramsToVec(p backtest.GridBacktestParams, bounds [4][2]float64) []float64 {
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

func (b *BayesianOptimizer) sampleParams(space OptimSearchSpace, totalCapital float64, feeRate, slippage float64) backtest.GridBacktestParams {
	low := space.PriceLowRange.Min + (space.PriceLowRange.Max-space.PriceLowRange.Min)*b.randSrc.Float64()
	high := space.PriceHighRange.Min + (space.PriceHighRange.Max-space.PriceHighRange.Min)*b.randSrc.Float64()
	if high <= low {
		high = low + space.PriceHighRange.Step
		if high > space.PriceHighRange.Max {
			high = space.PriceHighRange.Max
		}
	}
	gc := space.GridCountRange.Min
	if space.GridCountRange.Max > space.GridCountRange.Min {
		gc = space.GridCountRange.Min + b.randSrc.Intn(space.GridCountRange.Max-space.GridCountRange.Min+1)
	}
	if gc <= 0 {
		gc = 10
	}
	qty := space.OrderQtyRange.Min + (space.OrderQtyRange.Max-space.OrderQtyRange.Min)*b.randSrc.Float64()
	if qty <= 0 || qty > totalCapital {
		qty = totalCapital * 0.02
	}
	return ParamsFromSpace(low, high, gc, qty, totalCapital, feeRate, slippage)
}

// rbfKernel RBF 核 K(a, b) = exp(-||a-b||^2 / (2*l^2))
func (b *BayesianOptimizer) rbfKernel(a, bVec []float64) float64 {
	var d2 float64
	for i := range a {
		diff := a[i] - bVec[i]
		d2 += diff * diff
	}
	return math.Exp(-d2 / (2 * b.lengthScale * b.lengthScale))
}

// gpPredict 高斯過程預测：返回均值 mu 和標准差 sigma
func (b *BayesianOptimizer) gpPredict(X [][]float64, y []float64, xStar []float64) (mu, sigma float64) {
	n := len(X)
	if n == 0 {
		return 0, 1
	}
	// K = n×n 核矩阵
	K := make([][]float64, n)
	for i := range K {
		K[i] = make([]float64, n)
		for j := range K[i] {
			K[i][j] = b.rbfKernel(X[i], X[j])
		}
		K[i][i] += b.noiseVar
	}
	// k* = n×1
	kStar := make([]float64, n)
	for i := 0; i < n; i++ {
		kStar[i] = b.rbfKernel(X[i], xStar)
	}
	kStarStar := b.rbfKernel(xStar, xStar) + b.noiseVar

	// 解 (K) alpha = y
	alpha, err := solveSym(K, y)
	if err != nil {
		return 0, 1
	}
	mu = 0
	for i := 0; i < n; i++ {
		mu += kStar[i] * alpha[i]
	}
	// 解 (K) v = k*
	v, err := solveSym(K, kStar)
	if err != nil {
		return mu, 0.5
	}
	var predVar float64
	for i := 0; i < n; i++ {
		predVar += kStar[i] * v[i]
	}
	predVar = kStarStar - predVar
	if predVar < 1e-10 {
		predVar = 1e-10
	}
	sigma = math.Sqrt(predVar)
	return mu, sigma
}

// solveSym 解對称正定線性方程组 Kx = rhs（Cholesky 分解）
func solveSym(K [][]float64, rhs []float64) ([]float64, error) {
	n := len(rhs)
	L := make([][]float64, n)
	for i := range L {
		L[i] = make([]float64, n)
	}
	for i := 0; i < n; i++ {
		for j := 0; j <= i; j++ {
			sum := K[i][j]
			for k := 0; k < j; k++ {
				sum -= L[i][k] * L[j][k]
			}
			if i == j {
				if sum <= 0 {
					return nil, errInvalidRange
				}
				L[i][j] = math.Sqrt(sum)
			} else {
				L[i][j] = sum / L[j][j]
			}
		}
	}
	// 前代 L y = rhs
	y := make([]float64, n)
	for i := 0; i < n; i++ {
		sum := rhs[i]
		for j := 0; j < i; j++ {
			sum -= L[i][j] * y[j]
		}
		y[i] = sum / L[i][i]
	}
	// 回代 L^T x = y
	x := make([]float64, n)
	for i := n - 1; i >= 0; i-- {
		sum := y[i]
		for j := i + 1; j < n; j++ {
			sum -= L[j][i] * x[j]
		}
		x[i] = sum / L[i][i]
	}
	return x, nil
}

// expectedImprovement EI = E[max(0, f(x) - f_best)]
func (b *BayesianOptimizer) expectedImprovement(mu, sigma, fBest float64) float64 {
	if sigma < 1e-12 {
		diff := mu - fBest
		if diff > 0 {
			return diff
		}
		return 0
	}
	z := (mu - fBest) / sigma
	phiZ := math.Exp(-z*z/2) / math.Sqrt(2*math.Pi)
	PhiZ := (1 + math.Erf(z/math.Sqrt(2))) / 2
	ei := (mu-fBest)*PhiZ + sigma*phiZ
	if ei < 0 {
		return 0
	}
	return ei
}

func (b *BayesianOptimizer) buildResultFromParamResults(allResults []ParamResult, method string, holdOut bool, feeRate, slip float64) (*OptimResult, error) {
	if len(allResults) == 0 {
		return &OptimResult{Method: method, HoldOutEnabled: holdOut, FeeRateUsed: feeRate, SlippageUsed: slip}, nil
	}
	best, ok := PickBestParamResult(allResults)
	if !ok {
		best = ParamResult{}
	}
	heatmap := BuildHeatmapFromResults(allResults, "grid_count", "price_range")
	return &OptimResult{
		BestParams:       best.Params,
		BestScore:        best.Score,
		BestMetrics:      best.Metrics,
		AllResults:       allResults,
		HeatmapData:      heatmap,
		Iterations:       len(allResults),
		Method:           method,
		HoldOutEnabled:   holdOut,
		FeeRateUsed:      feeRate,
		SlippageUsed:     slip,
	}, nil
}
