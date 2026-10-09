package main

import (
	"context"
	"errors"
	"sync"
	"testing"

	"quantmesh/config"
	"quantmesh/event"
)

func TestFundingCarryFinalStopVerificationPreservesOtherFailures(t *testing.T) {
	for _, mode := range []string{"verified", "cancelled", "proof_failure", "existing_failure", "changed_same_text", "changed_during_proof", "lost_before", "lost_during_proof", "cancel_during_proof"} {
		t.Run(mode, func(t *testing.T) {
			rt := &SymbolRuntime{}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			lost, calls := false, 0
			v := &fundingCarryFinalStopVerification{rt: rt, ownershipGuard: func() error {
				if lost {
					return errors.New("fixture owner lost")
				}
				return nil
			}, verify: func(context.Context) error {
				calls++
				switch mode {
				case "proof_failure":
					return errors.New("fixture liability query failed")
				case "changed_during_proof":
					rt.markShutdownCloseUnverified("other fault")
				case "lost_during_proof":
					lost = true
				case "cancel_during_proof":
					cancel()
				}
				return nil
			}}
			if mode == "existing_failure" {
				rt.markShutdownCloseUnverified("existing fault")
			}
			// This unit fixture models the post-classification boundary only;
			// it does not manufacture a strategy's private candidate error.
			recorded := v.recordReason("final verification failed")
			if recorded == (mode == "existing_failure") {
				t.Fatal("record replaced other failure or failed to claim empty slot")
			}
			original := rt.shutdownCloseUnverified.Load()
			switch mode {
			case "cancelled":
				cancel()
			case "changed_same_text":
				rt.markShutdownCloseUnverified(*original)
			case "lost_before":
				lost = true
			}
			err := v.reconcile(ctx)
			if mode == "verified" {
				if err != nil || rt.shutdownCloseUnverified.Load() != nil || calls != 1 {
					t.Fatal("complete readonly proof did not clear exactly owned failure", err)
				}
				return
			}
			if err == nil || rt.shutdownCloseUnverified.Load() == nil {
				t.Fatal("failed proof cleared stop failure")
			}
			wantRetry := mode == "cancelled" || mode == "proof_failure" || mode == "cancel_during_proof"
			if isRetryableRuntimeStopVerification(err) != wantRetry {
				t.Fatalf("retry classification incorrect: %v", err)
			}
			if mode == "changed_same_text" && rt.shutdownCloseUnverified.Load() == original {
				t.Fatal("same text did not model distinct failure pointer")
			}
			if (mode == "existing_failure" || mode == "changed_same_text" || mode == "lost_before" || mode == "cancelled") && calls != 0 {
				t.Fatal("invalid prerequisite invoked verifier")
			}
		})
	}
}

func TestActualBotStopRetainsFinalVerificationPending(t *testing.T) {
	for _, all := range []bool{false, true} {
		t.Run(map[bool]string{false: "StopBot", true: "StopAll"}[all], func(t *testing.T) {
			bm := newEnableStateStorage(t)
			bm.eventBus = event.NewEventBus(8)
			t.Cleanup(bm.eventBus.Close)
			br := journalTestOwner(bm, nil)
			financial, proofs, capital, hot := 0, 0, 0, 0
			available := false
			v := &fundingCarryFinalStopVerification{rt: br.Inner, ownershipGuard: func() error { return nil }, verify: func(context.Context) error {
				proofs++
				if !available {
					return errors.New("fixture liability unavailable")
				}
				return nil
			}}
			var once sync.Once
			br.Inner.StopWithError = func() error {
				first := false
				once.Do(func() {
					first = true
					financial++ // Phase boundary fixture, not a real financial RPC.
					if !v.recordReason("fixture completed close needs verification") {
						t.Fatal("failed to record candidate boundary")
					}
				})
				if first {
					return v.pending(errors.New("fixture completed close needs verification"))
				}
				if err := v.reconcile(t.Context()); err != nil {
					return err
				}
				capital++ // Independent release boundary, not a SQL release proof.
				return nil
			}
			br.Inner.UpdateOpenControl = func(config.OpenPositionControl) error { hot++; return nil }
			stop := func() error { return bm.StopBot("owner") }
			if all {
				stop = bm.StopAll
			}
			if err := stop(); !isRetryableRuntimeStopVerification(err) || !br.stopVerificationPending.Load() || !br.stopTransitionPending() {
				t.Fatal("actual manager lost final verification pending", err)
			}
			original := br.Inner.shutdownCloseUnverified.Load()
			if err := stop(); !isRetryableRuntimeStopVerification(err) || br.Inner.shutdownCloseUnverified.Load() != original || capital != 0 {
				t.Fatal("failed readonly retry changed source or released capital", err)
			}
			if err := bm.EnableBot("owner"); err == nil {
				t.Fatal("pending verification accepted enable")
			}
			if _, err := bm.StartBot(t.Context(), br.Config); err == nil {
				t.Fatal("pending verification accepted duplicate start")
			}
			report := bm.UpdateRuntimeTradingParamsWithReport(&config.Config{Bots: []config.BotConfig{br.Config}})
			if hot != 0 || len(report.Applied) != 0 || report.Failed["owner"] != "bot_stop_verification_pending" {
				t.Fatal("pending verification accepted hot update")
			}
			adapter := &botManagerProviderAdapter{manager: &SymbolManager{botManager: bm}}
			detail, ok := adapter.GetBot("owner")
			if !ok || !detail.StopPending || detail.Running {
				t.Fatal("actual status adapter hides pending verification")
			}
			available = true
			if err := adapter.StopBot("owner"); err != nil {
				t.Fatal(err)
			}
			if financial != 1 || proofs != 2 || capital != 1 {
				t.Fatal("readonly retry repeated financial phase or premature release")
			}
			if _, ok := bm.Get("owner"); ok {
				t.Fatal("successful retry retained runtime")
			}
		})
	}
}
