package main

import (
	"context"
	"fmt"
	"testing"

	"quantmesh/config"
	"quantmesh/strategy"
)

func TestRuntimeStrategyCapitalReleaseRejectsInvisibleFuturesHedgeIntent(t *testing.T) {
	for _, name := range []string{"futures_long", "futures_short"} {
		t.Run(name, func(t *testing.T) {
			rt, venue, _ := capitalReleaseRuntimeFixture(t)
			cfg := &config.Config{}
			cfg.Trading.BotID, cfg.Trading.Symbol = rt.capitalReleaseScope.Bot, rt.capitalReleaseScope.Symbol
			var current strategy.Strategy
			if name == "futures_long" {
				current = strategy.NewFuturesLongStrategy(name, cfg, nil, nil, nil)
			} else {
				current = strategy.NewFuturesShortStrategy(name, cfg, nil, nil, nil)
			}
			clean := fmt.Sprintf(`{"bot_id":"a","strategy":%q,"group_id":"","symbol":"BTCUSDT"}`, name)
			dirty := fmt.Sprintf(`{"bot_id":"a","strategy":%q,"group_id":"","symbol":"BTCUSDT","pending":{"client_order_id":"prepared","side":"BUY","quantity":1,"executed_qty":0}}`, name)
			store := &capitalReleaseDebtStateStore{version: 1, found: true, payload: dirty}
			current.(interface {
				SetRuntimeStateStore(strategy.RuntimeStateStore)
			}).SetRuntimeStateStore(store)
			rt.StrategyManager.RegisterStrategy(name, current, 1, 0)
			if len(current.GetPositions()) != 0 || len(current.GetOrders()) != 0 {
				t.Fatal("fixture must omit durable intent from memory")
			}
			amounts, err := releaseRuntimeStrategyCapital(t.Context(), []*SymbolRuntime{rt}, "")
			if err == nil || amounts["dca"] != 0 || rt.StrategyManager.GetCapitalAllocator().GetUsed("dca") != 200 {
				t.Fatalf("durable hedge intent ignored: amounts=%v err=%v", amounts, err)
			}
			store.payload = clean
			venue.onRead = func(context.Context) { store.payload = dirty }
			amounts, err = releaseRuntimeStrategyCapital(t.Context(), []*SymbolRuntime{rt}, "dca")
			if err == nil || venue.lastReadSymbol != cfg.Trading.Symbol || amounts["dca"] != 0 || rt.StrategyManager.GetCapitalAllocator().GetUsed("dca") != 200 {
				t.Fatalf("new hedge state ignored: amounts=%v err=%v", amounts, err)
			}
			venue.onRead, store.payload = nil, clean
			amounts, err = releaseRuntimeStrategyCapital(t.Context(), []*SymbolRuntime{rt}, "")
			if err != nil || amounts["dca"] != 200 {
				t.Fatalf("clean hedge state blocked legal release: amounts=%v err=%v", amounts, err)
			}
		})
	}
}
