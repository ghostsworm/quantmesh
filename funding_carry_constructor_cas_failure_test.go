package main

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"quantmesh/storage"
	"quantmesh/strategy"
)

// The adapter really commits to SQL before hiding its successful reply. Only
// the SQL subrequest is cancelled; the outer verification context stays live.
type constructorCommittedCASFailure struct {
	*strategyRuntimeStateAdapter
	mode      string
	fired     atomic.Int32
	savedSame atomic.Int32
}

func (s *constructorCommittedCASFailure) CompareAndSwapRuntimeState(ctx context.Context, name string, version int, payload string, nextVersion int, nextPayload string) (bool, error) {
	sqlCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	saved, err := s.strategyRuntimeStateAdapter.CompareAndSwapRuntimeState(sqlCtx, name, version, payload, nextVersion, nextPayload)
	if err == nil && saved && version == nextVersion && payload == nextPayload {
		s.savedSame.Add(1)
	}
	if err != nil || !saved || !s.fired.CompareAndSwap(0, 1) {
		return saved, err
	}
	if s.mode == "cancelled_commit" {
		cancel()
		return false, sqlCtx.Err()
	}
	return false, errors.New("fixture SQL commit acknowledged internally but reply lost")
}

func TestFundingCarryFullConstructorCommittedCASFailureRecoversWithoutFinancialReplay(t *testing.T) {
	testFundingCarryConstructorFinalVerificationWithFaults(t, newEnableStateStorage, []string{"ack_error", "cancelled_commit"})
}

func injectConstructorCommittedCASFailure(t *testing.T, bm *BotManager, rt *SymbolRuntime, br *BotRuntime, store storage.StrategyRuntimeStateStore, account string, provider *runtimeLeaseTestLock, stop func() error, original *string, mode string) *constructorCommittedCASFailure {
	t.Helper()
	carry, ok := rt.StrategyManager.GetStrategy("funding_carry").(*strategy.FundingCarryStrategy)
	if !ok {
		t.Fatal("actual constructor carry strategy missing")
	}
	if bm.cfg.Storage.Type == "mysql" {
		freezeConstructorMySQLDiagnosticClock(t, bm)
	}
	fault := &constructorCommittedCASFailure{strategyRuntimeStateAdapter: &strategyRuntimeStateAdapter{storageService: bm.storageService, botID: "owner"}, mode: mode}
	// Install only after the financial stop has finished and drained producers.
	carry.SetRuntimeStateStore(fault)
	err := stop()
	if !isRetryableRuntimeStopVerification(err) || fault.fired.Load() != 1 || !br.stopVerificationPending.Load() || rt.shutdownCloseUnverified.Load() != original {
		t.Fatalf("committed SQL ambiguity lost retained original stop evidence: %v", err)
	}
	if mode == "cancelled_commit" && !errors.Is(err, context.Canceled) {
		t.Fatalf("SQL subrequest cancellation not propagated: %v", err)
	}
	clean := assertConstructorFinalVerificationCheckpoint(t, store, account, false)
	assertConstructorRecoveryEvidence(t, bm.storageService, "owner", clean.Payload)
	for _, market := range []string{"futures", "spot", "spot_margin"} {
		key, err := runtimeOwnershipScope(account, "binance", market, "BTCUSDT").Key()
		if err != nil {
			t.Fatal(err)
		}
		provider.mu.Lock()
		held := provider.held["runtime-owner:"+key]
		provider.mu.Unlock()
		if !held {
			t.Fatalf("committed reply failure surrendered %s ownership", market)
		}
	}
	if fault.savedSame.Load() != 0 {
		t.Fatal("first injected write was not a distinct pending-to-clean transition")
	}
	return fault
}
