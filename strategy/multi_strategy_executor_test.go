package strategy

import (
	"testing"

	"quantmesh/config"
)

func TestReleaseOrderCapitalByClientOrderIDAvoidsDoubleRelease(t *testing.T) {
	cfg := &config.Config{}
	allocator := NewCapitalAllocator(cfg, 1000)
	allocator.RegisterStrategy("dca", 1, 0)
	allocator.Allocate()
	if !allocator.Reserve("dca", 20) {
		t.Fatal("预留资金失败")
	}

	mse := NewMultiStrategyExecutor(nil, allocator)
	mse.mu.Lock()
	mse.clientStrategies["client-1"] = "dca"
	mse.clientReserved["client-1"] = 10
	mse.clientToOrderID["client-1"] = 10001
	mse.strategies["10001"] = "dca"
	mse.orderReservedAmount[10001] = 10
	mse.mu.Unlock()

	mse.ReleaseOrderCapitalByClientOrderID("client-1")
	if got := allocator.GetUsed("dca"); got != 10 {
		t.Fatalf("按 clientOrderID 释放后 used 应为 10，实际 %.2f", got)
	}

	// 再按 orderID 释放，不应重复释放
	mse.ReleaseOrderCapitalByOrderID(10001)
	if got := allocator.GetUsed("dca"); got != 10 {
		t.Fatalf("重复释放后 used 不应变化，实际 %.2f", got)
	}
}
