package main

import (
	"context"
	"sync"
	"testing"

	"quantmesh/config"
	"quantmesh/position"
	"quantmesh/utils"
)

func TestBotRuntimeRiskControlOwnsInputsAndPublishes(t *testing.T) {
	cfg := &config.Config{}
	cfg.Trading.Symbol = "BTCUSDT"
	cfg.Trading.PriceInterval = 100
	spm := position.NewSuperPositionManager(cfg, &pauseTestExecutor{}, pauseTestExchange{}, 2, 3)
	br := &BotRuntime{Inner: &SymbolRuntime{SuperPositionManager: spm}}
	rc := &config.BotRiskControl{Enabled: true, MaxPositionValue: 100}
	if err := br.SetRiskControls(rc, config.GridRiskControl{MaxGridLayers: 4}); err != nil {
		t.Fatal(err)
	}
	rc.MaxPositionValue = 999
	got := br.GetBotRiskControl()
	got.MaxPositionValue = 888
	if br.GetBotRiskControl().MaxPositionValue != 100 || spm.GetRiskControls().Open.BotRiskControl.MaxPositionValue != 100 || spm.GetRiskControls().Grid.MaxGridLayers != 4 {
		t.Fatal("runtime risk control aliased input or did not reach executor")
	}
	if err := br.SetBotRiskControl(&config.BotRiskControl{}); err != nil {
		t.Fatal(err)
	}
	br.PauseOpening("manual")
	if br.GetBotRiskControl().Enabled {
		t.Fatal("manual pause enabled independent risk override")
	}
}

func TestBotRuntimeRiskControlConcurrentReadUpdate(t *testing.T) {
	br := &BotRuntime{}
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 10000; i++ {
			_ = br.SetBotRiskControl(&config.BotRiskControl{MaxPositionLayers: i})
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 10000; i++ {
			br.GetBotRiskControl().MaxPositionLayers = -1
		}
	}()
	wg.Wait()
}

type riskStatusExchange struct{ pauseTestExchange }

func (riskStatusExchange) GetAccount(context.Context) (interface{}, error) {
	return struct{ AccountLeverage int }{AccountLeverage: 10}, nil
}

func TestBotRuntimePositionStatusUsesNominalMarkValue(t *testing.T) {
	cfg := &config.Config{}
	cfg.Trading.Symbol = "BTCUSDT"
	cfg.Trading.MarketType = "futures"
	cfg.Trading.PriceInterval = 1
	cfg.Trading.OrderQuantity = 100
	spm := position.NewSuperPositionManager(cfg, &pauseTestExecutor{}, riskStatusExchange{}, 2, 3)
	cid := utils.GenerateOrderIDWithSource(80, "BUY", 2, "")
	spm.OnOrderUpdate(position.OrderUpdate{OrderID: 1, ClientOrderID: cid, Symbol: "BTCUSDT", Status: "NEW", Side: "BUY", Price: 80})
	spm.OnOrderUpdate(position.OrderUpdate{OrderID: 1, ClientOrderID: cid, Symbol: "BTCUSDT", Status: "PARTIALLY_FILLED", Side: "BUY", Price: 80, AvgPrice: 80, ExecutedQty: 2})
	br := &BotRuntime{Inner: &SymbolRuntime{SuperPositionManager: spm}}
	if err := br.SetBotRiskControl(&config.BotRiskControl{Enabled: true, MaxPositionValue: 190}); err != nil {
		t.Fatal(err)
	}
	br.PauseOpening("manual")
	if err := spm.AdjustOrders(100); err != nil {
		t.Fatal(err)
	}
	status := br.GetPositionStatus()
	if status["total_position_qty"] != 2.0 || status["total_position_value"] != 200.0 || status["total_actual_margin"] != 20.0 || status["reached_limit_value"] != true || status["valuation_available"] != true {
		t.Fatalf("status uses wrong cost/margin basis: %v", status)
	}
}
