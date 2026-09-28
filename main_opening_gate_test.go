package main

import (
	"errors"
	"testing"

	"quantmesh/config"
	"quantmesh/execution"
	"quantmesh/lock"
	"quantmesh/order"
	"quantmesh/position"
	"quantmesh/strategy"
)

type sharedGateExchange struct{ lockedExecutorExchange }

func (*sharedGateExchange) GetMarketType() string { return "futures" }
func (*sharedGateExchange) EstimateFinalOrderAmount(_ string, price, quantity float64, _ bool) float64 {
	return price * quantity
}

func TestBotOpeningGateReachesGridAndAllStrategyAdapters(t *testing.T) {
	for _, name := range []string{"trend", "mean_reversion", "momentum", "martingale", "dca", "dca_enhanced", "combo", "spot_short", "spot_long", "futures_short", "futures_long"} {
		t.Run(name, func(t *testing.T) {
			cfg := &config.Config{}
			cfg.Trading.Symbol = "BTCUSDT"
			cfg.Trading.MarketType = "futures"
			cfg.Trading.Direction = "LONG"
			// Configured pause must be active even before OpeningController starts.
			cfg.Trading.OpenPositionControl.PauseOpening = true
			ex := &sharedGateExchange{}
			physical := order.NewExchangeOrderExecutor(ex, "BTCUSDT", 0, 0, lock.NewNopLock(), "")
			grid := &exchangeExecutorAdapter{executor: physical}
			spm := position.NewSuperPositionManager(cfg, grid, &positionExchangeAdapter{exchange: ex}, 2, 4)
			physical.SetOpeningGate(spm.OpeningGate(), cfg.Trading.Direction)
			allocator := strategy.NewCapitalAllocator(cfg, 1000)
			allocator.RegisterStrategy(name, 1, 0)
			allocator.Allocate()
			multi := strategy.NewMultiStrategyExecutor(physical, allocator)
			adapter := strategy.NewMultiStrategyExecutorAdapter(multi, name)
			open := &position.OrderRequest{Symbol: "BTCUSDT", Side: "BUY", Price: 100, Quantity: 1, ClientOrderID: "open", PositionSide: position.PositionSideLong}
			for _, executor := range []position.OrderExecutorInterface{grid, adapter} {
				if _, err := executor.PlaceOrder(open); !errors.Is(err, execution.ErrOpeningPaused) {
					t.Fatalf("expected shared pause: %v", err)
				}
			}
			if ex.placed != 0 || allocator.GetAvailable(name) != 1000 {
				t.Fatalf("rejected open reached venue or leaked reservation: placed=%d available=%f", ex.placed, allocator.GetAvailable(name))
			}
			close := &position.OrderRequest{Symbol: "BTCUSDT", Side: "SELL", Price: 90, Quantity: 1, ReduceOnly: true, ClientOrderID: "close"}
			if _, err := adapter.PlaceOrder(close); err != nil || ex.placed != 1 {
				t.Fatalf("protective close blocked: %v", err)
			}
			spm.SetMarketRiskPaused(true)
			spm.ResumeOpening()
			if _, err := adapter.PlaceOrder(open); !errors.Is(err, execution.ErrOpeningPaused) {
				t.Fatalf("manual resume cleared market risk: %v", err)
			}
			spm.SetMarketRiskPaused(false)
			if _, err := adapter.PlaceOrder(open); err != nil || ex.placed != 2 {
				t.Fatalf("runtime resume did not reach strategy: %v", err)
			}
		})
	}
}
