package main

import (
	"context"
	"errors"
	"quantmesh/config"
	"quantmesh/exchange"
	"quantmesh/execution"
	"quantmesh/indicators"
	"quantmesh/lock"
	"quantmesh/monitor"
	"quantmesh/order"
	"quantmesh/position"
	"quantmesh/strategy"
	"testing"
	"time"
)

type volatilityGateExchange struct{ sharedGateExchange }

func (*volatilityGateExchange) GetOpenOrders(context.Context, string) ([]*exchange.Order, error) {
	return nil, nil
}

func TestBotVolatilityWarmupBlocksSharedPhysicalAdapters(t *testing.T) {
	cfg := &config.Config{}
	cfg.Trading.Symbol = "BTCUSDT"
	cfg.Trading.Direction = "LONG"
	cfg.Trading.OpenPositionControl.BotRiskControl = &config.BotRiskControl{Enabled: true, VolatilityPauseEnabled: true, VolatilityPauseConfig: config.VolatilityPauseConfig{PauseOnExtremeVolatility: true}}
	ex := &volatilityGateExchange{}
	physical := order.NewExchangeOrderExecutor(ex, "BTCUSDT", 0, 0, lock.NewNopLock(), "")
	grid := &exchangeExecutorAdapter{executor: physical}
	spm := position.NewSuperPositionManager(cfg, grid, &positionExchangeAdapter{exchange: ex}, 2, 4)
	physical.SetOpeningGate(spm.OpeningGate(), "LONG")
	da := strategy.NewDynamicAdjuster(cfg, nil, spm)
	da.StartWithExternalPrices()
	t.Cleanup(da.Stop)
	allocator := strategy.NewCapitalAllocator(cfg, 1000)
	allocator.RegisterStrategy("trend", 1, 0)
	allocator.Allocate()
	multi := strategy.NewMultiStrategyExecutor(physical, allocator)
	adapter := strategy.NewMultiStrategyExecutorAdapter(multi, "trend")
	for _, executor := range []position.OrderExecutorInterface{grid, adapter} {
		_, err := executor.PlaceOrder(&position.OrderRequest{Symbol: "BTCUSDT", Side: "BUY", Price: 100, Quantity: 1, ClientOrderID: "vol-warmup-open", PositionSide: position.PositionSideLong})
		if !errors.Is(err, execution.ErrOpeningPaused) {
			t.Fatalf("warmup admitted opening: %v", err)
		}
	}
	if ex.placed != 0 || allocator.GetAvailable("trend") != 1000 {
		t.Fatal("warmup reached venue or leaked capital")
	}
}

func TestBotVolatilityHotUpdateReachesActivePriceConsumer(t *testing.T) {
	cfg := &config.Config{}
	cfg.Trading.Symbol = "ETHUSDT"
	cfg.Trading.Symbols = []config.SymbolConfig{{Symbol: "BTCUSDT"}}
	cfg.Trading.DynamicAdjustment.VolatilityDetection.ShortPeriod = 3
	cfg.Trading.DynamicAdjustment.VolatilityDetection.MediumPeriod = 3
	cfg.Trading.DynamicAdjustment.VolatilityDetection.LongPeriod = 3
	cfg.Trading.DynamicAdjustment.VolatilityDetection.PriceRangePeriod = 3
	spm := position.NewSuperPositionManager(cfg, &pauseTestExecutor{}, pauseTestExchange{}, 2, 3)
	da := strategy.NewDynamicAdjuster(cfg, nil, spm)
	da.StartWithExternalPrices()
	t.Cleanup(da.Stop)
	br := &BotRuntime{Inner: &SymbolRuntime{SuperPositionManager: spm, DynamicAdjuster: da}}
	end := time.Now().UTC().Truncate(time.Hour)
	prices := []float64{100, 200, 50, 200, 50, 200}
	points := make([]indicators.PricePoint, len(prices))
	for i, p := range prices {
		points[i] = indicators.PricePoint{Timestamp: end.Add(time.Duration(i-len(prices)+1) * time.Hour), Price: p, High: p, Low: p}
	}
	if err := da.ReplaceVolatilityHistory(points); err != nil {
		t.Fatal(err)
	}
	for _, price := range []float64{100, 200, 50, 200} {
		da.OnPriceChange(monitor.PriceChange{NewPrice: price})
	}
	if spm.IsVolatilityRiskPaused() {
		t.Fatal("disabled Bot risk paused trading")
	}
	rc := &config.BotRiskControl{Enabled: true, VolatilityPauseEnabled: true, VolatilityPauseConfig: config.VolatilityPauseConfig{PauseOnExtremeVolatility: true, AutoResumeOnNormal: true}}
	if err := br.SetBotRiskControl(rc); err != nil {
		t.Fatal(err)
	}
	if !spm.IsVolatilityRiskPaused() {
		t.Fatal("hot update waited for a new regime/tick")
	}
	if err := br.ResumeOpeningManually(); err == nil {
		t.Fatal("manual recovery bypassed live risk")
	}
	rc.VolatilityPauseEnabled = false
	if err := br.SetBotRiskControl(rc); err != nil {
		t.Fatal(err)
	}
	if spm.IsVolatilityRiskPaused() {
		t.Fatal("hot disable failed")
	}
}
