package main

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"quantmesh/event"
)

func TestActualBotStopRetriesLeaseReleaseWithoutRepeatingFinancialStop(t *testing.T) {
	bm := newEnableStateStorage(t)
	bm.eventBus = event.NewEventBus(8)
	t.Cleanup(bm.eventBus.Close)
	provider := &retryRuntimeLeaseLock{runtimeLeaseTestLock: &runtimeLeaseTestLock{}, renewed: make(chan struct{}, 8)}
	lease, err := acquireRuntimeOwnershipLease(t.Context(), provider, runtimeOwnershipScope("fixture", "binance", "futures", "BTCUSDT"), time.Second, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { lease.stopRenew() })
	var financialCalls atomic.Int32
	br := journalTestOwner(bm, nil)
	br.Inner.StopWithError = newStandardRuntimeStop(br.Inner, lease, func() error { financialCalls.Add(1); return nil })
	if err := bm.StopBot("owner"); err == nil {
		t.Fatal("unlock failure reported stop success")
	}
	journal, err := bm.readStopJournal("owner")
	if err != nil || journal == nil || journal.Complete {
		t.Fatal("unreleased lease marked durable stop complete")
	}
	if _, ok := bm.Get("owner"); !ok {
		t.Fatal("unreleased lease lost controller ownership")
	}
	if br.Inner.shutdownCloseUnverifiedReason() != "" {
		t.Fatal("lease-only failure poisoned verified financial state")
	}
	if err := bm.StopBot("owner"); err != nil {
		t.Fatal(err)
	}
	if financialCalls.Load() != 1 || provider.unlockCalls.Load() != 2 {
		t.Fatal("lease retry replayed financial stop or failed to retry unlock")
	}
	if _, ok := bm.Get("owner"); ok {
		t.Fatal("confirmed stop retained controller")
	}
	state, err := bm.storageService.GetStorage().GetBotState("owner")
	if err != nil || state == nil || state.Enabled {
		t.Fatal("confirmed retry not disabled in primary")
	}
	if journal, err := bm.readStopJournal("owner"); err != nil || journal != nil {
		t.Fatal("confirmed retry did not retire intent")
	}
}

func TestRuntimeStopPhasesNeverReplayUnknownFinancialResult(t *testing.T) {
	cause := errors.New("fixture financial UNKNOWN")
	var financial, releases atomic.Int32
	stop := newRuntimeStopWithRetryableRelease(func() error { financial.Add(1); return cause }, func() error { releases.Add(1); return nil })
	for range 3 {
		if err := stop(); !errors.Is(err, cause) {
			t.Fatal("financial UNKNOWN lost")
		}
	}
	if financial.Load() != 1 || releases.Load() != 0 {
		t.Fatal("UNKNOWN replayed or released ownership")
	}
}

func TestActualStopAllCanRetryLeaseOnlyFailure(t *testing.T) {
	bm := newEnableStateStorage(t)
	provider := &retryRuntimeLeaseLock{runtimeLeaseTestLock: &runtimeLeaseTestLock{}, renewed: make(chan struct{}, 8)}
	lease, err := acquireRuntimeOwnershipLease(t.Context(), provider, runtimeOwnershipScope("fixture", "binance", "futures", "BTCUSDT"), time.Second, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(lease.stopRenew)
	var financial atomic.Int32
	br := journalTestOwner(bm, nil)
	br.Inner.StopWithError = newStandardRuntimeStop(br.Inner, lease, func() error { financial.Add(1); return nil })
	if err := bm.StopAll(); err == nil {
		t.Fatal("unconfirmed lease release reported successful StopAll")
	}
	if br.Inner.shutdownCloseUnverifiedReason() != "" {
		t.Fatal("StopAll poisoned financial phase after lease-only failure")
	}
	if _, ok := bm.Get("owner"); !ok {
		t.Fatal("failed StopAll lost controller")
	}
	if err := bm.StopAll(); err != nil {
		t.Fatal(err)
	}
	if _, ok := bm.Get("owner"); ok {
		t.Fatal("confirmed StopAll retained controller")
	}
	if financial.Load() != 1 || provider.unlockCalls.Load() != 2 {
		t.Fatal("StopAll repeated financial phase or skipped release retry")
	}
}

func TestStandardRuntimeStopNeverLabelsLostOwnershipRetryable(t *testing.T) {
	provider := &runtimeLeaseTestLock{}
	lease, err := acquireRuntimeOwnershipLease(t.Context(), provider, runtimeOwnershipScope("fixture", "binance", "futures", "BTCUSDT"), time.Second, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		lease.stopRenew()
		if err := lease.Release(); err != nil {
			t.Error(err)
		}
	})
	lease.lost.Store(true)
	var financial atomic.Int32
	stop := newStandardRuntimeStop(&SymbolRuntime{}, lease, func() error { financial.Add(1); return nil })
	for range 2 {
		err := stop()
		if err == nil || isRetryableRuntimeOwnershipRelease(err) {
			t.Fatal("lost ownership treated as retryable lease RPC failure")
		}
	}
	if financial.Load() != 1 {
		t.Fatal("lost ownership replayed financial phase")
	}
}

func TestRuntimeStopPhasesSerializeConcurrentReleaseRetries(t *testing.T) {
	var financial, releases atomic.Int32
	stop := newRuntimeStopWithRetryableRelease(func() error { financial.Add(1); return nil }, func() error {
		if releases.Add(1) == 1 {
			return errors.New("fixture first release unknown")
		}
		return nil
	})
	var wait sync.WaitGroup
	for range 16 {
		wait.Add(1)
		go func() { defer wait.Done(); _ = stop() }()
	}
	wait.Wait()
	if financial.Load() != 1 || releases.Load() != 2 {
		t.Fatal("concurrent stops duplicated financial/release phases")
	}
}
