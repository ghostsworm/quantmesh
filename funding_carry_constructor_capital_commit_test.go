package main

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"quantmesh/config"
	"quantmesh/storage"
)

var errConstructorCapitalCommitReply = errors.New("fixture capital SQL commit reply lost")
var errConstructorCapitalBeforeCommit = errors.New("fixture capital SQL release failed before commit")

var constructorCapitalCommitModes = []string{"capital_ack_error", "capital_cancelled_commit", "capital_before_commit_error", "capital_before_commit_cancel"}

type constructorCapitalCommitFailure struct {
	mode    string
	armed   atomic.Bool
	fired   atomic.Int32
	calls   atomic.Int32
	wire    *constructorMySQLWireFault
	wireErr error
}

func isConstructorCapitalCommitMode(mode string) bool { return strings.HasPrefix(mode, "capital_") }

func (f *constructorCapitalCommitFailure) release(ctx context.Context, store storage.AccountWalletCapitalReservationStore, botID string, claims []storage.AccountWalletCapitalClaim) error {
	f.calls.Add(1)
	if f.wire != nil && f.armed.Load() && f.fired.CompareAndSwap(0, 1) {
		f.wireErr = f.wire.release(ctx, botID, claims)
		return f.wireErr
	}
	sqlCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	if strings.HasPrefix(f.mode, "capital_before_commit_") && f.armed.Load() && f.fired.CompareAndSwap(0, 1) {
		if f.mode == "capital_before_commit_cancel" {
			cancel()
			return store.ReleaseAccountWalletCapital(sqlCtx, botID, claims)
		}
		return errConstructorCapitalBeforeCommit
	}
	if err := store.ReleaseAccountWalletCapital(sqlCtx, botID, claims); err != nil {
		return err
	}
	if !f.armed.Load() || !f.fired.CompareAndSwap(0, 1) {
		return nil
	}
	if f.mode == "capital_cancelled_commit" {
		cancel()
		return sqlCtx.Err()
	}
	return errConstructorCapitalCommitReply
}

func TestFundingCarryFullConstructorCapitalCommittedReplyFailureRecoversWithoutFinancialReplay(t *testing.T) {
	testFundingCarryConstructorFinalVerificationWithFaults(t, newEnableStateStorage, constructorCapitalCommitModes)
}

func TestMySQLFundingCarryFullConstructorCapitalCommittedReplyFailureRecoversWithoutFinancialReplay(t *testing.T) {
	constructorMySQLConfig(t)
	testFundingCarryConstructorFinalVerificationWithFaults(t, newConstructorMySQLStorage, constructorCapitalCommitModes)
}

func injectConstructorCapitalCommitFailure(t *testing.T, bm *BotManager, rt *SymbolRuntime, br *BotRuntime, store storage.StrategyRuntimeStateStore, account string, provider *runtimeLeaseTestLock, venues map[string]*constructorFinalVerificationVenue, stop func() error, original *string, fault *constructorCapitalCommitFailure) {
	t.Helper()
	fault.armed.Store(true)
	err := stop()
	want := errConstructorCapitalCommitReply
	if fault.wire != nil {
		want = fault.wireErr
		wantOK := int32(1)
		if fault.mode == "capital_wire_before_commit" {
			wantOK = 0
		}
		if want == nil || fault.wire.hits.Load() != 1 || fault.wire.commitOK.Load() != wantOK {
			t.Fatal("real driver COMMIT interruption was not observed", want)
		}
	} else if fault.mode == "capital_cancelled_commit" || fault.mode == "capital_before_commit_cancel" {
		want = context.Canceled
	} else if fault.mode == "capital_before_commit_error" {
		want = errConstructorCapitalBeforeCommit
	}
	marker := rt.shutdownCloseUnverified.Load()
	if !errors.Is(err, want) || !isRetryableRuntimeStopVerification(err) || !br.stopVerificationPending.Load() || marker == nil || marker == original || fault.fired.Load() != 1 || fault.calls.Load() != 1 {
		t.Fatalf("post-commit capital reply lost stage-specific evidence: %v", err)
	}
	if strings.Contains(err.Error(), "capital reservation retained") || strings.Contains(*marker, "capital reservation retained") || !strings.Contains(err.Error(), "capital release is unverified; runtime ownership release withheld") {
		t.Fatalf("unverified SQL outcome was reported as retained capital: %v", err)
	}
	claims, err := bm.storageService.GetStorage().(storage.AccountWalletCapitalReservationReader).ListAccountWalletCapitalReservations(t.Context(), "", "", 10)
	wantClaims := 0
	if strings.HasPrefix(fault.mode, "capital_before_commit_") || fault.mode == "capital_wire_before_commit" {
		wantClaims = 3
	}
	if err != nil || len(claims) != wantClaims {
		t.Fatalf("SQL outcome differs from injected phase: %v count=%d want=%d", err, len(claims), wantClaims)
	}
	before := assertConstructorFinalVerificationCheckpoint(t, store, account, false)
	for _, market := range []string{"futures", "spot", "spot_margin"} {
		key, err := runtimeOwnershipScope(account, "binance", market, "BTCUSDT").Key()
		if err != nil {
			t.Fatal(err)
		}
		provider.mu.Lock()
		held := provider.held["runtime-owner:"+key]
		provider.mu.Unlock()
		if !held {
			t.Fatalf("ambiguous capital release surrendered %s ownership", market)
		}
	}
	if err := bm.EnableBot(br.BotID); err == nil {
		t.Fatal("ambiguous capital release accepted enable")
	}
	if _, err := bm.StartBot(t.Context(), br.Config); err == nil {
		t.Fatal("unverified capital release accepted duplicate start")
	}
	report := bm.UpdateRuntimeTradingParamsWithReport(&config.Config{Bots: []config.BotConfig{br.Config}})
	if len(report.Applied) != 0 || report.Failed[br.BotID] != "bot_stop_verification_pending" {
		t.Fatal("unverified capital release accepted hot update", report)
	}
	adapter := &botManagerProviderAdapter{manager: &SymbolManager{botManager: bm}}
	if detail, ok := adapter.GetBot(br.BotID); !ok || detail.Running || !detail.StopPending {
		t.Fatal("actual status hid unverified capital release")
	}
	venues["spot_margin"].failFinal.Store(true)
	if err := stop(); !isRetryableRuntimeStopVerification(err) || rt.shutdownCloseUnverified.Load() != marker || fault.calls.Load() != 1 {
		t.Fatalf("missing fresh proof bypassed guard or rewrote marker: %v", err)
	}
	after := assertConstructorFinalVerificationCheckpoint(t, store, account, false)
	if before.Payload != after.Payload || before.SchemaVersion != after.SchemaVersion {
		t.Fatal("capital proof retry rewrote financial ledger")
	}
	venues["spot_margin"].failFinal.Store(false)
}
