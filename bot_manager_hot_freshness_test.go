package main

import (
	"sync/atomic"
	"testing"
	"time"

	"quantmesh/config"
	"quantmesh/web"
)

func TestHotFreshnessCheckedAfterLifecycleWait(t *testing.T) {
	bm := NewBotManager(&config.Config{}, nil, nil, nil, "")
	owner := config.BotConfig{ID: "owner", Exchange: "binance", Symbol: "BTCUSDT"}
	var callbacks, checks atomic.Int32
	prechecked := make(chan struct{})
	var fresh atomic.Bool
	fresh.Store(true)
	bm.AddRuntime(&BotRuntime{BotID: owner.ID, Config: owner, Inner: &SymbolRuntime{
		Config:            config.BotConfigToSymbolConfig(owner),
		UpdateOpenControl: func(config.OpenPositionControl) error { callbacks.Add(1); return nil },
	}})
	unlock := bm.lockBotLifecycle(owner.ID)
	done := make(chan web.TradingParamsUpdateReport, 1)
	go func() {
		done <- bm.UpdateRuntimeTradingParamsWithGuardedReport(&config.Config{Bots: []config.BotConfig{owner}}, func() bool {
			checks.Add(1)
			if checks.Load() == 1 {
				close(prechecked)
			}
			return fresh.Load()
		})
	}()
	<-prechecked
	time.Sleep(30 * time.Millisecond)
	early := checks.Load()
	fresh.Store(false)
	unlock()
	report := <-done
	if early != 1 || checks.Load() != 2 || callbacks.Load() != 0 || len(report.Applied) != 0 || report.Failed[owner.ID] != "runtime_configuration_changed" {
		t.Fatalf("stale callback applied or checked before wait: early=%d checks=%d callbacks=%d report=%+v", early, checks.Load(), callbacks.Load(), report)
	}
}

func TestActualWebAdapterRejectsStaleAndAcceptsFreshHotSnapshot(t *testing.T) {
	bm := NewBotManager(&config.Config{}, nil, nil, nil, "")
	owner := config.BotConfig{ID: "owner", Exchange: "binance", Symbol: "BTCUSDT"}
	calls := 0
	br := &BotRuntime{BotID: owner.ID, Config: owner, Inner: &SymbolRuntime{
		Config:            config.BotConfigToSymbolConfig(owner),
		UpdateOpenControl: func(config.OpenPositionControl) error { calls++; return nil },
	}}
	bm.AddRuntime(br)
	adapter := &symbolManagerWebAdapter{manager: &SymbolManager{botManager: bm}}
	next := owner
	next.OpenPositionControl.PauseOpening = true
	cfg := &config.Config{Bots: []config.BotConfig{next}}
	report := adapter.UpdateTradingParamsWithGuardedReport(cfg, func() bool { return false })
	if calls != 0 || br.Config.OpenPositionControl.PauseOpening || len(report.Applied) != 0 || report.Failed[owner.ID] != "runtime_configuration_changed" {
		t.Fatalf("actual adapter dispatched stale snapshot: %+v", report)
	}
	report = adapter.UpdateTradingParamsWithGuardedReport(cfg, func() bool { return true })
	if calls != 1 || !br.Config.OpenPositionControl.PauseOpening || len(report.Applied) != 1 || len(report.Failed) != 0 {
		t.Fatalf("fresh snapshot rejected: %+v", report)
	}
}
