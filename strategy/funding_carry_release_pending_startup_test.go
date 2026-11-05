package strategy

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"quantmesh/accounting"
	"quantmesh/config"
	"quantmesh/execution"
	"quantmesh/storage"
)

func TestFundingCarryReleasePendingStartupRetainsOnlyStoppedRuntime(t *testing.T) {
	for _, restart := range []bool{false, true} {
		name := "current_owner"
		if restart {
			name = "new_runtime_generation"
		}
		t.Run(name, func(t *testing.T) {
			fixture := newFundingCarryRecoveryHashIntegrationFixture(t, false)
			fixture.runtimeStore.provider.repository.(*fundingCarryRecoveryHashStore).failFirst = false
			var required *FundingCarryDurableRecoveryRequiredError
			if err := fixture.strategy.reconcileDurableFundingCarryRecovery(fixture.ctx); !errors.As(err, &required) {
				t.Fatalf("complete original projection while retaining hold: %v", err)
			}
			episode, found, err := fixture.store.GetRecoveryEpisode(fixture.ctx, fixture.episode.ID)
			if err != nil || !found {
				t.Fatalf("read projected episode: found=%t err=%v", found, err)
			}
			hold, found, err := fixture.store.GetRecoveryHold(fixture.ctx, episode.ID, episode.RecoveryGeneration)
			if err != nil || !found {
				t.Fatalf("read original hold: found=%t err=%v", found, err)
			}
			outbox, found, err := fixture.store.GetRecoveryOutbox(fixture.ctx, episode.ID, fixture.operationID)
			if err != nil || !found {
				t.Fatalf("read completed outbox: found=%t err=%v", found, err)
			}
			auth := accounting.RecoveryReleaseAuthorization{
				EpisodeID: episode.ID, OperationID: "release-before-restart", Actor: "operator", AuditRef: "startup-release-test",
				EvidenceHash: outbox.ProjectionHash, ExposureClearanceHash: outbox.ProjectionHash,
				ProjectionHash: outbox.ProjectionHash, RuntimeRevision: outbox.ExpectedRuntimeRevision,
				OwnerGeneration: episode.OwnerGeneration, ExpectedEpisodeRevision: episode.Revision,
				RecoveryGeneration: episode.RecoveryGeneration, ExpectedHoldRevision: hold.Revision,
				Confirmed: true, ExposureClearanceConfirmed: true,
			}
			pending, err := fixture.store.PrepareRecoveryRelease(fixture.ctx, fixture.runtimeStore.provider.generation, auth)
			if err != nil || pending.State != accounting.RecoveryReleasePending {
				t.Fatalf("commit release preparation: state=%s err=%v", pending.State, err)
			}
			receipt, found, err := fixture.store.GetRecoveryReceipt(fixture.ctx, episode.ID, auth.OperationID)
			if err != nil || !found || receipt.State != accounting.RecoveryReleasePending {
				t.Fatalf("read durable release receipt: found=%t err=%v", found, err)
			}
			if restart {
				generation, err := fixture.store.ClaimFundingCarryRuntimeGeneration(fixture.ctx, episode.AccountScopes)
				if err != nil {
					t.Fatal("claim restarted runtime generation:", err)
				}
				fixture.runtimeStore.provider.generation = generation
			}
			futures := &durableRecoveryFinancialRPCSpy{mockFCExchange: &mockFCExchange{name: "binance", marketType: "futures"}}
			spot := &durableRecoveryFinancialRPCSpy{mockFCExchange: &mockFCExchange{name: "binance", marketType: "spot"}}
			carry := NewFundingCarryStrategy("funding_carry", nil, config.SymbolConfig{Symbol: "BTCUSDT"}, futures, spot, nil, nil)
			carry.SetRuntimeStateStore(fixture.runtimeStore)
			gate := &execution.OpeningGate{}
			gate.Block("manual_operator_hold")
			carry.SetOpeningGate(gate)
			beforePayload, beforeCAS, beforeMarks := fixture.runtime.payload, fixture.runtime.compareCalls, fixture.runtimeStore.markCalls()
			for range 2 {
				err := carry.Start(fixture.ctx)
				required = nil
				if !errors.As(err, &required) || len(required.Episodes) != 1 || required.Episodes[0].ID != episode.ID || required.Episodes[0].Generation != episode.RecoveryGeneration {
					t.Fatalf("release_pending startup must retain exact reconciliation reference: %v", err)
				}
				if err := carry.VerifyDurableRecovery(fixture.ctx, gate, required.Episodes); err != nil {
					t.Fatal("managed startup readback rejected retained runtime:", err)
				}
			}
			if carry.IsRunning() || carry.ctx != nil || !gate.HasBlock(hold.Source) || !gate.HasBlock("manual_operator_hold") {
				t.Fatal("pending release started trading or lost an opening block")
			}
			if fixture.runtime.payload != beforePayload || fixture.runtime.compareCalls != beforeCAS || fixture.runtimeStore.markCalls() != beforeMarks ||
				len(futures.placedOrders)+len(spot.placedOrders)+futures.borrowRPCs+spot.borrowRPCs+futures.repayRPCs+spot.repayRPCs != 0 {
				t.Fatal("pending release startup projected or replayed financial operations")
			}
			afterEpisode, _, err := fixture.store.GetRecoveryEpisode(fixture.ctx, episode.ID)
			if err != nil || !reflect.DeepEqual(afterEpisode, pending) {
				t.Fatalf("startup changed episode owner/state: %v", err)
			}
			afterHold, _, err := fixture.store.GetRecoveryHold(fixture.ctx, episode.ID, episode.RecoveryGeneration)
			if err != nil || !reflect.DeepEqual(afterHold, hold) {
				t.Fatalf("startup changed original held hold: %v", err)
			}
			afterReceipt, _, err := fixture.store.GetRecoveryReceipt(fixture.ctx, episode.ID, auth.OperationID)
			if err != nil || !reflect.DeepEqual(afterReceipt, receipt) {
				t.Fatalf("startup changed release receipt: %v", err)
			}
		})
	}
}

