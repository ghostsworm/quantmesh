package strategy

import (
	"math"
	"testing"
	"time"

	"quantmesh/config"
)

func TestCapitalAllocatorFixedWeightedReserveAndRelease(t *testing.T) {
	cfg := &config.Config{}
	allocator := NewCapitalAllocator(cfg, 1000)
	if allocator.GetConfig() != cfg {
		t.Fatalf("config pointer mismatch")
	}

	allocator.RegisterStrategy("fixed", 0, 300)
	allocator.RegisterStrategy("weighted-a", 2, 0)
	allocator.RegisterStrategy("weighted-b", 1, 0)
	allocator.RegisterStrategy("negative", -1, -10)
	allocator.Allocate()

	if got := allocator.GetAllocated("fixed"); got != 300 {
		t.Fatalf("fixed allocation = %v", got)
	}
	if got := allocator.GetAllocated("weighted-a"); math.Abs(got-466.6666667) > 0.0001 {
		t.Fatalf("weighted-a allocation = %v", got)
	}
	if got := allocator.GetAllocated("weighted-b"); math.Abs(got-233.3333333) > 0.0001 {
		t.Fatalf("weighted-b allocation = %v", got)
	}
	if got := allocator.GetAllocated("negative"); got != 0 {
		t.Fatalf("negative allocation = %v", got)
	}
	if allocator.CheckAvailable("missing", 1) || !allocator.CheckAvailable("missing", 0) {
		t.Fatalf("missing strategy availability mismatch")
	}

	if !allocator.Reserve("weighted-a", 100) {
		t.Fatalf("reserve should succeed")
	}
	if allocator.GetUsed("weighted-a") != 100 || allocator.GetAvailable("weighted-a") <= 300 {
		t.Fatalf("reserve state used=%v available=%v", allocator.GetUsed("weighted-a"), allocator.GetAvailable("weighted-a"))
	}
	if allocator.Reserve("weighted-a", 1000) {
		t.Fatalf("oversized reserve should fail")
	}
	allocator.Release("weighted-a", 40)
	if allocator.GetUsed("weighted-a") != 60 {
		t.Fatalf("release used = %v", allocator.GetUsed("weighted-a"))
	}
	allocator.Release("weighted-a", 1000)
	if allocator.GetUsed("weighted-a") != 0 {
		t.Fatalf("over release should reset used")
	}
	allocator.Reserve("fixed", 50)
	if released := allocator.ReleaseAll("fixed"); released != 50 {
		t.Fatalf("release all fixed = %v", released)
	}
	if released := allocator.ReleaseAll("missing"); released != 0 {
		t.Fatalf("release all missing = %v", released)
	}

	allocator.Reserve("weighted-a", 10)
	allocator.Reserve("weighted-b", 20)
	released := allocator.ReleaseAllStrategies()
	if released["weighted-a"] != 10 || released["weighted-b"] != 20 {
		t.Fatalf("release all strategies = %#v", released)
	}
	snapshot := allocator.GetAllStrategiesCapital()
	snapshot["fixed"].Allocated = 1
	if allocator.GetAllocated("fixed") == 1 {
		t.Fatalf("snapshot should be a copy")
	}

	scaled := NewCapitalAllocator(cfg, 100)
	scaled.RegisterStrategy("a", 0, 80)
	scaled.RegisterStrategy("b", 0, 70)
	scaled.Allocate()
	if math.Abs(scaled.GetAllocated("a")+scaled.GetAllocated("b")-100) > 0.0001 {
		t.Fatalf("scaled fixed allocations exceed total")
	}
}

