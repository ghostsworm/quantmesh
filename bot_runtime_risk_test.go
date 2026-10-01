package main

import (
	"context"
	"errors"
	"math"
	"sync"
	"testing"

	"quantmesh/config"
	"quantmesh/execution"
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

func TestBotRuntimeRejectsInvalidGridRiskControlsWithoutApplying(t *testing.T) {
	cfg := &config.Config{}
	cfg.Trading.Symbol = "BTCUSDT"
	cfg.Trading.PriceInterval = 100
	spm := position.NewSuperPositionManager(cfg, &pauseTestExecutor{}, pauseTestExchange{}, 2, 3)
	initial := config.GridRiskControl{Enabled: true, StopLossRatio: 0.1}
	br := &BotRuntime{Config: config.BotConfig{GridRiskControl: initial}, Inner: &SymbolRuntime{SuperPositionManager: spm}}
	if err := br.SetGridRiskControl(initial); err != nil {
		t.Fatal(err)
	}

	tests := []config.GridRiskControl{
		{Enabled: true, StopLossRatio: -0.1},
		{Enabled: true, StopLossRatio: 1.1},
		{Enabled: true, TrailingTakeProfitRatio: math.NaN()},
		{Enabled: true, StopLossBasis: "equitty"},
	}
	for _, invalid := range tests {
		if err := br.SetRiskControls(&config.BotRiskControl{}, invalid); err == nil {
			t.Fatalf("SetRiskControls accepted invalid grid controls: %+v", invalid)
		}
		if err := br.SetGridRiskControl(invalid); err == nil {
			t.Fatalf("SetGridRiskControl accepted invalid grid controls: %+v", invalid)
		}
		if got := br.GetGridRiskControl(); got != initial || spm.GetRiskControls().Grid != initial {
			t.Fatalf("invalid update changed effective config: bot=%+v spm=%+v", got, spm.GetRiskControls().Grid)
		}
	}
}

func TestBotRuntimeRiskHotUpdateCannotExceedVerifiedCapitalBudget(t *testing.T) {
	cfg := &config.Config{}
	cfg.Trading.Symbol = "BTCUSDT"
	cfg.Trading.PriceInterval = 100
	spm := position.NewSuperPositionManager(cfg, &pauseTestExecutor{}, pauseTestExchange{}, 2, 3)
	br := &BotRuntime{Inner: &SymbolRuntime{SuperPositionManager: spm, verifiedCapitalBudget: 500}}
	if err := br.SetRiskControls(&config.BotRiskControl{Enabled: true, MaxPositionValue: 900}, config.GridRiskControl{}); err != nil {
		t.Fatal(err)
	}
	got := br.GetBotRiskControl()
	if got.MaxPositionValue != 500 {
		t.Fatalf("persistable Bot risk limit = %v, want verified ceiling 500", got.MaxPositionValue)
	}
	active := spm.GetRiskControls().Open
	if active.MaxPositionValue != 500 || active.BotRiskControl == nil || active.BotRiskControl.MaxPositionValue != 500 {
		t.Fatalf("runtime controls escaped verified capital ceiling: %+v", active)
	}
}

func TestSpecializedRiskHotUpdateUsesVerifiedCapitalAndRealApplyPath(t *testing.T) {
	var applied config.OpenPositionControl
	br := &BotRuntime{Inner: &SymbolRuntime{
		verifiedCapitalBudget: 500,
		UpdateOpenControl: func(control config.OpenPositionControl) error {
			applied = config.CloneOpenPositionControl(control)
			return nil
		},
	}}
	if err := br.SetRiskControls(&config.BotRiskControl{Enabled: true, MaxPositionValue: 900}, config.GridRiskControl{}); err != nil {
		t.Fatal(err)
	}
	if applied.BotRiskControl == nil || applied.BotRiskControl.MaxPositionValue != 500 || br.GetBotRiskControl().MaxPositionValue != 500 {
		t.Fatalf("specialized risk limit escaped its verified cap: applied=%+v config=%+v", applied, br.GetBotRiskControl())
	}
	if err := br.SetRiskControls(&config.BotRiskControl{Enabled: true}, config.GridRiskControl{MaxGridLayers: 2}); err == nil {
		t.Fatal("specialized runtime reported unsupported grid-risk update as applied")
	}

	unsupported := &BotRuntime{Inner: &SymbolRuntime{verifiedCapitalBudget: 500}}
	if err := unsupported.SetBotRiskControl(&config.BotRiskControl{Enabled: true, MaxPositionValue: 400}); err == nil {
		t.Fatal("runtime without a real risk-control application path reported success")
	}
}

func TestSpecializedRiskApplyFailureRollsBackAndBlocksOpenings(t *testing.T) {
	applyErr := errors.New("strategy risk update failed")
	gate := &execution.OpeningGate{}
	br := &BotRuntime{Config: config.BotConfig{OpenPositionControl: config.OpenPositionControl{
		BotRiskControl: &config.BotRiskControl{Enabled: true, MaxPositionValue: 100},
	}, GridRiskControl: config.GridRiskControl{MaxGridLayers: 3}}, Inner: &SymbolRuntime{
		verifiedCapitalBudget: 500,
		OpeningGate:           gate,
		UpdateOpenControl: func(config.OpenPositionControl) error {
			return applyErr
		},
	}}
	err := br.SetRiskControls(&config.BotRiskControl{Enabled: true, MaxPositionValue: 400}, br.Config.GridRiskControl)
	if !errors.Is(err, applyErr) {
		t.Fatalf("SetRiskControls() error = %v, want strategy apply failure", err)
	}
	if got := br.GetBotRiskControl().MaxPositionValue; got != 100 {
		t.Fatalf("failed update leaked into runtime config: got %v, want previous value 100", got)
	}
	if !gate.HasBlock("risk_control_update_unverified") {
		t.Fatal("failed specialized update did not block new openings")
	}
	br.Inner.UpdateOpenControl = func(config.OpenPositionControl) error { return nil }
	if err := br.SetRiskControls(&config.BotRiskControl{Enabled: true, MaxPositionValue: 400}, br.Config.GridRiskControl); err != nil {
		t.Fatal(err)
	}
	if gate.HasBlock("risk_control_update_unverified") || br.GetBotRiskControl().MaxPositionValue != 400 {
		t.Fatal("successful verified retry did not publish and clear its own block")
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
