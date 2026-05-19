package strategy

import (
	"math"
	"testing"

	"quantmesh/config"
)

func TestCapitalAllocatorFixedPoolsCannotOverAllocate(t *testing.T) {
	allocator := NewCapitalAllocator(&config.Config{}, 100)
	allocator.RegisterStrategy("fixed-a", 0, 80)
	allocator.RegisterStrategy("fixed-b", 0, 80)
	allocator.RegisterStrategy("weighted", 1, 0)

	allocator.Allocate()

	all := allocator.GetAllStrategiesCapital()
	totalAllocated := 0.0
	for name, capital := range all {
		if capital.Allocated < 0 || capital.Available < 0 {
			t.Fatalf("%s 不应出现负资金: allocated=%.8f available=%.8f", name, capital.Allocated, capital.Available)
		}
		totalAllocated += capital.Allocated
	}

	if math.Abs(totalAllocated-100) > 1e-9 {
		t.Fatalf("超额固定池应被缩放到总资金内: total=%.8f", totalAllocated)
	}
	if math.Abs(all["fixed-a"].Allocated-50) > 1e-9 || math.Abs(all["fixed-b"].Allocated-50) > 1e-9 {
		t.Fatalf("固定池应按比例缩放: a=%.8f b=%.8f", all["fixed-a"].Allocated, all["fixed-b"].Allocated)
	}
	if all["weighted"].Allocated != 0 || all["weighted"].Available != 0 {
		t.Fatalf("固定池超额时权重策略不应获得负数或额外资金: allocated=%.8f available=%.8f",
			all["weighted"].Allocated, all["weighted"].Available)
	}
}

func TestCapitalAllocatorIgnoresNegativeReserveAndRelease(t *testing.T) {
	allocator := NewCapitalAllocator(&config.Config{}, 100)
	allocator.RegisterStrategy("grid", 1, 0)
	allocator.Allocate()

	if !allocator.Reserve("grid", -10) {
		t.Fatal("非正预留金额应被视为无操作成功，避免上层重试污染状态")
	}
	if allocator.GetAvailable("grid") != 100 || allocator.GetUsed("grid") != 0 {
		t.Fatalf("负数预留不应增加可用资金或减少占用: available=%.8f used=%.8f",
			allocator.GetAvailable("grid"), allocator.GetUsed("grid"))
	}

	if !allocator.Reserve("grid", 40) {
		t.Fatal("正数预留应成功")
	}
	allocator.Release("grid", -20)
	if allocator.GetAvailable("grid") != 60 || allocator.GetUsed("grid") != 40 {
		t.Fatalf("负数释放不应改变资金状态: available=%.8f used=%.8f",
			allocator.GetAvailable("grid"), allocator.GetUsed("grid"))
	}
}

func TestCapitalAllocatorClampsNegativeStrategyConfig(t *testing.T) {
	allocator := NewCapitalAllocator(&config.Config{}, 100)
	allocator.RegisterStrategy("bad", -1, -50)
	allocator.Allocate()

	if allocator.GetAllocated("bad") != 0 || allocator.GetAvailable("bad") != 0 {
		t.Fatalf("负权重和负固定池应归零: allocated=%.8f available=%.8f",
			allocator.GetAllocated("bad"), allocator.GetAvailable("bad"))
	}
}