func TestCapitalAllocatorRejectsNonFiniteAmounts(t *testing.T) {
	allocator := NewCapitalAllocator(&config.Config{}, 1000)
	allocator.RegisterStrategy("strategy", 1, 0)
	allocator.Allocate()

	if allocator.Reserve("strategy", math.NaN()) || allocator.Reserve("strategy", math.Inf(1)) {
		t.Fatal("non-finite reserve must fail")
	}
	if !allocator.Reserve("strategy", 100) {
		t.Fatal("finite reserve should succeed")
	}
	allocator.Release("strategy", math.NaN())
	allocator.Release("strategy", math.Inf(1))
	if got := allocator.GetUsed("strategy"); got != 100 {
		t.Fatalf("invalid release changed used capital: got %v want 100", got)
	}
	if allocator.CheckAvailable("strategy", math.NaN()) || allocator.CheckAvailable("strategy", math.Inf(1)) {
		t.Fatal("non-finite availability checks must fail")
	}
}

func TestCapitalAllocatorFailsClosedForInvalidAllocationInputs(t *testing.T) {
	tests := []struct {
		name         string
		totalCapital float64
		weight       float64
		fixedPool    float64
		secondWeight float64
	}{
		{name: "nan total", totalCapital: math.NaN(), weight: 1},
		{name: "infinite total", totalCapital: math.Inf(1), weight: 1},
		{name: "nan weight", totalCapital: 1000, weight: math.NaN()},
		{name: "infinite fixed pool", totalCapital: 1000, fixedPool: math.Inf(1)},
		{name: "weight sum overflow", totalCapital: 1000, weight: 1e308, secondWeight: 1e308},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			allocator := NewCapitalAllocator(&config.Config{}, tc.totalCapital)
			allocator.RegisterStrategy("strategy", tc.weight, tc.fixedPool)
			if tc.secondWeight != 0 {
				allocator.RegisterStrategy("strategy-2", tc.secondWeight, 0)
			}
			allocator.Allocate()
			if got := allocator.GetAllocated("strategy"); got != 0 {
				t.Fatalf("allocated=%v want 0", got)
			}
			if got := allocator.GetAvailable("strategy"); got != 0 {
				t.Fatalf("available=%v want 0", got)
			}
			if allocator.Reserve("strategy", 1) {
				t.Fatal("invalid allocation input must not allow reserve")
			}
		})
	}
}

func TestDynamicAllocatorWeightsRebalanceAndPerformance(t *testing.T) {
	cfg := &config.Config{}
	cfg.Strategies.CapitalAllocation.DynamicAllocation.RebalanceInterval = 0
	da := NewDynamicAllocator(cfg)

	if da.rebalanceInterval != time.Hour || da.maxChangePerRebalance != 0.05 || da.minWeight != 0.1 || da.maxWeight != 0.7 {
		t.Fatalf("unexpected defaults: %#v", da)
	}
	if len(da.performanceWeights) == 0 {
		t.Fatalf("default performance weights missing")
	}

	da.RegisterStrategy("grid", 0.5)
	da.RegisterStrategy("dca", 0.5)
	da.UpdatePerformance("grid", 500, true)
	da.UpdatePerformance("grid", 200, true)
	da.UpdatePerformance("dca", -100, false)
	da.UpdatePerformance("missing", 1000, true)

	gridPerf := da.GetPerformance("grid")
	if gridPerf == nil || gridPerf.TotalTrades != 2 || gridPerf.WinningTrades != 2 || gridPerf.WinRate != 1 {
		t.Fatalf("grid performance = %#v", gridPerf)
	}
	if da.GetPerformance("missing") != nil {
		t.Fatalf("missing performance should be nil")
	}
	da.strategies["grid"].SharpeRatio = 3
	da.strategies["grid"].MaxDrawdown = 0.1
	da.strategies["dca"].MaxDrawdown = 0.8

	targets := da.CalculateTargetWeights()
	if len(targets) != 2 || targets["grid"] <= targets["dca"] {
		t.Fatalf("target weights = %#v", targets)
	}
	adjusted := da.Rebalance(map[string]float64{"grid": 0.7, "dca": 0.1, "missing": 0.9})
	if math.Abs(adjusted["grid"]-0.55) > 0.0001 || math.Abs(adjusted["dca"]-0.45) > 0.0001 {
		t.Fatalf("adjusted weights = %#v", adjusted)
	}

	zeroDA := NewDynamicAllocator(&config.Config{})
	zeroDA.RegisterStrategy("flat", 0.4)
	zeroTargets := zeroDA.CalculateTargetWeights()
	if zeroTargets["flat"] != 0.4 {
		t.Fatalf("zero-score targets = %#v", zeroTargets)
	}

	allocator := NewCapitalAllocator(&config.Config{}, 1000)
	allocator.RegisterStrategy("grid", 0.5, 0)
	da.Start(allocator)
	da.Stop()
}

