package main

import (
	"testing"

	"quantmesh/config"
	"quantmesh/strategy"
)

func TestRuntimeStrategyCapitalReleaseRejectsInvisibleSpotLongIntent(t *testing.T) {
	venue := &capitalReleaseSpotVenue{capitalReleaseRuntimeVenue: &capitalReleaseRuntimeVenue{runtimeJournalVenue: &runtimeJournalVenue{}}}
	rt, _ := capitalReleaseRuntimeFixtureWithVenue(t, venue)
	cfg := &config.Config{}
	cfg.Trading.BotID, cfg.Trading.Symbol = rt.capitalReleaseScope.Bot, rt.capitalReleaseScope.Symbol
	long := strategy.NewSpotLongStrategy("spot_long", cfg, nil, nil, nil)
	state := &capitalReleaseDebtStateStore{version: 2, found: true,
		payload: `{"bot_id":"a","strategy":"spot_long","group_id":"","symbol":"BTCUSDT","base_asset":"BTC","pending_orders":{},"pending_intents":{"pending":{"side":"BUY","quantity":1,"created_at_unix_milli":1}}}`}
	long.SetRuntimeStateStore(state)
	rt.StrategyManager.RegisterStrategy("spot_long", long, 1, 0)
	if len(long.GetPositions()) != 0 || len(long.GetOrders()) != 0 {
		t.Fatal("fixture must omit persisted intent from generic snapshots")
	}
	amounts, err := releaseRuntimeStrategyCapital(t.Context(), []*SymbolRuntime{rt}, "")
	if err == nil || amounts["dca"] != 0 || rt.StrategyManager.GetCapitalAllocator().GetUsed("dca") != 200 {
		t.Fatalf("durable SpotLong intent ignored: amounts=%v err=%v", amounts, err)
	}
	// Completed recovery fixture, not an implementation of debt/order recovery.
	state.payload = `{"bot_id":"a","strategy":"spot_long","group_id":"","symbol":"BTCUSDT","base_asset":"BTC","pending_orders":{},"pending_intents":{}}`
	amounts, err = releaseRuntimeStrategyCapital(t.Context(), []*SymbolRuntime{rt}, "")
	if err != nil || amounts["dca"] != 200 {
		t.Fatalf("clean durable state prevented stale capital recovery: amounts=%v err=%v", amounts, err)
	}
}

func TestRuntimeStrategyCapitalReleaseRechecksSpotLongAfterVenueRead(t *testing.T) {
	venue := &capitalReleaseSpotVenue{capitalReleaseRuntimeVenue: &capitalReleaseRuntimeVenue{runtimeJournalVenue: &runtimeJournalVenue{}}}
	rt, _ := capitalReleaseRuntimeFixtureWithVenue(t, venue)
	cfg := &config.Config{}
	cfg.Trading.BotID, cfg.Trading.Symbol = rt.capitalReleaseScope.Bot, rt.capitalReleaseScope.Symbol
	long := strategy.NewSpotLongStrategy("spot_long", cfg, nil, nil, nil)
	state := &capitalReleaseDebtStateStore{version: 2, found: true,
		payload: `{"bot_id":"a","strategy":"spot_long","group_id":"","symbol":"BTCUSDT","base_asset":"BTC","pending_orders":{},"pending_intents":{}}`}
	long.SetRuntimeStateStore(state)
	rt.StrategyManager.RegisterStrategy("spot_long", long, 1, 0)
	venue.onInventory = func() {
		state.payload = `{"bot_id":"a","strategy":"spot_long","group_id":"","symbol":"BTCUSDT","base_asset":"BTC","pending_orders":{"17":{"side":"BUY","quantity":1,"executed_qty":0}},"pending_intents":{}}`
	}
	amounts, err := releaseRuntimeStrategyCapital(t.Context(), []*SymbolRuntime{rt}, "dca")
	if err == nil || venue.inventoryCalls != 1 || amounts["dca"] != 0 || rt.StrategyManager.GetCapitalAllocator().GetUsed("dca") != 200 {
		t.Fatalf("SpotLong durable state not rechecked: calls=%d amounts=%v err=%v", venue.inventoryCalls, amounts, err)
	}
}
