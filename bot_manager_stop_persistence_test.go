package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"quantmesh/config"
	"quantmesh/event"
)

func TestStopPersistenceFailureCanRetryWithoutRepeatingShutdown(t *testing.T) {
	for _, batch := range []bool{false, true} {
		name := "StopBot"
		if batch {
			name = "StopAll"
		}
		t.Run(name, func(t *testing.T) {
			bm := NewBotManager(&config.Config{}, event.NewEventBus(8), nil, nil, "")
			bm.botStatesFileOverride = t.TempDir()
			calls := 0
			owner := config.BotConfig{ID: "owner", Exchange: "binance", Symbol: "BTCUSDT"}
			br := &BotRuntime{BotID: owner.ID, Config: owner, Inner: &SymbolRuntime{
				Config:        config.BotConfigToSymbolConfig(owner),
				StopWithError: func() error { calls++; return nil },
			}}
			bm.AddRuntime(br)
			if err := bm.StopBot(owner.ID); err == nil {
				t.Fatal("failed persistence reported success")
			}
			if retained, ok := bm.Get(owner.ID); !ok || retained != br {
				t.Fatal("pending stop released owner")
			}
			if err := bm.EnableBot(owner.ID); err == nil {
				t.Fatal("pending stop allowed enable")
			}
			if _, err := bm.StartBot(context.Background(), owner); err == nil {
				t.Fatal("pending stop reported successful start")
			}
			report := bm.UpdateRuntimeTradingParamsWithReport(&config.Config{Bots: []config.BotConfig{owner}})
			if len(report.Applied) != 0 || report.Failed[owner.ID] != "bot_stop_persistence_pending" {
				t.Fatal("stopped owner reported hot apply")
			}
			bm.botStatesFileOverride = filepath.Join(t.TempDir(), "states.json")
			var err error
			if batch {
				err = bm.StopAll()
			} else {
				err = bm.StopBot(owner.ID)
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(bm.botStatesFileOverride); err != nil {
				t.Fatal("retry did not persist state")
			}
			if enabled, reason := bm.IsBotEnabledInDB(owner.ID); enabled || reason != "from_file" {
				t.Fatal("durable retry not readable as stopped")
			}
			if calls != 1 {
				t.Fatal("persistence retry repeated financial shutdown")
			}
			if _, ok := bm.Get(owner.ID); ok {
				t.Fatal("durably stopped owner retained")
			}
		})
	}
}

func TestStopPrimaryFailureDoesNotReleaseOwnerViaFallback(t *testing.T) {
	bm := newEnableStateStorage(t)
	bm.eventBus = event.NewEventBus(8)
	if err := bm.EnableBot("owner"); err != nil {
		t.Fatal(err)
	}
	if err := bm.storageService.GetStorage().Close(); err != nil {
		t.Fatal(err)
	}
	owner := config.BotConfig{ID: "owner", Exchange: "binance", Symbol: "BTCUSDT"}
	bm.AddRuntime(&BotRuntime{BotID: owner.ID, Config: owner, Inner: &SymbolRuntime{Config: config.BotConfigToSymbolConfig(owner), StopWithError: func() error { return nil }}})
	if err := bm.StopBot(owner.ID); err == nil {
		t.Fatal("failed primary stop reported success")
	}
	if _, ok := bm.Get(owner.ID); !ok {
		t.Fatal("failed primary stop released owner")
	}
	if _, err := os.Stat(bm.botStatesFileOverride); !os.IsNotExist(err) {
		t.Fatal("primary failure wrote misleading fallback")
	}
}