func TestDynamicAllocatorPreservesWeightsWithoutVerifiedMetrics(t *testing.T) {
	cfg := &config.Config{}
	cfg.Strategies.CapitalAllocation.DynamicAllocation.PerformanceWeights = map[string]float64{
		"total_pnl": 0.4, "sharpe_ratio": 0.3, "win_rate": 0.2, "max_drawdown": 0.1,
	}
	da := NewDynamicAllocator(cfg)
	da.RegisterStrategy("grid", 0.7)
	da.RegisterStrategy("dca", 0.3)

	got := da.CalculateTargetWeights()
	if got["grid"] != 0.7 || got["dca"] != 0.3 {
		t.Fatalf("weights without any trade samples = %#v, want existing allocations", got)
	}
	da.minWeight = 0.5
	got = da.Rebalance(got)
	if got["grid"] != 0.7 || got["dca"] != 0.3 {
		t.Fatalf("rebalance changed weights without evidence: %#v", got)
	}

	da.UpdatePerformance("grid", 10, true)
	da.UpdatePerformance("dca", 10, true)
	da.strategies["grid"].mu.Lock()
	da.strategies["grid"].SharpeRatio = 3
	da.strategies["grid"].MaxDrawdown = 0
	da.strategies["grid"].mu.Unlock()
	da.strategies["dca"].mu.Lock()
	da.strategies["dca"].SharpeRatio = 0
	da.strategies["dca"].MaxDrawdown = 0.9
	da.strategies["dca"].mu.Unlock()

	got = da.CalculateTargetWeights()
	if math.Abs(got["grid"]-0.5) > 0.0001 || math.Abs(got["dca"]-0.5) > 0.0001 {
		t.Fatalf("uncomputed Sharpe/drawdown changed equal observed metrics: %#v", got)
	}
}

func TestDynamicAllocatorTargetsAndRebalancesWithinBoundsAndUnitSum(t *testing.T) {
	cfg := &config.Config{}
	cfg.Strategies.CapitalAllocation.DynamicAllocation.MinWeight = 0.1
	cfg.Strategies.CapitalAllocation.DynamicAllocation.MaxWeight = 0.7
	cfg.Strategies.CapitalAllocation.DynamicAllocation.MaxChangePerRebalance = 0.05
	da := NewDynamicAllocator(cfg)
	da.RegisterStrategy("winner", 0.5)
	da.RegisterStrategy("loser", 0.5)
	da.UpdatePerformance("winner", 1000, true)
	da.UpdatePerformance("loser", -1000, false)

	targets := da.CalculateTargetWeights()
	if math.Abs(targets["winner"]-0.7) > 1e-9 || math.Abs(targets["loser"]-0.3) > 1e-9 {
		t.Fatalf("bounded target weights = %#v, want 0.7/0.3", targets)
	}

	for step := 0; step < 10; step++ {
		previous := da.GetPerformance("winner").CurrentWeight
		weights := da.Rebalance(targets)
		total := weights["winner"] + weights["loser"]
		if math.Abs(total-1) > 1e-9 {
			t.Fatalf("step %d weights sum to %v: %#v", step, total, weights)
		}
		for name, weight := range weights {
			if weight < da.minWeight-1e-9 || weight > da.maxWeight+1e-9 {
				t.Fatalf("step %d weight %s=%v violates bounds: %#v", step, name, weight, weights)
			}
		}
		if math.Abs(weights["winner"]-previous) > da.maxChangePerRebalance+1e-9 {
			t.Fatalf("step %d exceeded per-rebalance change", step)
		}
	}
}

