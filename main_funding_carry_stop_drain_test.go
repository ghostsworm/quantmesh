package main

import (
	"context"
	"errors"
	"testing"

	"quantmesh/config"
	"quantmesh/event"
	"quantmesh/lock"
	"quantmesh/order"
	"quantmesh/strategy"
)

func TestActualBotStopsRetryFundingCarryDrainWithoutPoisoningUnknown(t *testing.T) {
	for _, all := range []bool{false, true} {
		t.Run(map[bool]string{false: "StopBot", true: "StopAll"}[all], func(t *testing.T) {
			bm := newEnableStateStorage(t)
			bm.eventBus = event.NewEventBus(8)
			t.Cleanup(bm.eventBus.Close)
			br := journalTestOwner(bm, nil)
			carry := strategy.NewFundingCarryStrategy("owner", nil, config.SymbolConfig{Symbol: "BTCUSDT"}, nil, nil, nil, nil)
			executor := order.NewExchangeOrderExecutor(nil, "BTCUSDT", 0, 0, lock.NewNopLock(), "")
			calls, financial := 0, 0
			br.Inner.StopWithError = func() error {
				calls++
				ctx := t.Context()
				if calls == 1 {
					cancelled, cancel := context.WithCancel(ctx)
					cancel()
					ctx = cancelled
				}
				if err := drainFundingCarryStop(ctx, carry, []*order.ExchangeOrderExecutor{executor}, nil); err != nil {
					return err
				}
				financial++ // No account/financial RPC: manager continuation boundary only.
				return nil
			}
			stop := func() error { return bm.StopBot("owner") }
			if all {
				stop = bm.StopAll
			}
			if err := stop(); !isRetryableRuntimeStopDrain(err) || !errors.Is(err, context.Canceled) {
				t.Fatalf("first stop = %v", err)
			}
			if financial != 0 || !br.stopTransitionPending() || !br.stopDrainPending.Load() {
				t.Fatal("drain failure entered financial phase or lost pending state")
			}
			if br.Inner.shutdownCloseUnverifiedReason() != "" {
				t.Fatal("pure waiting failure poisoned financial UNKNOWN")
			}
			if _, ok := bm.Get("owner"); !ok {
				t.Fatal("drain failure lost controller")
			}
			if err := bm.EnableBot("owner"); err == nil {
				t.Fatal("enable accepted unfinished drain")
			}
			if _, err := bm.StartBot(t.Context(), br.Config); err == nil {
				t.Fatal("duplicate start accepted unfinished drain")
			}
			hot := 0
			br.Inner.UpdateOpenControl = func(config.OpenPositionControl) error { hot++; return nil }
			report := bm.UpdateRuntimeTradingParamsWithReport(&config.Config{Bots: []config.BotConfig{br.Config}})
			if hot != 0 || len(report.Applied) != 0 || report.Failed["owner"] != "bot_stop_drain_pending" {
				t.Fatalf("hot application accepted unfinished drain: hot=%d report=%+v pending=%t", hot, report, br.stopDrainPending.Load())
			}
			if err := stop(); err != nil {
				t.Fatal(err)
			}
			if calls != 2 || financial != 1 {
				t.Fatal("retry did not cross boundary exactly once")
			}
			if _, ok := bm.Get("owner"); ok {
				t.Fatal("successful retry retained controller")
			}
		})
	}
}

func TestFundingCarryDrainOwnershipLossSupersedesWaitRetry(t *testing.T) {
	for _, cancelled := range []bool{false, true} {
		t.Run(map[bool]string{false: "after_successful_drain", true: "during_cancelled_wait"}[cancelled], func(t *testing.T) {
			carry := strategy.NewFundingCarryStrategy("owner", nil, config.SymbolConfig{Symbol: "BTCUSDT"}, nil, nil, nil, nil)
			executor := order.NewExchangeOrderExecutor(nil, "BTCUSDT", 0, 0, lock.NewNopLock(), "")
			ctx := t.Context()
			if cancelled {
				cctx, cancel := context.WithCancel(ctx)
				cancel()
				ctx = cctx
			}
			lease := &runtimeOwnershipLease{}
			checks := 0
			err := drainFundingCarryStop(ctx, carry, []*order.ExchangeOrderExecutor{executor}, func() error {
				checks++
				if checks > 1 {
					lease.lost.Store(true)
				}
				return fundingCarryStopDrainOwnershipGuard([]*runtimeOwnershipLease{lease})
			})
			if !errors.Is(err, errFundingCarryDrainOwnershipLost) || isRetryableRuntimeStopDrain(err) {
				t.Fatalf("lost ownership admitted financial continuation or wait retry: %v", err)
			}
		})
	}
}
