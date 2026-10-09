package main

import (
	"context"
	"testing"
	"time"

	"quantmesh/config"
	"quantmesh/web"
)

type hotContextReportProvider interface {
	UpdateTradingParamsWithContext(context.Context, *config.Config, func() bool) web.TradingParamsUpdateReport
}

func TestHotApplicationCancelledBeforeDispatchAndFreshRequestRetained(t *testing.T) {
	bm := NewBotManager(&config.Config{}, nil, nil, nil, "")
	owner := config.BotConfig{ID: "owner", Exchange: "binance", Symbol: "BTCUSDT"}
	calls := 0
	br := &BotRuntime{BotID: owner.ID, Config: owner, Inner: &SymbolRuntime{Config: config.BotConfigToSymbolConfig(owner), UpdateOpenControl: func(config.OpenPositionControl) error { calls++; return nil }}}
	bm.AddRuntime(br)
	cfg := &config.Config{Bots: []config.BotConfig{owner}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	report := bm.UpdateRuntimeTradingParamsWithContext(ctx, cfg, nil)
	if calls != 0 || len(report.Applied) != 0 || report.Failed[owner.ID] != "runtime_application_cancelled" {
		t.Fatal("cancelled request applied")
	}
	report = bm.UpdateRuntimeTradingParamsWithContext(context.Background(), cfg, nil)
	if calls != 1 || len(report.Applied) != 1 || len(report.Failed) != 0 {
		t.Fatal("fresh request lost normal application")
	}
}

func TestHotApplicationCancellationDuringFreshnessCheck(t *testing.T) {
	bm := NewBotManager(&config.Config{}, nil, nil, nil, "")
	owner := config.BotConfig{ID: "owner", Exchange: "binance", Symbol: "BTCUSDT"}
	calls, checks := 0, 0
	bm.AddRuntime(&BotRuntime{BotID: owner.ID, Config: owner, Inner: &SymbolRuntime{Config: config.BotConfigToSymbolConfig(owner), UpdateOpenControl: func(config.OpenPositionControl) error { calls++; return nil }}})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	report := bm.UpdateRuntimeTradingParamsWithContext(ctx, &config.Config{Bots: []config.BotConfig{owner}}, func() bool {
		checks++
		if checks == 2 {
			cancel()
		}
		return true
	})
	if calls != 0 || len(report.Applied) != 0 || report.Failed[owner.ID] != "runtime_application_cancelled" {
		t.Fatalf("post-check cancellation dispatched: %+v", report)
	}
}

func TestActualHotAdapterCancellationWhileLifecycleLocked(t *testing.T) {
	bm := NewBotManager(&config.Config{}, nil, nil, nil, "")
	owner := config.BotConfig{ID: "owner", Exchange: "binance", Symbol: "BTCUSDT"}
	calls := 0
	bm.AddRuntime(&BotRuntime{BotID: owner.ID, Config: owner, Inner: &SymbolRuntime{
		Config:            config.BotConfigToSymbolConfig(owner),
		UpdateOpenControl: func(config.OpenPositionControl) error { calls++; return nil },
	}})
	adapter := &symbolManagerWebAdapter{manager: &SymbolManager{botManager: bm}}
	unlock := bm.lockBotLifecycle(owner.ID)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	done := make(chan web.TradingParamsUpdateReport, 1)
	go func() {
		cfg := &config.Config{Bots: []config.BotConfig{owner}}
		if provider, ok := any(adapter).(hotContextReportProvider); ok {
			done <- provider.UpdateTradingParamsWithContext(ctx, cfg, func() bool { return true })
		} else {
			done <- adapter.UpdateTradingParamsWithGuardedReport(cfg, func() bool { return ctx.Err() == nil })
		}
	}()
	var report web.TradingParamsUpdateReport
	returned := false
	select {
	case report = <-done:
		returned = true
	case <-time.After(200 * time.Millisecond):
	}
	unlock()
	if !returned {
		report = <-done
	}
	if !returned || calls != 0 || len(report.Applied) != 0 || report.Failed[owner.ID] != "runtime_application_cancelled" {
		t.Fatalf("cancelled hot update waited for released lifecycle or applied: returned=%v calls=%d report=%+v", returned, calls, report)
	}
}
