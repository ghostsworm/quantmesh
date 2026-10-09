package main

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"quantmesh/config"
	"quantmesh/event"
	"quantmesh/web"
)

func TestHotApplyWaitsForActualStopLifecycle(t *testing.T) {
	assertHotApplyWaitsForStop(t, func(bm *BotManager) error { return bm.StopBot("owner") })
}

func TestHotApplyWaitsForStopAllLifecycle(t *testing.T) {
	assertHotApplyWaitsForStop(t, func(bm *BotManager) error { return bm.StopAll() })
}

func assertHotApplyWaitsForStop(t *testing.T, stop func(*BotManager) error) {
	t.Helper()
	bm := NewBotManager(&config.Config{}, event.NewEventBus(8), nil, nil, "")
	bm.botStatesFileOverride = filepath.Join(t.TempDir(), "states.json")
	owner := config.BotConfig{ID: "owner", Exchange: "binance", Symbol: "BTCUSDT"}
	stopping, release := make(chan struct{}), make(chan struct{})
	applied := make(chan struct{}, 1)
	br := &BotRuntime{BotID: owner.ID, Config: owner, Inner: &SymbolRuntime{
		Config:            config.BotConfigToSymbolConfig(owner),
		StopWithError:     func() error { close(stopping); <-release; return nil },
		UpdateOpenControl: func(config.OpenPositionControl) error { applied <- struct{}{}; return nil },
	}}
	bm.AddRuntime(br)
	stopped := make(chan error, 1)
	go func() { stopped <- stop(bm) }()
	<-stopping
	updated := make(chan web.TradingParamsUpdateReport, 1)
	go func() {
		updated <- bm.UpdateRuntimeTradingParamsWithReport(&config.Config{Bots: []config.BotConfig{owner}})
	}()
	overlapped := false
	select {
	case <-applied:
		overlapped = true
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	if err := <-stopped; err != nil {
		t.Fatal(err)
	}
	report := <-updated
	if overlapped {
		t.Fatalf("hot callback overlapped actual StopBot lifecycle; applied=%v failed=%v", report.Applied, report.Failed)
	}
	if len(report.Applied) != 0 || len(report.NotRunning) != 1 || report.NotRunning[0] != owner.ID {
		t.Fatalf("stopped runtime incorrectly reported as applied: %+v", report)
	}
}

func TestHotApplicationAdmittedUntilCallbackDrained(t *testing.T) {
	bm := NewBotManager(&config.Config{}, nil, nil, nil, "")
	owner := config.BotConfig{ID: "owner", Exchange: "binance", Symbol: "BTCUSDT"}
	entered, release := make(chan struct{}), make(chan struct{})
	bm.AddRuntime(&BotRuntime{BotID: owner.ID, Config: owner, Inner: &SymbolRuntime{
		Config:            config.BotConfigToSymbolConfig(owner),
		UpdateOpenControl: func(config.OpenPositionControl) error { close(entered); <-release; return nil },
	}})
	done := make(chan web.TradingParamsUpdateReport, 1)
	go func() {
		done <- bm.UpdateRuntimeTradingParamsWithReport(&config.Config{Bots: []config.BotConfig{owner}})
	}()
	<-entered
	bm.runtimeAdmissions.Block("shutdown")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	err := bm.runtimeAdmissions.Drain(ctx)
	close(release)
	report := <-done
	if err == nil || len(report.Applied) != 1 {
		t.Fatalf("shutdown failed to track admitted callback: drain=%v report=%+v", err, report)
	}
	ctx, cancel = context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := bm.runtimeAdmissions.Drain(ctx); err != nil {
		t.Fatal(err)
	}
	report = bm.UpdateRuntimeTradingParamsWithReport(&config.Config{Bots: []config.BotConfig{owner}})
	if len(report.Applied) != 0 || report.Failed[owner.ID] != "runtime_lifecycle_unavailable" {
		t.Fatalf("shutdown admitted a new update: %+v", report)
	}
}

func TestActualStopWaitsForHotApplication(t *testing.T) {
	bm := NewBotManager(&config.Config{}, event.NewEventBus(8), nil, nil, "")
	bm.botStatesFileOverride = filepath.Join(t.TempDir(), "states.json")
	owner := config.BotConfig{ID: "owner", Exchange: "binance", Symbol: "BTCUSDT"}
	applying, release := make(chan struct{}), make(chan struct{})
	stopping := make(chan struct{}, 1)
	br := &BotRuntime{BotID: owner.ID, Config: owner, Inner: &SymbolRuntime{
		Config: config.BotConfigToSymbolConfig(owner),
		UpdateOpenControl: func(config.OpenPositionControl) error {
			// Manager lookups remain available while the lifecycle lock is held.
			if bm.findConflictingRuntime(&owner) == nil {
				t.Error("registered runtime disappeared during callback")
			}
			close(applying)
			<-release
			return nil
		},
		StopWithError: func() error { stopping <- struct{}{}; return nil },
	}}
	bm.AddRuntime(br)
	updated := make(chan web.TradingParamsUpdateReport, 1)
	go func() {
		updated <- bm.UpdateRuntimeTradingParamsWithReport(&config.Config{Bots: []config.BotConfig{owner}})
	}()
	<-applying
	stopped := make(chan error, 1)
	go func() { stopped <- bm.StopBot(owner.ID) }()
	overlapped := false
	select {
	case <-stopping:
		overlapped = true
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	report := <-updated
	if err := <-stopped; err != nil {
		t.Fatal(err)
	}
	if overlapped || len(report.Applied) != 1 {
		t.Fatalf("stop overlapped an admitted update: overlapped=%v report=%+v", overlapped, report)
	}
}
