package main

import (
	"errors"
	"sync/atomic"
	"testing"

	"quantmesh/config"
	"quantmesh/execution"
	"quantmesh/lock"
	"quantmesh/order"
	"quantmesh/position"
	"quantmesh/strategy"
)

func TestLiveRiskAdmissionReachesPhysicalStrategyExecutor(t *testing.T) {
	cfg := &config.Config{}
	cfg.Trading.Symbol, cfg.Trading.MarketType, cfg.Trading.Direction = "BTCUSDT", "futures", "LONG"
	venue := &sharedGateExchange{}
	physical := order.NewExchangeOrderExecutor(venue, "BTCUSDT", 0, 0, lock.NewNopLock(), "")
	grid := &exchangeExecutorAdapter{executor: physical}
	spm := position.NewSuperPositionManager(cfg, grid, &positionExchangeAdapter{exchange: venue}, 2, 4)
	var healthy atomic.Bool
	healthy.Store(true)
	spm.OpeningGate().ApplyAdmissionContext(execution.WithOpeningAdmissionCheck(t.Context(), healthy.Load))
	physical.SetOpeningGate(spm.OpeningGate(), "LONG")
	allocator := strategy.NewCapitalAllocator(cfg, 1000)
	allocator.RegisterStrategy("trend", 1, 0)
	allocator.Allocate()
	adapter := strategy.NewMultiStrategyExecutorAdapter(strategy.NewMultiStrategyExecutor(physical, allocator), "trend")
	open := &position.OrderRequest{Symbol: "BTCUSDT", Side: "BUY", Price: 100, Quantity: 1, ClientOrderID: "expiry", PositionSide: position.PositionSideLong}
	healthy.Store(false)
	for _, executor := range []position.OrderExecutorInterface{grid, adapter} {
		if _, err := executor.PlaceOrder(open); !errors.Is(err, execution.ErrOpeningPaused) {
			t.Fatalf("expired admission reached physical executor: %v", err)
		}
	}
	if venue.placed != 0 || allocator.GetAvailable("trend") != 1000 {
		t.Fatal("rejected risk reached venue or leaked capital")
	}
	close := &position.OrderRequest{Symbol: "BTCUSDT", Side: "SELL", Price: 90, Quantity: 1, ReduceOnly: true, ClientOrderID: "protective"}
	if _, err := adapter.PlaceOrder(close); err != nil || venue.placed != 1 {
		t.Fatalf("expiry blocked protective close: %v", err)
	}
	healthy.Store(true)
	if _, err := adapter.PlaceOrder(open); err != nil || venue.placed != 2 {
		t.Fatalf("renewed admission failed: %v", err)
	}
}