func TestDynamicAllocatorRetainsWeightsWhenBoundsCannotFitStrategyCount(t *testing.T) {
	cfg := &config.Config{}
	cfg.Strategies.CapitalAllocation.DynamicAllocation.MinWeight = 0.1
	cfg.Strategies.CapitalAllocation.DynamicAllocation.MaxWeight = 0.7
	da := NewDynamicAllocator(cfg)
	da.RegisterStrategy("only", 0.6)
	da.UpdatePerformance("only", 1000, true)

	if got := da.CalculateTargetWeights(); got["only"] != 0.6 {
		t.Fatalf("infeasible single-strategy bounds changed weight: %#v", got)
	}
	if got := da.Rebalance(map[string]float64{"only": 0.9}); got["only"] != 0.6 {
		t.Fatalf("infeasible single-strategy rebalance changed weight: %#v", got)
	}
}
func TestDynamicAllocatorRejectsNonFinitePerformanceSamples(t *testing.T) {
	for _, pnl := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		da := NewDynamicAllocator(&config.Config{})
		da.RegisterStrategy("grid", 0.6)
		da.UpdatePerformance("grid", pnl, true)
		got := da.GetPerformance("grid")
		if got == nil || got.TotalTrades != 0 || got.TotalPnL != 0 || got.WinRate != 0 {
			t.Fatalf("invalid pnl %v mutated performance: %+v", pnl, got)
		}
		if weights := da.CalculateTargetWeights(); weights["grid"] != 0.6 {
			t.Fatalf("invalid pnl %v changed capital weight: %#v", pnl, weights)
		}
	}

	da := NewDynamicAllocator(&config.Config{})
	da.RegisterStrategy("grid", 0.6)
	da.UpdatePerformance("grid", math.MaxFloat64, true)
	da.UpdatePerformance("grid", math.MaxFloat64, true)
	got := da.GetPerformance("grid")
	if got == nil || got.TotalTrades != 1 || got.TotalPnL != math.MaxFloat64 {
		t.Fatalf("overflowing pnl mutated performance: %+v", got)
	}
}

func TestDynamicAllocatorRejectsPerformanceSampleAtomicallyOnCounterOverflow(t *testing.T) {
	da := NewDynamicAllocator(&config.Config{})
	da.RegisterStrategy("grid", 0.6)
	perf := da.strategies["grid"]
	maxInt := int(^uint(0) >> 1)
	perf.mu.Lock()
	perf.TotalPnL = 12
	perf.TotalTrades = maxInt
	perf.WinningTrades = maxInt
	perf.WinRate = 1
	perf.mu.Unlock()

	da.UpdatePerformance("grid", 7, true)
	got := da.GetPerformance("grid")
	if got == nil || got.TotalPnL != 12 || got.TotalTrades != maxInt || got.WinningTrades != maxInt || got.WinRate != 1 {
		t.Fatalf("counter overflow partially applied performance sample: %+v", got)
	}
}

func TestDynamicAllocatorRejectsInvalidWeights(t *testing.T) {
	cfg := &config.Config{}
	cfg.Strategies.CapitalAllocation.DynamicAllocation.PerformanceWeights = map[string]float64{
		"total_pnl": math.NaN(),
		"win_rate":  math.Inf(1),
	}
	da := NewDynamicAllocator(cfg)
	da.RegisterStrategy("a", 0.5)
	da.RegisterStrategy("b", 0.5)
	da.UpdatePerformance("a", 100, true)
	da.UpdatePerformance("b", 0, false)
	targets := da.CalculateTargetWeights()
	if targets["a"] != 0.5 || targets["b"] != 0.5 {
		t.Fatalf("invalid score weights changed targets: %#v", targets)
	}
	if got := da.Rebalance(map[string]float64{"a": math.NaN(), "b": math.Inf(1)}); len(got) != 0 {
		t.Fatalf("invalid targets should not be applied: %#v", got)
	}
	if da.GetPerformance("a").CurrentWeight != 0.5 || da.GetPerformance("b").CurrentWeight != 0.5 {
		t.Fatal("invalid targets changed current weights")
	}
}
