package strategy

import (
	"testing"

	"quantmesh/config"
	"quantmesh/position"
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

func TestMultiStrategyExecutorClassifiesReduceAndOpenOrders(t *testing.T) {
	tests := []struct {
		name         string
		direction    string
		strategyName string
		req          *position.OrderRequest
		wantReduce   bool
	}{
		{
			name:         "long sell is close by default",
			direction:    "LONG",
			strategyName: "grid-long",
			req:          &position.OrderRequest{Side: "SELL"},
			wantReduce:   true,
		},
		{
			name:         "short sell opens position",
			direction:    "SHORT",
			strategyName: "grid-short",
			req:          &position.OrderRequest{Side: "SELL"},
			wantReduce:   false,
		},
		{
			name:         "both sell can open short leg",
			direction:    "BOTH",
			strategyName: "grid-both",
			req:          &position.OrderRequest{Side: "SELL"},
			wantReduce:   false,
		},
		{
			name:         "reduce only sell always closes",
			direction:    "BOTH",
			strategyName: "grid-both",
			req:          &position.OrderRequest{Side: "SELL", ReduceOnly: true},
			wantReduce:   true,
		},
		{
			name:         "strategy name short opens even without global direction",
			direction:    "LONG",
			strategyName: "futures_short",
			req:          &position.OrderRequest{Side: "SELL"},
			wantReduce:   false,
		},
		{
			name:         "buy opens unless explicitly reduce only",
			direction:    "LONG",
			strategyName: "grid-long",
			req:          &position.OrderRequest{Side: "BUY"},
			wantReduce:   false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &config.Config{}
			cfg.Trading.Direction = tc.direction
			mse := NewMultiStrategyExecutor(nil, NewCapitalAllocator(cfg, 1000))

			if got := mse.isReducePositionOrder(tc.strategyName, tc.req); got != tc.wantReduce {
				t.Fatalf("isReducePositionOrder()=%v want %v", got, tc.wantReduce)
			}
		})
	}
}
