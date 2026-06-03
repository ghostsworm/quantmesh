package optimizer

import (
	"context"
	"encoding/json"
	"math"
	"math/rand"
	"testing"
	"time"

	"quantmesh/backtest"
	"quantmesh/exchange"
)

func tinySearchSpace() OptimSearchSpace {
	return OptimSearchSpace{
		PriceLowRange:  Range{Min: 90, Max: 95, Step: 5},
		PriceHighRange: Range{Min: 105, Max: 110, Step: 5},
		GridCountRange: IntRange{Min: 2, Max: 3, Step: 1},
		OrderQtyRange:  Range{Min: 10, Max: 20, Step: 10},
	}
}

func optimizerCandles(n int) []*exchange.Candle {
	candles := make([]*exchange.Candle, 0, n)
	for i := 0; i < n; i++ {
		closePrice := 100 + math.Sin(float64(i)/3)*4 + float64(i%7)
		candles = append(candles, &exchange.Candle{
			Symbol:    "BTCUSDT",
			Open:      closePrice - 1,
			High:      closePrice + 2,
			Low:       closePrice - 2,
			Close:     closePrice,
			Volume:    100 + float64(i),
			Timestamp: int64(i * 60_000),
			IsClosed:  true,
		})
	}
	return candles
}

func TestOptimizerConfigSpaceAndScoringHelpers(t *testing.T) {
	cfg := DefaultOptimConfig()
	if cfg.Method != "grid" || cfg.Lambda != 0.5 || cfg.MaxIterations != 100 {
		t.Fatalf("unexpected default config: %#v", cfg)
	}
	if err := ValidateOptimConfig(OptimConfig{ValidationRatio: -0.1}); err != errInvalidValidationRatio {
		t.Fatalf("negative validation ratio error = %v", err)
	}
	if err := ValidateOptimConfig(OptimConfig{ValidationRatio: 0.5}); err != errInvalidValidationRatio {
		t.Fatalf("large validation ratio error = %v", err)
	}
	if err := ValidateOptimConfig(OptimConfig{ValidationRatio: 0.2}); err != nil {
		t.Fatalf("valid validation ratio failed: %v", err)
	}

	invalidSpaces := []OptimSearchSpace{
		{PriceLowRange: Range{Min: 2, Max: 1, Step: 1}},
		{PriceLowRange: Range{Min: 1, Max: 2, Step: 0}, PriceHighRange: Range{Min: 3, Max: 4, Step: 1}},
		{PriceLowRange: Range{Min: 1, Max: 2, Step: 1}, PriceHighRange: Range{Min: 3, Max: 4, Step: 1}, GridCountRange: IntRange{Min: 0, Max: 1}},
		{PriceLowRange: Range{Min: 1, Max: 2, Step: 1}, PriceHighRange: Range{Min: 3, Max: 4, Step: 1}, GridCountRange: IntRange{Min: 1, Max: 2}, OrderQtyRange: Range{Min: 0, Max: 1}},
	}
	for _, space := range invalidSpaces {
		if err := ValidateSearchSpace(space); err != errInvalidRange {
			t.Fatalf("invalid search space error = %v", err)
		}
	}
	if err := ValidateSearchSpace(tinySearchSpace()); err != nil {
		t.Fatalf("valid search space failed: %v", err)
	}

	fee, slip := DefaultFeeSlippage(OptimConfig{})
	if fee != 0.0004 || slip != 0.0003 {
		t.Fatalf("default fee/slippage = %.6f %.6f", fee, slip)
	}
	fee, slip = DefaultFeeSlippage(OptimConfig{FeeRate: 0.001, SlippageRatio: 0.002})
	if fee != 0.001 || slip != 0.002 {
		t.Fatalf("custom fee/slippage = %.6f %.6f", fee, slip)
	}

	params := ParamsFromSpace(90, 110, 4, 25, 1000, 0, 0)
	if params.FeeRate != 0.0004 || params.SlippageRatio != 0.0003 || params.TotalCapital != 1000 {
		t.Fatalf("params from space did not apply defaults: %#v", params)
	}

	score := CalculateScore(backtest.Metrics{AnnualizedReturn: 20, MaxDrawdown: 10, SharpeRatio: math.Inf(1)}, 0.5)
	if score != 15 {
		t.Fatalf("score with infinite sharpe = %v, want 15", score)
	}
}

