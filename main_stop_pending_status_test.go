package main

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"
	"time"

	"quantmesh/config"
	"quantmesh/event"
	"quantmesh/web"
)

func TestLeasePendingActualStopRejectsStartHotAndReportsPending(t *testing.T) {
	for _, all := range []bool{false, true} {
		name := "StopBot"
		if all {
			name = "StopAll"
		}
		t.Run(name, func(t *testing.T) {
			bm := newEnableStateStorage(t)
			bm.eventBus = event.NewEventBus(8)
			t.Cleanup(bm.eventBus.Close)
			br := journalTestOwner(bm, nil)
			cfg := &config.Config{Bots: []config.BotConfig{br.Config}, Exchanges: map[string]config.ExchangeConfig{"binance": {}}}
			oldConfig, oldStore := web.GetConfig(), web.GetPrimaryStorageForAppConfig()
			fcm := web.NewFileConfigManager("")
			if err := fcm.SetRuntimeConfig(cfg); err != nil {
				t.Fatal(err)
			}
			web.SetFileConfigManager(fcm)
			web.SetPrimaryStorageForAppConfig(nil)
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
			provider := &retryRuntimeLeaseLock{runtimeLeaseTestLock: &runtimeLeaseTestLock{}, renewed: make(chan struct{}, 8)}
			lease, err := acquireRuntimeOwnershipLease(t.Context(), provider, runtimeOwnershipScope("fixture", "binance", "futures", "BTCUSDT"), time.Second, nil)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(lease.stopRenew)
			var financial, hot atomic.Int32
			br.Inner.StopWithError = newStandardRuntimeStop(br.Inner, lease, func() error { financial.Add(1); return nil })
			br.Inner.UpdateOpenControl = func(config.OpenPositionControl) error { hot.Add(1); return nil }
			stop := func() error {
				if all {
					return bm.StopAll()
				}
				return bm.StopBot("owner")
			}
			if err := stop(); err == nil {
				t.Fatal("fixture release failure missing")
			}
			if _, err := bm.StartBot(context.Background(), br.Config); err == nil {
				t.Error("pending stop accepted duplicate start")
			}
			if err := bm.EnableBot("owner"); err == nil {
				t.Error("pending stop accepted explicit enable")
			}
			report := bm.UpdateRuntimeTradingParamsWithReport(cfg)
			if len(report.Applied) != 0 || report.Failed["owner"] != "bot_stop_ownership_pending" || hot.Load() != 0 {
				t.Errorf("pending stop accepted hot application: applied=%v reason=%s calls=%d", report.Applied, report.Failed["owner"], hot.Load())
			}
			adapter := &botManagerProviderAdapter{manager: &SymbolManager{botManager: bm}}
			detail, ok := adapter.GetBot("owner")
			if !ok || detail.Running {
				t.Error("actual detail reports stopped financial runtime running")
			}
			responses := adapter.ListBots()
			if len(responses) != 1 || responses[0].Running || !responses[0].StopPending {
				t.Error("actual list reports stopped financial runtime running")
			}
			data, err := json.Marshal(detail)
			if err != nil {
				t.Fatal(err)
			}
			var fields map[string]any
			if err := json.Unmarshal(data, &fields); err != nil {
				t.Fatal(err)
			}
			if fields["stop_pending"] != true {
				t.Error("response hides retained pending stop")
			}
			// The UI retry always uses the Bot endpoint, even if the original
			// transition was process-level StopAll. Do not repeat financial work.
			if err := adapter.StopBot("owner"); err != nil {
				t.Fatal(err)
			}
			if financial.Load() != 1 || provider.unlockCalls.Load() != 2 {
				t.Fatal("pending status broke lease-only retry")
			}
			state, err := bm.storageService.GetStorage().GetBotState("owner")
			if err != nil || state == nil || state.Enabled {
				t.Fatal("Bot API retry did not persist disabled primary state")
			}
		})
	}
}
