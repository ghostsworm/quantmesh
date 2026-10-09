package main

import (
	"os"
	"testing"

	"quantmesh/config"
	"quantmesh/storage"
	"quantmesh/web"
)

func TestReconstructedDurableStopActualAPIReportsPending(t *testing.T) {
	oldConfig, oldStore := web.GetConfig(), web.GetPrimaryStorageForAppConfig()
	t.Cleanup(func() {
		web.SetPrimaryStorageForAppConfig(oldStore)
		if oldConfig == nil {
			web.SetFileConfigManager(nil)
			return
		}
		fcm := web.NewFileConfigManager("")
		if err := fcm.SetRuntimeConfig(oldConfig); err != nil {
			t.Error(err)
		}
		web.SetFileConfigManager(fcm)
	})
	for _, mode := range []string{"incomplete", "complete", "unreadable", "absent"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			original := stopJournalProcessManager(t, dir)
			bot := config.BotConfig{ID: "owner", Exchange: "binance", Symbol: "BTCUSDT", MarketType: "futures"}
			cfg := &config.Config{Bots: []config.BotConfig{bot}, Exchanges: map[string]config.ExchangeConfig{"binance": {}}}
			fcm := web.NewFileConfigManager("")
			if err := fcm.SetRuntimeConfig(cfg); err != nil {
				t.Fatal(err)
			}
			web.SetFileConfigManager(fcm)
			web.SetPrimaryStorageForAppConfig(nil)
			if mode != "absent" {
				journal := &botStopJournal{State: &storage.BotState{BotID: bot.ID, Enabled: false}, Operation: "isolated-stop", Complete: mode == "complete"}
				if err := original.writeStopJournal(journal, true); err != nil {
					t.Fatal(err)
				}
				if mode == "unreadable" {
					// A directory at the expected journal path is unreadable as JSON.
					if err := os.Remove(original.stopJournalPath(bot.ID)); err != nil {
						t.Fatal(err)
					}
					if err := os.Mkdir(original.stopJournalPath(bot.ID), 0700); err != nil {
						t.Fatal(err)
					}
				}
			}
			fresh := stopJournalProcessManager(t, dir)
			adapter := &botManagerProviderAdapter{manager: &SymbolManager{botManager: fresh}}
			detail, ok := adapter.GetBot(bot.ID)
			pending := mode != "absent"
			if !ok || detail.Running || detail.StopPending != pending {
				t.Error("actual reconstructed detail hid durable stop status")
			}
			list := adapter.ListBots()
			if len(list) != 1 || list[0].Running || list[0].StopPending != pending {
				t.Error("actual reconstructed list hid durable stop status")
			}
			if mode == "complete" {
				if err := fresh.StopBot(bot.ID); err != nil {
					t.Fatal(err)
				}
				detail, ok = adapter.GetBot(bot.ID)
				if !ok || detail.StopPending || detail.Running {
					t.Fatal("retired completed journal remained pending")
				}
			} else if pending {
				if err := fresh.StopBot(bot.ID); err == nil {
					t.Fatal("unverified journal was guessed complete")
				}
				if err := fresh.EnableBot(bot.ID); err == nil {
					t.Fatal("pending journal enabled without reconciliation")
				}
			}
		})
	}
}
