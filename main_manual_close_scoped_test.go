package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"quantmesh/web"
)

func TestScopedManualCloseCancellationReturnsWhileLifecycleStillLocked(t *testing.T) {
	sm := newManualManager()
	f := &shutdownFaultExchange{fakeCloseExchange: newFakeCloseExchange(100, 0)}
	newManualRuntime(t, sm, "target", "account", f)
	a := &symbolManagerWebAdapter{manager: sm, ctx: t.Context()}
	unlock := sm.botManager.lockBotLifecycle("target")
	ctx, cancel := context.WithTimeout(t.Context(), 25*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := a.ClosePositionsScoped(ctx, "binance", "BTCUSDT", "futures", "target")
		done <- err
	}()
	select {
	case err := <-done:
		unlock()
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("deadline lost: %v", err)
		}
	case <-time.After(250 * time.Millisecond):
		unlock()
		<-done
		t.Fatal("canceled close remained blocked on lifecycle lock")
	}
	if f.positionQueries != 0 || f.submissions != 0 {
		t.Fatal("expired lock waiter reached venue")
	}
}

func TestScopedManualCloseProcessCancellationInterruptsLifecycleWait(t *testing.T) {
	sm := newManualManager()
	f := &shutdownFaultExchange{fakeCloseExchange: newFakeCloseExchange(100, 0)}
	newManualRuntime(t, sm, "target", "account", f)
	processCtx, stop := context.WithCancel(t.Context())
	defer stop()
	a := &symbolManagerWebAdapter{manager: sm, ctx: processCtx}
	unlock := sm.botManager.lockBotLifecycle("target")
	done := make(chan error, 1)
	go func() {
		_, err := a.ClosePositionsScoped(t.Context(), "binance", "BTCUSDT", "futures", "target")
		done <- err
	}()
	stop()
	select {
	case err := <-done:
		unlock()
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("process cancellation lost: %v", err)
		}
	case <-time.After(250 * time.Millisecond):
		unlock()
		<-done
		t.Fatal("process cancellation retained lifecycle waiter")
	}
	if f.positionQueries != 0 || f.submissions != 0 {
		t.Fatal("process cancellation reached venue")
	}
}

func TestScopedManualCloseRejectsAmbiguousMarketsAndBotMismatchBeforeRPC(t *testing.T) {
	sm := newManualManager()
	futures := &shutdownFaultExchange{fakeCloseExchange: newFakeCloseExchange(100, 1)}
	spot := &shutdownFaultExchange{fakeCloseExchange: newFakeCloseExchange(100, 1)}
	newManualRuntime(t, sm, "futures-bot", "account-f", futures)
	rt := newShutdownRuntime(spot, "spot-bot", "account-s")
	rt.Config.Exchange, rt.Config.MarketType = "binance", "spot"
	sm.Add(rt)
	a := &symbolManagerWebAdapter{manager: sm, ctx: t.Context()}
	if _, err := a.ClosePositionsScoped(t.Context(), "binance", "BTCUSDT", "", ""); !errors.Is(err, web.ErrManualCloseScopeAmbiguous) {
		t.Fatalf("ambiguous request guessed market: %v", err)
	}
	if _, err := a.ClosePositionsScoped(t.Context(), "binance", "BTCUSDT", "spot", "futures-bot"); !errors.Is(err, web.ErrManualCloseScopeUnavailable) {
		t.Fatalf("bot/market mismatch accepted: %v", err)
	}
	if futures.positionQueries != 0 || spot.positionQueries != 0 || futures.submissions != 0 || spot.submissions != 0 {
		t.Fatal("invalid scope reached financial RPC")
	}
}

func TestScopedManualCloseExactMarketAndBotUsesOnlyTarget(t *testing.T) {
	sm := newManualManager()
	futures := &shutdownFaultExchange{fakeCloseExchange: newFakeCloseExchange(100, 1)}
	spot := &shutdownFaultExchange{fakeCloseExchange: newFakeCloseExchange(100, 1)}
	newManualRuntime(t, sm, "futures-bot", "account-f", futures)
	rt := newShutdownRuntime(spot, "spot-bot", "account-s")
	rt.Config.Exchange, rt.Config.MarketType = "binance", "spot"
	sm.Add(rt)
	a := &symbolManagerWebAdapter{manager: sm, ctx: t.Context()}
	result, err := a.ClosePositionsScoped(t.Context(), "binance", "BTCUSDT", "spot", "spot-bot")
	// Unowned inventory is deliberately rejected by the original financial gate.
	if err == nil || result != nil || spot.positionQueries == 0 || futures.positionQueries != 0 || spot.submissions != 0 || futures.submissions != 0 {
		t.Fatal("selected wrong target or bypassed original ownership gate")
	}
}

func TestScopedManualCloseRequestCancellationAndShutdownDoNotReachVenue(t *testing.T) {
	sm := newManualManager()
	f := &shutdownFaultExchange{fakeCloseExchange: newFakeCloseExchange(100, 1)}
	newManualRuntime(t, sm, "target", "account", f)
	a := &symbolManagerWebAdapter{manager: sm, ctx: t.Context()}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := a.ClosePositionsScoped(ctx, "binance", "BTCUSDT", "futures", "target"); !errors.Is(err, context.Canceled) {
		t.Fatal("request cancellation lost")
	}
	a.ctx = ctx
	if _, err := a.ClosePositionsScoped(t.Context(), "binance", "BTCUSDT", "futures", "target"); !errors.Is(err, context.Canceled) {
		t.Fatal("process cancellation lost")
	}
	a.ctx = t.Context()
	sm.botManager.runtimeAdmissions.Block("shutdown")
	if _, err := a.ClosePositionsScoped(t.Context(), "binance", "BTCUSDT", "futures", "target"); err == nil {
		t.Fatal("shutdown admission ignored")
	}
	if f.positionQueries != 0 || f.submissions != 0 {
		t.Fatal("canceled/refused close reached venue")
	}
}

func TestScopedManualCloseSameMarketMultipleAccountsCannotGuessOwner(t *testing.T) {
	sm := newManualManager()
	f := &shutdownFaultExchange{fakeCloseExchange: newFakeCloseExchange(100, 1)}
	newManualRuntime(t, sm, "account-one-bot", "account-one", f)
	newManualRuntime(t, sm, "account-two-bot", "account-two", f)
	a := &symbolManagerWebAdapter{manager: sm, ctx: t.Context()}
	if _, err := a.ClosePositionsScoped(t.Context(), "binance", "BTCUSDT", "futures", ""); !errors.Is(err, web.ErrManualCloseScopeAmbiguous) || f.positionQueries != 0 {
		t.Fatal("same-market account ambiguity reached venue")
	}
}

func TestScopedManualCloseConfirmedFlatTargetRemainsAvailable(t *testing.T) {
	sm := newManualManager()
	f := &shutdownFaultExchange{fakeCloseExchange: newFakeCloseExchange(100, 0)}
	newManualRuntime(t, sm, "target", "account", f)
	a := &symbolManagerWebAdapter{manager: sm, ctx: t.Context()}
	result, err := a.ClosePositionsScoped(t.Context(), "binance", "BTCUSDT", "futures", "target")
	if err != nil || result == nil || result.FailCount != 0 || f.submissions != 0 {
		t.Fatalf("confirmed flat target rejected: %v", err)
	}
}
