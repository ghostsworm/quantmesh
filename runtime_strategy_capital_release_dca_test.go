package main

import (
	"context"
	"testing"

	"quantmesh/config"
	"quantmesh/strategy"
)

func TestRuntimeStrategyCapitalReleaseRejectsInvisibleDCALayer(t *testing.T) {
	rt, venue, _ := capitalReleaseRuntimeFixture(t)
	cfg := &config.Config{}
	cfg.Trading.BotID, cfg.Trading.Symbol = rt.capitalReleaseScope.Bot, rt.capitalReleaseScope.Symbol
	dca := strategy.NewDCAEnhancedStrategy("dca", cfg.Trading.Symbol, cfg, nil, nil, nil)
	state := &capitalReleaseDebtStateStore{version: 2, found: true,
		payload: `{"bot_id":"a","strategy_name":"dca","symbol":"BTCUSDT","close_layer_index":-1,"layers":[{"Index":0,"ClientOrderID":"prepared","Status":"UNKNOWN","RequestedQuantity":1}],"current_layer":1}`}
	dca.SetRuntimeStateStore(state)
	rt.StrategyManager.RegisterStrategy("dca", dca, 1, 0)
	allocator := rt.StrategyManager.GetCapitalAllocator()
	allocator.Allocate()
	if !allocator.Reserve("dca", 200) {
		t.Fatal("real strategy fixture reserve failed")
	}
	if len(dca.GetPositions()) != 0 || len(dca.GetOrders()) != 0 {
		t.Fatal("fixture must omit durable layer from memory")
	}
	amounts, err := releaseRuntimeStrategyCapital(t.Context(), []*SymbolRuntime{rt}, "dca")
	if err == nil || amounts["dca"] != 0 || rt.StrategyManager.GetCapitalAllocator().GetUsed("dca") != 200 {
		t.Fatalf("durable DCA layer ignored: amounts=%v err=%v", amounts, err)
	}
	clean := `{"bot_id":"a","strategy_name":"dca","symbol":"BTCUSDT","close_layer_index":-1,"layers":[]}`
	state.payload = clean
	venue.onRead = func(context.Context) {
		state.payload = `{"bot_id":"a","strategy_name":"dca","symbol":"BTCUSDT","close_layer_index":-1,"layers":[],"close_client_order_id":"prepared","close_requested_qty":1,"close_limit_price":100}`
	}
	amounts, err = releaseRuntimeStrategyCapital(t.Context(), []*SymbolRuntime{rt}, "dca")
	if err == nil || venue.lastReadSymbol != cfg.Trading.Symbol || amounts["dca"] != 0 || rt.StrategyManager.GetCapitalAllocator().GetUsed("dca") != 200 {
		t.Fatalf("new durable DCA close ignored: amounts=%v err=%v", amounts, err)
	}
	venue.onRead, state.payload = nil, clean
	amounts, err = releaseRuntimeStrategyCapital(t.Context(), []*SymbolRuntime{rt}, "dca")
	if err != nil || amounts["dca"] != 200 {
		t.Fatalf("clean DCA state blocked legal release: amounts=%v err=%v", amounts, err)
	}
}