func TestFundingCarryReleasePendingStartupVerifiesPriorTakeoverProjectionReceipt(t *testing.T) {
	for _, tampered := range []bool{false, true} {
		t.Run(fmt.Sprintf("tampered_takeover_receipt_%t", tampered), func(t *testing.T) {
			fixture := newDurableRecoveryFixture(t)
			prior := fixture.repository.episode
			prior.OwnerGeneration = "original-owner"
			fixture.setProjection(fundingCarryRecoveryProjectionPayload{Episode: prior})
			takeover := accounting.RecoveryOwnerTakeoverRecord{OperationID: "prior-takeover", Actor: "operator", AuditRef: "prior-incident",
				EvidenceHash: fmt.Sprintf("%064x", 92), PreviousOwnerGeneration: prior.OwnerGeneration,
				OwnerGeneration: "owner-before-restart", EpisodeRevision: prior.Revision + 1, CreatedAt: time.Now().UTC()}
			pending := prior
			pending.OwnerGeneration, pending.OwnerTakeovers = takeover.OwnerGeneration, []accounting.RecoveryOwnerTakeoverRecord{takeover}
			pending.State, pending.Revision = accounting.RecoveryReleasePending, prior.Revision+3
			fixture.repository.episode = pending
			outbox := &fixture.repository.outbox
			outbox.State, outbox.AppliedOperationID, outbox.AppliedRevision, outbox.AppliedHash = accounting.RecoveryProjected, outbox.OperationID, outbox.TargetEpisodeRevision, outbox.ProjectionHash
			fixture.repository.holds = []accounting.RecoveryHold{{ID: pending.ID + ":1", EpisodeID: pending.ID, Bot: pending.Bot,
				Strategy: pending.Strategy, AccountScopes: pending.AccountScopes, Generation: pending.RecoveryGeneration, Revision: 1,
				Source: execution.RecoveryEpisodeGateSource(pending.ID, pending.RecoveryGeneration), State: "held"}}
			provider := fixture.strategy.runtimeStateStore.(*durableRecoveryRuntimeBindingFixture).provider.(*durableRecoveryProviderFixture)
			provider.repository = pendingReleaseReceiptReader{FundingRecoveryStore: fixture.repository, mutate: func(r *accounting.RecoveryReceipt) {
				if r.OperationID == outbox.OperationID {
					r.EpisodeRevision++ // Audited takeover precedes the original projection completion.
				} else if tampered {
					r.RequestHash = "forged-takeover-request"
				}
			}}
			gate := &execution.OpeningGate{}
			fixture.strategy.SetOpeningGate(gate)
			var retained *FundingCarryDurableRecoveryRequiredError
			err := fixture.strategy.Start(fixture.ctx)
			if err == nil || errors.As(err, &retained) == tampered {
				t.Fatalf("prior takeover projection admission, tampered=%t: %v", tampered, err)
			}
			if fixture.strategy.IsRunning() || fixture.runtime.compareCalls != 0 || fixture.repository.marks != 0 || !gate.HasBlock(fixture.repository.holds[0].Source) {
				t.Fatal("prior takeover pending release replayed a projection or cleared its gate")
			}
		})
	}
}

type pendingReleaseReceiptReader struct {
	storage.FundingRecoveryStore
	mutate func(*accounting.RecoveryReceipt)
	absent bool
	err    error
}

func (r pendingReleaseReceiptReader) GetRecoveryReceipt(ctx context.Context, episodeID, operationID string) (accounting.RecoveryReceipt, bool, error) {
	receipt, found, err := r.FundingRecoveryStore.GetRecoveryReceipt(ctx, episodeID, operationID)
	if r.mutate != nil {
		r.mutate(&receipt)
	}
	if r.absent || r.err != nil {
		return accounting.RecoveryReceipt{}, false, r.err
	}
	return receipt, found, err
}

