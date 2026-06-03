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
	if zeroTargets["flat"] != 1 {
		t.Fatalf("zero-score targets = %#v", zeroTargets)
	}

	allocator := NewCapitalAllocator(&config.Config{}, 1000)
	allocator.RegisterStrategy("grid", 0.5, 0)
	da.Start(allocator)
	da.Stop()
}
