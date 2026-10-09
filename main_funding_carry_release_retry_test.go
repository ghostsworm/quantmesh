package main

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"quantmesh/event"
)

type fundingCarryReleaseTestLock struct {
	*retryRuntimeLeaseLock
	failedKey     atomic.Pointer[string]
	failedRenewed chan struct{}
}

func (l *fundingCarryReleaseTestLock) Unlock(ctx context.Context, key string) error {
	if l.unlockCalls.Load() == 0 {
		l.failedKey.Store(&key)
	}
	return l.retryRuntimeLeaseLock.Unlock(ctx, key)
}

func (l *fundingCarryReleaseTestLock) Extend(ctx context.Context, key string, ttl time.Duration) error {
	err := l.retryRuntimeLeaseLock.Extend(ctx, key, ttl)
	failed := l.failedKey.Load()
	if err == nil && failed != nil && *failed == key {
		select {
		case l.failedRenewed <- struct{}{}:
		default:
		}
	}
	return err
}

func TestFundingCarryManagedStopRetriesOwnershipRelease(t *testing.T) {
	for _, all := range []bool{false, true} {
		t.Run(map[bool]string{false: "StopBot", true: "StopAll"}[all], func(t *testing.T) {
			bm := newEnableStateStorage(t)
			bm.eventBus = event.NewEventBus(8)
			t.Cleanup(bm.eventBus.Close)
			br := journalTestOwner(bm, nil)
			provider := &fundingCarryReleaseTestLock{retryRuntimeLeaseLock: &retryRuntimeLeaseLock{runtimeLeaseTestLock: &runtimeLeaseTestLock{}, renewed: make(chan struct{}, 8)}, failedRenewed: make(chan struct{}, 8)}
			var leases []*runtimeOwnershipLease
			for _, market := range []string{"futures", "spot", "spot_margin"} {
				lease, err := acquireRuntimeOwnershipLease(t.Context(), provider, runtimeOwnershipScope("fixture", "binance", market, "BTCUSDT"), 150*time.Millisecond, nil)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(lease.stopRenew)
				leases = append(leases, lease)
			}
			var once sync.Once
			var financial atomic.Int32
			var capital atomic.Int32
			releaseVerified := newFundingCarryVerifiedRelease(br.Inner, leases, func() error { capital.Add(1); return nil })
			br.Inner.StopWithError = func() error {
				once.Do(func() { financial.Add(1) })
				return releaseVerified()
			}
			stop := func() error {
				if all {
					return bm.StopAll()
				}
				return bm.StopBot("owner")
			}
			if err := stop(); err == nil {
				t.Fatal("unlock failure reported successful stop")
			}
			if br.Inner.shutdownCloseUnverifiedReason() != "" || !br.stopOwnershipPending.Load() {
				t.Fatal("FundingCarry lease-only failure poisoned financial state or lost pending release")
			}
			for i, market := range []string{"futures", "spot", "spot_margin"} {
				peer, err := acquireRuntimeOwnershipLease(t.Context(), provider, runtimeOwnershipScope("fixture", "binance", market, "BTCUSDT"), time.Second, nil)
				if !leases[i].released.Load() {
					if err == nil {
						t.Fatal("peer acquired unresolved lease")
					}
					continue
				}
				if err != nil {
					t.Fatal("peer cannot acquire verified released leg", err)
				}
				t.Cleanup(func() { _ = peer.Release() })
			}
			journal, err := bm.readStopJournal("owner")
			wantMode := botStopJournalModeDisable
			if all {
				wantMode = botStopJournalModeShutdown
			}
			if err != nil || journal == nil || journal.Complete || journal.Mode != wantMode {
				t.Fatal("partial release lost its durable pending stop mode")
			}
			select {
			case <-provider.failedRenewed:
			case <-time.After(time.Second):
				t.Fatal("failed FundingCarry lease stopped renewing")
			}
			if err := stop(); err != nil {
				t.Fatal(err)
			}
			if financial.Load() != 1 || capital.Load() != 1 || provider.unlockCalls.Load() != 4 {
				t.Fatal("retry replayed financial actions or already released leases")
			}
			if _, exists := bm.Get("owner"); exists {
				t.Fatal("verified retry retained controller")
			}
			journal, err = bm.readStopJournal("owner")
			if err != nil || journal != nil {
				t.Fatal("verified retry failed to retire durable stop journal")
			}
			if !all {
				state, err := bm.storageService.GetStorage().GetBotState("owner")
				if err != nil || state == nil || state.Enabled {
					t.Fatal("retry did not persist disabled primary state")
				}
			} else if enabled, _ := bm.IsBotEnabledInDB("owner"); !enabled {
				t.Fatal("verified StopAll unexpectedly disabled Bot")
			}
		})
	}
}

func TestFundingCarryVerifiedReleaseRefusesUnverifiedState(t *testing.T) {
	for _, mode := range []string{"unknown", "lost", "capital_failure", "lost_during_capital"} {
		t.Run(mode, func(t *testing.T) {
			provider := &retryRuntimeLeaseLock{runtimeLeaseTestLock: &runtimeLeaseTestLock{}, renewed: make(chan struct{}, 8)}
			lease, err := acquireRuntimeOwnershipLease(t.Context(), provider, runtimeOwnershipScope("fixture", "binance", "futures", "BTCUSDT"), time.Second, nil)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(lease.stopRenew)
			rt := &SymbolRuntime{}
			if mode == "unknown" {
				rt.markShutdownCloseUnverified("fixture UNKNOWN")
			}
			if mode == "lost" {
				lease.lost.Store(true)
			}
			capitalCalls := 0
			release := newFundingCarryVerifiedRelease(rt, []*runtimeOwnershipLease{lease}, func() error {
				capitalCalls++
				if mode == "capital_failure" {
					return errors.New("fixture capital release unverified")
				}
				if mode == "lost_during_capital" {
					lease.lost.Store(true)
				}
				return nil
			})
			if err := release(); err == nil || isRetryableRuntimeOwnershipRelease(err) {
				t.Fatal("unverified release classified as benign retry")
			}
			if provider.unlockCalls.Load() != 0 {
				t.Fatal("unverified state released ownership")
			}
			if (mode == "unknown" || mode == "lost") && capitalCalls != 0 {
				t.Fatal("unverified state reached capital release")
			}
		})
	}
}
