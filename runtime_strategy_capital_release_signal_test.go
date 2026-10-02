package main

import (
	"context"
	"fmt"
	"testing"

	"quantmesh/config"
	"quantmesh/strategy"
)

func TestRuntimeStrategyCapitalReleaseRejectsInvisibleSignalInventory(t *testing.T) {
	for _, name := range []string{"trend", "mean_reversion", "momentum"} {
		t.Run(name, func(t *testing.T) {
			rt, venue, _ := capitalReleaseRuntimeFixture(t)
			cfg := &config.Config{}
			cfg.Trading.BotID, cfg.Trading.Symbol = rt.capitalReleaseScope.Bot, rt.capitalReleaseScope.Symbol
			state := &capitalReleaseDebtStateStore{version: 1, found: true,
				payload: fmt.Sprintf(`{"bot_id":"a","strategy_name":%q,"symbol":"BTCUSDT","entry_price":100,"position":{"Symbol":"BTCUSDT","Size":1,"EntryPrice":100}}`, name)}
			var current strategy.Strategy
			switch name {
			case "trend":
				s := strategy.NewTrendFollowingStrategy(name, cfg, nil, nil, nil)
				s.SetRuntimeStateStore(state)
				current = s
			case "mean_reversion":
				s := strategy.NewMeanReversionStrategy(name, cfg, nil, nil, nil)
				s.SetRuntimeStateStore(state)
				current = s
			case "momentum":
				s := strategy.NewMomentumStrategy(name, cfg, nil, nil, nil)
				s.SetRuntimeStateStore(state)
				current = s
			}
			rt.StrategyManager.RegisterStrategy(name, current, 1, 0)
			if len(current.GetPositions()) != 0 || len(current.GetOrders()) != 0 {
				t.Fatal("fixture must omit durable inventory from memory")
			}
			amounts, err := releaseRuntimeStrategyCapital(t.Context(), []*SymbolRuntime{rt}, "")
			if err == nil || amounts["dca"] != 0 || rt.StrategyManager.GetCapitalAllocator().GetUsed("dca") != 200 {
				t.Fatalf("durable signal inventory ignored: amounts=%v err=%v", amounts, err)
			}
			state.payload = fmt.Sprintf(`{"bot_id":"a","strategy_name":%q,"symbol":"BTCUSDT"}`, name)
			// First read is flat; introduce durable order recovery evidence while
			// reading the venue. The second owner proof must observe it.
			venue.onRead = func(context.Context) {
				state.payload = fmt.Sprintf(`{"bot_id":"a","strategy_name":%q,"symbol":"BTCUSDT","pending_action":"open_long","active_order":{"OrderID":17,"ClientOrderID":"pending","Symbol":"BTCUSDT","Side":"BUY","Quantity":1,"Price":100}}`, name)
			}
			amounts, err = releaseRuntimeStrategyCapital(t.Context(), []*SymbolRuntime{rt}, "dca")
			if err == nil || venue.lastReadSymbol != cfg.Trading.Symbol || amounts["dca"] != 0 || rt.StrategyManager.GetCapitalAllocator().GetUsed("dca") != 200 {
				t.Fatalf("new durable signal order ignored: amounts=%v err=%v", amounts, err)
			}
			venue.onRead = nil
			state.payload = fmt.Sprintf(`{"bot_id":"a","strategy_name":%q,"symbol":"BTCUSDT"}`, name)
			amounts, err = releaseRuntimeStrategyCapital(t.Context(), []*SymbolRuntime{rt}, "")
			if err != nil || amounts["dca"] != 200 {
				t.Fatalf("clean signal state blocked legal release: amounts=%v err=%v", amounts, err)
			}
		})
	}
}
