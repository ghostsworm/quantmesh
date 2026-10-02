package main

import (
	"context"
	"testing"

	"quantmesh/config"
	"quantmesh/strategy"
)

func TestRuntimeStrategyCapitalReleaseRejectsInvisibleMartingaleEntry(t *testing.T) {
	rt, _, _ := capitalReleaseRuntimeFixture(t)
	cfg := &config.Config{}
	cfg.Trading.BotID, cfg.Trading.Symbol = rt.capitalReleaseScope.Bot, rt.capitalReleaseScope.Symbol
	martin := strategy.NewMartingaleStrategy("martingale", cfg.Trading.Symbol, cfg, nil, nil, nil)
	state := &capitalReleaseDebtStateStore{version: 1, found: true,
		payload: `{"bot_id":"a","strategy_name":"martingale","symbol":"BTCUSDT","direction":"LONG","entries":[{"Level":0,"ClientOrderID":"prepared","Status":"UNKNOWN","RequestedQuantity":1}],"current_level":1}`}
	martin.SetRuntimeStateStore(state)
	rt.StrategyManager.RegisterStrategy("martingale", martin, 1, 0)
	if len(martin.GetPositions()) != 0 || len(martin.GetOrders()) != 0 {
		t.Fatal("fixture must omit durable entry from generic snapshots")
	}
	amounts, err := releaseRuntimeStrategyCapital(t.Context(), []*SymbolRuntime{rt}, "")
	if err == nil || amounts["dca"] != 0 || rt.StrategyManager.GetCapitalAllocator().GetUsed("dca") != 200 {
		t.Fatalf("durable Martingale entry ignored: amounts=%v err=%v", amounts, err)
	}
	state.payload = `{"bot_id":"a","strategy_name":"martingale","symbol":"BTCUSDT","direction":"LONG","entries":[]}`
	amounts, err = releaseRuntimeStrategyCapital(t.Context(), []*SymbolRuntime{rt}, "")
	if err != nil || amounts["dca"] != 200 {
		t.Fatalf("reconciled Martingale state blocked legal release: amounts=%v err=%v", amounts, err)
	}
}

func TestRuntimeStrategyCapitalReleaseRechecksMartingaleAfterVenueRead(t *testing.T) {
	rt, venue, _ := capitalReleaseRuntimeFixture(t)
	cfg := &config.Config{}
	cfg.Trading.BotID, cfg.Trading.Symbol = rt.capitalReleaseScope.Bot, rt.capitalReleaseScope.Symbol
	martin := strategy.NewMartingaleStrategy("martingale", cfg.Trading.Symbol, cfg, nil, nil, nil)
	state := &capitalReleaseDebtStateStore{version: 1, found: true,
		payload: `{"bot_id":"a","strategy_name":"martingale","symbol":"BTCUSDT","direction":"LONG","entries":[]}`}
	martin.SetRuntimeStateStore(state)
	rt.StrategyManager.RegisterStrategy("martingale", martin, 1, 0)
	venue.onRead = func(context.Context) {
		state.payload = `{"bot_id":"a","strategy_name":"martingale","symbol":"BTCUSDT","direction":"LONG","entries":[],"close_client_order_id":"prepared","close_reason":"stop_loss","close_requested_qty":1}`
	}
	amounts, err := releaseRuntimeStrategyCapital(t.Context(), []*SymbolRuntime{rt}, "dca")
	if err == nil || venue.lastReadSymbol != cfg.Trading.Symbol || amounts["dca"] != 0 || rt.StrategyManager.GetCapitalAllocator().GetUsed("dca") != 200 {
		t.Fatalf("new durable close state ignored: amounts=%v err=%v", amounts, err)
	}
}
