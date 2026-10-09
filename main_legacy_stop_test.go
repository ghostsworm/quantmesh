package main

import (
	"errors"
	"testing"

	"quantmesh/config"
	"quantmesh/event"
	"quantmesh/web"
)

func TestLegacyStopPropagatesFailureWithoutDuplicateShutdown(t *testing.T) {
	bm := newEnableStateStorage(t)
	bm.eventBus = event.NewEventBus(8)
	oldConfig, oldStore := web.GetConfig(), web.GetPrimaryStorageForAppConfig()
	fcm := web.NewFileConfigManager("")
	cfg := &config.Config{Exchanges: map[string]config.ExchangeConfig{"binance": {}}}
	cfg.Trading.Symbols = []config.SymbolConfig{{Exchange: "binance", Symbol: "BTCUSDT", MarketType: "futures"}}
	if err := fcm.SetRuntimeConfig(cfg); err != nil {
		t.Fatal(err)
	}
	web.SetFileConfigManager(fcm)
	web.SetPrimaryStorageForAppConfig(bm.storageService.GetStorage())
	t.Cleanup(func() {
		web.SetPrimaryStorageForAppConfig(oldStore)
		if oldConfig == nil {
			web.SetFileConfigManager(nil)
			return
		}
		restored := web.NewFileConfigManager("")
		if err := restored.SetRuntimeConfig(oldConfig); err != nil {
			t.Error(err)
		}
		web.SetFileConfigManager(restored)
	})
	calls := 0
	sc := cfg.Trading.Symbols[0]
	bc := config.SymbolConfigToBotConfig(sc, false)
	bc.ID = config.GenerateBotID(sc.Exchange, sc.Symbol, sc.GetMarketType())
	br := &BotRuntime{BotID: bc.ID, Config: bc, Inner: &SymbolRuntime{Config: sc,
		Stop:          func() { calls++ },
		StopWithError: func() error { calls++; return errors.New("fixture stop terminal unknown") },
	}}
	bm.AddRuntime(br)
	adapter := &symbolManagerWebAdapter{manager: &SymbolManager{botManager: bm}, cfg: cfg, eventBus: bm.eventBus}
	if err := adapter.StopSymbol(sc.Exchange, sc.Symbol); err == nil {
		t.Fatal("legacy adapter swallowed shutdown failure")
	}
	if calls != 1 {
		t.Fatal("legacy adapter performed duplicate financial shutdown")
	}
	if retained, ok := bm.Get(bc.ID); !ok || retained != br {
		t.Fatal("unknown shutdown released owner")
	}
	// A recovered terminal verifier must allow the same public path to finish.
	br.Inner.StopWithError = func() error { calls++; return nil }
	if err := adapter.StopSymbol(sc.Exchange, sc.Symbol); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatal("successful retry duplicated shutdown")
	}
	if _, ok := bm.Get(bc.ID); ok {
		t.Fatal("verified durable stop retained owner")
	}
	state, err := bm.storageService.GetStorage().GetBotState(bc.ID)
	if err != nil || state == nil || state.Enabled {
		t.Fatal("successful public stop lacks disabled primary readback")
	}
}
