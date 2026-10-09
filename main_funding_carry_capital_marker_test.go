package main

import (
	"errors"
	"testing"
	"time"
)

func TestFundingCarryCapitalRetryOwnsOnlyOriginalFailure(t *testing.T) {
	for _, mode := range []string{"recovered", "same_text_replaced", "externally_cleared", "lost_before", "lost_during", "changed_during"} {
		t.Run(mode, func(t *testing.T) {
			provider := &runtimeLeaseTestLock{}
			lease, err := acquireRuntimeOwnershipLease(t.Context(), provider, runtimeOwnershipScope("fixture", "binance", "futures", "BTCUSDT"), time.Second, nil)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(lease.stopRenew)
			rt := &SymbolRuntime{}
			calls := 0
			release := newFundingCarryVerifiedRelease(rt, []*runtimeOwnershipLease{lease}, func() error {
				calls++
				if calls == 1 {
					return errors.New("fixture capital query failed")
				}
				if mode == "lost_during" {
					lease.lost.Store(true)
				}
				if mode == "changed_during" {
					reason := "other failure during proof"
					rt.shutdownCloseUnverified.Store(&reason)
				}
				return nil
			})
			if err := release(); err == nil || !isRetryableRuntimeStopVerification(err) {
				t.Fatal("missing capital retry classification", err)
			}
			original := rt.shutdownCloseUnverified.Load()
			if original == nil {
				t.Fatal("capital marker absent")
			}
			switch mode {
			case "same_text_replaced":
				reason := *original
				rt.shutdownCloseUnverified.Store(&reason)
			case "externally_cleared":
				rt.shutdownCloseUnverified.Store(nil)
			case "lost_before":
				lease.lost.Store(true)
			}
			err = release()
			if mode == "recovered" {
				if err != nil || rt.shutdownCloseUnverified.Load() != nil || !lease.released.Load() || calls != 2 {
					t.Fatal("owned proof failed to complete", err)
				}
				if err := release(); err != nil || calls != 2 {
					t.Fatal("completed release queried peer scope", err)
				}
				return
			}
			if err == nil || isRetryableRuntimeStopVerification(err) || lease.released.Load() {
				t.Fatal("superseded or lost proof released ownership", err)
			}
			if (mode == "same_text_replaced" || mode == "externally_cleared" || mode == "lost_before") && calls != 1 {
				t.Fatal("invalid proof reached capital callback")
			}
			if mode == "same_text_replaced" && rt.shutdownCloseUnverified.Load() == original {
				t.Fatal("other equal-text failure overwritten")
			}
			if mode == "changed_during" && *rt.shutdownCloseUnverified.Load() != "other failure during proof" {
				t.Fatal("proof erased other failure")
			}
		})
	}
}
