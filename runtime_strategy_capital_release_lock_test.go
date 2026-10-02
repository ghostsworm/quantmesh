package main

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"quantmesh/execution"
	"quantmesh/web"
)

type capitalReleaseBlockingJournal struct {
	execution.IntentJournal
	block   atomic.Bool
	entered chan struct{}
}

func (j *capitalReleaseBlockingJournal) LoadExecutionIntents(ctx context.Context, scope string, after int64, limit int) ([]execution.IntentJournalRecord, error) {
	if j.block.Swap(false) {
		close(j.entered)
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return j.IntentJournal.LoadExecutionIntents(ctx, scope, after, limit)
}

func TestRuntimeStrategyCapitalReleaseOwnerLockDeadlinePreservesCapital(t *testing.T) {
	rt, venue, store := capitalReleaseRuntimeFixture(t)
	journal := &capitalReleaseBlockingJournal{IntentJournal: store, entered: make(chan struct{})}
	if err := rt.ExchangeExecutor.ConfigureIntentJournal(t.Context(), journal, rt.capitalReleaseScope); err != nil {
		t.Fatal(err)
	}
	journal.block.Store(true)
	holdCtx, cancelHold := context.WithCancel(t.Context())
	defer cancelHold()
	holder := make(chan error, 1)
	go func() { holder <- rt.ExchangeExecutor.VerifyCapitalReleaseIntents(holdCtx) }()
	select {
	case <-journal.entered:
	case <-time.After(time.Second):
		t.Fatal("fixture did not hold the real executor intent lock")
	}
	provider := newRuntimeStrategyCapitalProvider(nil, func() []*SymbolRuntime { return []*SymbolRuntime{rt} }).(web.VerifiedStrategyCapitalProvider)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
	defer cancel()
	type result struct {
		amount float64
		err    error
	}
	done := make(chan result, 1)
	go func() {
		amount, err := provider.ReleaseVerifiedCapital(ctx, "dca")
		done <- result{amount, err}
	}()
	var got result
	select {
	case got = <-done:
	case <-time.After(time.Second):
		cancelHold()
		got = <-done
		t.Error("production owner proof outlived request deadline while lock was held")
	}
	if !errors.Is(got.err, context.DeadlineExceeded) || got.amount != 0 || venue.lastReadSymbol != "" || rt.StrategyManager.GetCapitalAllocator().GetUsed("dca") != 200 {
		t.Errorf("blocked owner proof mutated or reached venue: amount=%v err=%v symbol=%s", got.amount, got.err, venue.lastReadSymbol)
	}
	cancelHold()
	if err := <-holder; !errors.Is(err, context.Canceled) {
		t.Fatalf("holder cancellation = %v", err)
	}
	amount, err := provider.ReleaseVerifiedCapital(t.Context(), "dca")
	if err != nil || amount != 200 || rt.StrategyManager.GetCapitalAllocator().GetUsed("dca") != 0 {
		t.Fatalf("production retry failed: amount=%v err=%v", amount, err)
	}
}