func TestFundingCarryReleasePendingStartupRejectsIncompleteEvidence(t *testing.T) {
	for _, tc := range []struct {
		name    string
		mutate  func(*durableRecoveryFixture)
		receipt pendingReleaseReceiptReader
	}{
		{name: "released_episode", mutate: func(f *durableRecoveryFixture) { f.repository.episode.State = accounting.RecoveryReleased }},
		{name: "released_hold", mutate: func(f *durableRecoveryFixture) { f.repository.holds[0].State = "released" }},
		{name: "zero_hold_revision", mutate: func(f *durableRecoveryFixture) { f.repository.holds[0].Revision = 0 }},
		{name: "released_by_operation", mutate: func(f *durableRecoveryFixture) { f.repository.holds[0].ReleasedByOperation = "prior-release" }},
		{name: "wrong_hold_source", mutate: func(f *durableRecoveryFixture) { f.repository.holds[0].Source = "other-source" }},
		{name: "wrong_hold_generation", mutate: func(f *durableRecoveryFixture) { f.repository.holds[0].Generation++ }},
		{name: "wrong_hold_scope", mutate: func(f *durableRecoveryFixture) { f.repository.holds[0].AccountScopes = []string{"wrong-scope"} }},
		{name: "missing_projection", mutate: func(f *durableRecoveryFixture) { f.repository.outbox = accounting.RecoveryOutbox{} }},
		{name: "pending_projection_current_owner", mutate: func(f *durableRecoveryFixture) { f.repository.outbox.State = accounting.RecoveryProjectionPending }},
		{name: "pending_projection_old_owner", mutate: func(f *durableRecoveryFixture) {
			f.repository.episode.OwnerGeneration = "old-owner"
			f.repository.outbox.State = accounting.RecoveryProjectionPending
		}},
		{name: "projection_hash_drift", mutate: func(f *durableRecoveryFixture) {
			f.repository.outbox.ProjectionPayload = append(f.repository.outbox.ProjectionPayload, 'x')
		}},
		{name: "missing_receipt", receipt: pendingReleaseReceiptReader{absent: true}},
		{name: "receipt_read_error", receipt: pendingReleaseReceiptReader{err: errors.New("receipt unavailable")}},
		{name: "receipt_hash_drift", receipt: pendingReleaseReceiptReader{mutate: func(r *accounting.RecoveryReceipt) { r.ProjectionHash = "different-hash" }}},
		{name: "receipt_owner_drift", receipt: pendingReleaseReceiptReader{mutate: func(r *accounting.RecoveryReceipt) { r.OwnerGeneration = "different-owner" }}},
		{name: "receipt_revision_drift", receipt: pendingReleaseReceiptReader{mutate: func(r *accounting.RecoveryReceipt) { r.EpisodeRevision++ }}},
		{name: "receipt_generation_drift", receipt: pendingReleaseReceiptReader{mutate: func(r *accounting.RecoveryReceipt) { r.RecoveryGeneration++ }}},
		{name: "receipt_still_pending", receipt: pendingReleaseReceiptReader{mutate: func(r *accounting.RecoveryReceipt) { r.State = accounting.RecoveryLedgerCommitted }}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture := newDurableRecoveryFixture(t)
			if err := fixture.strategy.reconcileDurableFundingCarryRecovery(fixture.ctx); err != nil {
				t.Fatal("complete fixture projection:", err)
			}
			fixture.repository.episode.State = accounting.RecoveryReleasePending
			fixture.repository.episode.Revision++
			episode := fixture.repository.episode
			fixture.repository.holds = []accounting.RecoveryHold{{ID: episode.ID + ":1", EpisodeID: episode.ID,
				Bot: episode.Bot, Strategy: episode.Strategy, AccountScopes: episode.AccountScopes,
				Generation: episode.RecoveryGeneration, Revision: 1, State: "held",
				Source: execution.RecoveryEpisodeGateSource(episode.ID, episode.RecoveryGeneration)}}
			if tc.mutate != nil {
				tc.mutate(&fixture)
			}
			provider := fixture.strategy.runtimeStateStore.(*durableRecoveryRuntimeBindingFixture).provider.(*durableRecoveryProviderFixture)
			tc.receipt.FundingRecoveryStore = fixture.repository
			provider.repository = tc.receipt
			gate := &execution.OpeningGate{}
			fixture.strategy.SetOpeningGate(gate)
			beforePayload, beforeCAS, beforeMarks := fixture.runtime.payload, fixture.runtime.compareCalls, fixture.repository.marks
			err := fixture.strategy.Start(fixture.ctx)
			var required *FundingCarryDurableRecoveryRequiredError
			if err == nil || errors.As(err, &required) {
				t.Fatalf("incomplete pending release admitted as managed recovery: %v", err)
			}
			if fixture.strategy.IsRunning() || fixture.runtime.payload != beforePayload || fixture.runtime.compareCalls != beforeCAS || fixture.repository.marks != beforeMarks || !gate.Blocked() {
				t.Fatal("rejected release evidence started trading, projected state or cleared gate")
			}
		})
	}
}