func TestGridSearchEnumerationHeatmapAndParallel(t *testing.T) {
	g := &GridSearchOptimizer{}
	params := g.enumerateParams(tinySearchSpace(), 1000, 0.001, 0.002)
	if len(params) != 16 {
		t.Fatalf("enumerated params = %d, want 16", len(params))
	}
	for _, p := range params {
		if p.PriceHigh <= p.PriceLow || p.OrderQuantity <= 0 || p.FeeRate != 0.001 || p.SlippageRatio != 0.002 {
			t.Fatalf("invalid enumerated params: %#v", p)
		}
	}

	if got := steps(1, 2, 0); got != nil {
		t.Fatalf("non-positive float step = %#v, want nil", got)
	}
	if got := intSteps(1, 3, 0); len(got) != 3 {
		t.Fatalf("default int steps = %#v", got)
	}

	results := []ParamResult{
		{Params: params[0], Score: 1.5},
		{Params: params[1], Score: 3.5},
		{Params: params[2], Score: math.Inf(-1)},
	}
	best, ok := PickBestParamResult(results)
	if !ok || best.Score != 3.5 {
		t.Fatalf("best result = %#v ok=%v", best, ok)
	}
	if _, ok := PickBestParamResult([]ParamResult{{Score: math.Inf(-1)}}); ok {
		t.Fatalf("all invalid scores should not yield best result")
	}

	heatmap := BuildHeatmapFromResults(results, "order_quantity", "price_low")
	if heatmap == nil || len(heatmap.XAxis) == 0 || len(heatmap.YAxis) == 0 || len(heatmap.Data) == 0 {
		t.Fatalf("heatmap not built: %#v", heatmap)
	}
	if BuildHeatmapFromResults(nil, "grid_count", "price_range") != nil {
		t.Fatalf("empty heatmap should be nil")
	}

	sorted := sortedKeysNum(map[interface{}]bool{"b": true, "a": true, int64(2): true, 1.5: true})
	if len(sorted) != 4 {
		t.Fatalf("sorted keys len = %d", len(sorted))
	}
	if f, ok := toFloat(json.Number("12.5")); ok || f != 0 {
		t.Fatalf("json.Number should fall back to non numeric branch: %v %v", f, ok)
	}

	runResults := g.runParallel(context.Background(), "BTCUSDT", optimizerCandles(20), nil, false, params[:2], 0.5, 1000, 2)
	if len(runResults) == 0 {
		t.Fatalf("parallel run returned no results")
	}
}

func TestValidationSplitAndUniversalOptimizerHelpers(t *testing.T) {
	candles := optimizerCandles(80)
	shuffled := append([]*exchange.Candle{}, candles...)
	shuffled[0], shuffled[79] = shuffled[79], shuffled[0]

	train, val, holdOut, err := SplitCandlesForValidation(shuffled, 0.25)
	if err != nil {
		t.Fatalf("split validation failed: %v", err)
	}
	if !holdOut || len(train) != 60 || len(val) != 20 || train[0].Timestamp > train[1].Timestamp {
		t.Fatalf("unexpected split train=%d val=%d holdout=%v", len(train), len(val), holdOut)
	}
	if _, _, _, err := SplitCandlesForValidation(optimizerCandles(10), 0.2); err != errValidationNotEnoughBars {
		t.Fatalf("short validation split error = %v", err)
	}
	train, val, holdOut, err = SplitCandlesForValidation(candles, 0)
	if err != nil || holdOut || val != nil || len(train) != len(candles) {
		t.Fatalf("disabled validation split train=%d val=%d holdout=%v err=%v", len(train), len(val), holdOut, err)
	}

	u := &UniversalOptimizer{}
	if combos := u.EnumerateParamCombos(UniversalSearchSpace{}); len(combos) != 1 || len(combos[0]) != 0 {
		t.Fatalf("empty universal combos = %#v", combos)
	}
	combos := u.EnumerateParamCombos(UniversalSearchSpace{Ranges: map[string]ParamRange{
		"b": {Min: 1, Max: 2, Step: 1},
		"a": {Min: 0, Max: 0.2, Step: 0.1},
	}})
	if len(combos) != 6 {
		t.Fatalf("universal combos = %d, want 6", len(combos))
	}
	if vals := stepsRange(3, 1, 1); len(vals) != 1 || vals[0] != 3 {
		t.Fatalf("reversed steps range = %#v", vals)
	}
	if vals := stepsRange(1, 2, 0); len(vals) != 1 || vals[0] != 1 {
		t.Fatalf("zero step range = %#v", vals)
	}

	m := map[string]interface{}{
		"float": float64(1.7),
		"int":   2,
		"int64": int64(3),
		"json":  json.Number("4.8"),
		"bad":   "x",
	}
	if getFloatParam(nil, "x", 9) != 9 || getFloatParam(m, "missing", 9) != 9 || getFloatParam(m, "json", 0) != 4.8 || getFloatParam(m, "bad", 9) != 9 {
		t.Fatalf("getFloatParam returned unexpected values")
	}
	if getIntParam(nil, "x", 9) != 9 || getIntParam(m, "missing", 9) != 9 || getIntParam(m, "float", 0) != 1 || getIntParam(m, "json", 0) != 4 || getIntParam(m, "bad", 9) != 9 {
		t.Fatalf("getIntParam returned unexpected values")
	}

	for _, strategy := range []string{"grid", "momentum", "mean_reversion", "trend_following", "dca", "martingale", "unknown"} {
		space := GetDefaultSearchSpace(strategy)
		if space.Strategy != strategy {
			t.Fatalf("default strategy %q returned %#v", strategy, space)
		}
	}
	if _, err := u.RunOne(context.Background(), "BTCUSDT", "1m", candles, "unknown", nil, 1000); err == nil {
		t.Fatalf("unknown strategy should fail")
	}
}

func TestGeneticAndBayesianInternalAlgorithms(t *testing.T) {
	space := tinySearchSpace()
	totalCapital := 1000.0

	g := NewGeneticOptimizer()
	g.randSrc = rand.New(rand.NewSource(1))
	g.MutationRate = 1
	boundsG := g.spaceBounds(space)
	p := ParamsFromSpace(90, 110, 3, 20, totalCapital, 0.001, 0.002)
	vec := g.paramsToVec(p, boundsG)
	if len(vec) != 4 {
		t.Fatalf("genetic vector len = %d", len(vec))
	}
	roundTrip := g.vecToParams([]float64{0, 0, 0.5, 0.5}, boundsG, space, totalCapital, 0.001, 0.002)
	if roundTrip.PriceHigh <= roundTrip.PriceLow || roundTrip.FeeRate != 0.001 {
		t.Fatalf("genetic vecToParams invalid: %#v", roundTrip)
	}
	sampled := g.sampleParams(space, totalCapital, 0.001, 0.002)
	if sampled.PriceHigh <= sampled.PriceLow || sampled.OrderQuantity <= 0 {
		t.Fatalf("genetic sample invalid: %#v", sampled)
	}
	pop := []individual{
		{genes: []float64{0, 0, 0, 0}, trainScore: 1},
		{genes: []float64{1, 1, 1, 1}, trainScore: 5},
	}
	selected := g.tournamentSelect(pop, 3)
	if len(selected.genes) != 4 {
		t.Fatalf("selected individual missing genes: %#v", selected)
	}
	child := g.crossover(pop[0].genes, pop[1].genes)
	g.mutate(child)
	for _, gene := range child {
		if gene < 0 || gene > 1 {
			t.Fatalf("mutated gene out of range: %#v", child)
		}
	}
	emptyResult, err := g.buildResultFromPopulation(nil, nil, time.Millisecond, "genetic", true, 0.001, 0.002)
	if err != nil || emptyResult.Method != "genetic" || !emptyResult.HoldOutEnabled {
		t.Fatalf("empty genetic result = %#v err=%v", emptyResult, err)
	}

	b := NewBayesianOptimizer()
	b.randSrc = rand.New(rand.NewSource(2))
	boundsB := b.spaceBounds(space)
	vecB := b.paramsToVec(p, boundsB)
	if len(vecB) != 4 {
		t.Fatalf("bayesian vector len = %d", len(vecB))
	}
	if kernel := b.rbfKernel([]float64{0, 0}, []float64{0, 0}); kernel != 1 {
		t.Fatalf("same-point kernel = %v, want 1", kernel)
	}
	mu, sigma := b.gpPredict(nil, nil, []float64{0.5, 0.5})
	if mu != 0 || sigma != 1 {
		t.Fatalf("empty GP prediction = %v %v", mu, sigma)
	}
	mu, sigma = b.gpPredict([][]float64{{0}, {1}}, []float64{1, 2}, []float64{0.5})
	if sigma <= 0 || math.IsNaN(mu) {
		t.Fatalf("GP prediction invalid: mu=%v sigma=%v", mu, sigma)
	}
	if _, err := solveSym([][]float64{{0}}, []float64{1}); err != errInvalidRange {
		t.Fatalf("non positive matrix error = %v", err)
	}
	solution, err := solveSym([][]float64{{4, 1}, {1, 3}}, []float64{1, 2})
	if err != nil || len(solution) != 2 {
		t.Fatalf("solveSym failed: %#v err=%v", solution, err)
	}
	if b.expectedImprovement(2, 0, 1) != 1 || b.expectedImprovement(0, 0, 1) != 0 {
		t.Fatalf("zero-sigma expected improvement unexpected")
	}
	if ei := b.expectedImprovement(2, 0.5, 1); ei <= 0 {
		t.Fatalf("positive expected improvement = %v", ei)
	}
	emptyBayes, err := b.buildResultFromParamResults(nil, "bayesian", false, 0.001, 0.002)
	if err != nil || emptyBayes.Method != "bayesian" || emptyBayes.FeeRateUsed != 0.001 {
		t.Fatalf("empty bayesian result = %#v err=%v", emptyBayes, err)
	}
}
