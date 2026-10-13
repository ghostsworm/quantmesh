package storage

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"quantmesh/accounting"
)

func TestMarkRecoveryProjectedRejectsRuntimeRevisionAndReceiptMismatchWithoutSideEffects(t *testing.T) {
	tests := []struct {
		name                  string
		tamperReceipt         bool
		tamperDurableMetadata bool
	}{
		{name: "expected runtime revision differs from durable outbox"},
		{name: "durable outbox runtime revision differs from hashed projection", tamperDurableMetadata: true},
		{name: "receipt projection hash differs while outbox remains intact", tamperReceipt: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store, generation, episode, scope := newSourceCursorFixture(t, "episode-integrity-"+strings.ReplaceAll(test.name, " ", "-"))
			ctx := context.Background()
			from := time.Date(2026, 10, 13, 8, 0, 0, 0, time.UTC)
			window := sourceWindow(scope, from, from.Add(time.Hour), 0, 1)
			operation := fundingOperation(episode, scope, "integrity-operation", episode.Revision, window, "integrity-funding-row", "-0.125")
			if _, replayed, err := store.ApplyRecoveryOperation(ctx, generation, operation); err != nil || replayed {
				t.Fatalf("seed durable recovery operation: replayed=%t err=%v", replayed, err)
			}

			outbox, found, err := store.GetRecoveryOutbox(ctx, episode.ID, operation.OperationID)
			if err != nil || !found || outbox.State != accounting.RecoveryProjectionPending {
				t.Fatalf("read pending outbox: found=%t outbox=%+v err=%v", found, outbox, err)
			}
			identity := window.Identity()
			if test.tamperReceipt {
				receipt, found, readErr := store.GetRecoveryReceipt(ctx, episode.ID, operation.OperationID)
				if readErr != nil || !found || receipt.ProjectionHash != outbox.ProjectionHash {
					t.Fatalf("read matching operation receipt: found=%t receipt=%+v err=%v", found, receipt, readErr)
				}
				receipt.ProjectionHash = strings.Repeat("f", 64)
				receiptJSON, marshalErr := json.Marshal(receipt)
				if marshalErr != nil {
					t.Fatal("marshal receipt-only tamper:", marshalErr)
				}
				if _, err = store.db.ExecContext(ctx, `UPDATE funding_recovery_operations SET receipt_json=? WHERE episode_id=? AND operation_id=?`, receiptJSON, episode.ID, operation.OperationID); err != nil {
					t.Fatal("tamper only durable receipt projection hash:", err)
				}
				tamperedReceipt, receiptFound, readErr := store.GetRecoveryReceipt(ctx, episode.ID, operation.OperationID)
				if readErr != nil || !receiptFound || tamperedReceipt.ProjectionHash == outbox.ProjectionHash {
					t.Fatalf("receipt-only fixture did not create an explicit hash mismatch: receipt=%+v outbox_hash=%q found=%t err=%v",
						tamperedReceipt, outbox.ProjectionHash, receiptFound, readErr)
				}
			} else {
				outbox.ExpectedRuntimeRevision += ":tampered"
				if test.tamperDurableMetadata {
					outboxJSON, marshalErr := json.Marshal(outbox)
					if marshalErr != nil {
						t.Fatal("marshal outbox metadata tamper:", marshalErr)
					}
					if _, err = store.db.ExecContext(ctx, `UPDATE funding_recovery_outbox SET payload_json=? WHERE episode_id=? AND operation_id=?`,
						outboxJSON, episode.ID, operation.OperationID); err != nil {
						t.Fatal("persist only outbox runtime revision metadata tamper:", err)
					}
				}
			}

			before := readFundingRecoveryIntegritySnapshot(t, store, episode.ID, operation.OperationID, identity, episode.RecoveryGeneration)
			outbox.AppliedOperationID = outbox.OperationID
			outbox.AppliedRevision = outbox.TargetEpisodeRevision
			outbox.AppliedHash = outbox.ProjectionHash
			outbox.State = accounting.RecoveryProjected
			if err := store.MarkRecoveryProjected(ctx, generation, outbox); err == nil {
				t.Fatal("MarkRecoveryProjected accepted mismatched durable recovery evidence")
			}
			after := readFundingRecoveryIntegritySnapshot(t, store, episode.ID, operation.OperationID, identity, episode.RecoveryGeneration)
			if !reflect.DeepEqual(before, after) {
				t.Fatalf("rejected projection had durable side effects:\nbefore=%+v\nafter=%+v", before, after)
			}
			if after.outbox.State != accounting.RecoveryProjectionPending || after.hold.State != "held" {
				t.Fatalf("rejected projection did not retain pending outbox and active hold: outbox=%+v hold=%+v", after.outbox, after.hold)
			}
		})
	}
}

func TestRecoveryReceiptOutboxTargetPlusTwoRequiresExactTakeoverEpisodeSnapshot(t *testing.T) {
	created := time.Date(2026, 10, 13, 8, 0, 0, 0, time.UTC)
	prior := accounting.RecoveryEpisode{ID: "episode-takeover-pair", Bot: "bot-a", Strategy: "funding_carry",
		AccountScopes: []string{strings.Repeat("a", 64)}, OwnerGeneration: "generation-old", Revision: 4,
		RecoveryGeneration: 1, State: accounting.RecoveryLedgerCommitted, CreatedAt: created, UpdatedAt: created}
	takeover := accounting.RecoveryOwnerTakeoverRecord{OperationID: "takeover-op", Actor: "operator", AuditRef: "incident-takeover",
		EvidenceHash: strings.Repeat("b", 64), PreviousOwnerGeneration: "generation-old", OwnerGeneration: "generation-new",
		EpisodeRevision: 5, CreatedAt: created.Add(time.Minute)}
	current := prior
	current.OwnerGeneration = takeover.OwnerGeneration
	current.Revision = takeover.EpisodeRevision
	current.State = accounting.RecoveryBlocked
	current.UpdatedAt = takeover.CreatedAt
	current.OwnerTakeovers = []accounting.RecoveryOwnerTakeoverRecord{takeover}
	projected := current
	projected.Revision = 6
	projected.State = accounting.RecoveryProjected
	projected.UpdatedAt = created.Add(2 * time.Minute)
	const expectedRuntimeRevision = "runtime-revision-before-projection"
	payload, err := json.Marshal(fundingRecoveryProjection{Episode: prior, Children: []accounting.RecoveryChild{}, Events: []accounting.RecoveryEvent{},
		ExpectedRuntimeRevision: expectedRuntimeRevision})
	if err != nil {
		t.Fatal("marshal takeover projection:", err)
	}
	hash, err := fundingRecoveryProjectionHash(payload)
	if err != nil {
		t.Fatal("hash takeover projection:", err)
	}
	outbox := accounting.RecoveryOutbox{EpisodeID: prior.ID, OperationID: "operation-source", ProjectionPayload: payload,
		ExpectedRuntimeRevision: expectedRuntimeRevision, TargetEpisodeRevision: prior.Revision, ProjectionHash: hash, AppliedOperationID: "operation-source",
		AppliedRevision: prior.Revision, AppliedHash: hash, State: accounting.RecoveryProjected}
	receipt := accounting.RecoveryReceipt{EpisodeID: prior.ID, OperationID: outbox.OperationID, ProjectionHash: hash,
		EpisodeRevision: projected.Revision, State: accounting.RecoveryProjected, OwnerGeneration: prior.OwnerGeneration,
		RecoveryGeneration: prior.RecoveryGeneration}
	proposal := accounting.RecoveryOwnerTakeoverAuthorization{EpisodeID: prior.ID, OperationID: takeover.OperationID, Actor: takeover.Actor,
		AuditRef: takeover.AuditRef, ExpectedOwnerGeneration: takeover.PreviousOwnerGeneration,
		ExpectedEpisodeRevision: takeover.EpisodeRevision - 1, Confirmed: true}
	requestHash, err := ExpectedFundingRecoveryOwnerTakeoverRequestHash(takeover.OwnerGeneration, prior.AccountScopes, proposal)
	if err != nil {
		t.Fatal("hash takeover authorization:", err)
	}
	takeoverReceipt := accounting.RecoveryReceipt{EpisodeID: prior.ID, OperationID: takeover.OperationID, RequestHash: requestHash,
		Actor: takeover.Actor, AuditRef: takeover.AuditRef, EvidenceHash: takeover.EvidenceHash, ProjectionHash: takeover.EvidenceHash,
		RecoveryGeneration: prior.RecoveryGeneration, State: accounting.RecoveryBlocked, OwnerGeneration: takeover.OwnerGeneration,
		PreviousOwnerGeneration: takeover.PreviousOwnerGeneration, EpisodeRevision: takeover.EpisodeRevision, CreatedAt: takeover.CreatedAt.Add(time.Second)}
	if err := VerifyFundingRecoveryReceiptOutboxPairAfterVerifiedTakeover(receipt, takeoverReceipt, outbox, projected); err != nil {
		t.Fatalf("valid exact takeover projection rejected: %v", err)
	}
	projected.RecoveryGeneration++
	if err := VerifyFundingRecoveryReceiptOutboxPairAfterVerifiedTakeover(receipt, takeoverReceipt, outbox, projected); err == nil {
		t.Fatal("takeover pair accepted unrelated episode business-state mutation")
	}
	if err := VerifyFundingRecoveryReceiptOutboxPair(receipt, outbox); err == nil {
		t.Fatal("generic receipt/outbox verifier accepted unproven target+2 revision")
	}
	projected.RecoveryGeneration--
	takeoverReceipt.EvidenceHash = strings.Repeat("c", 64)
	if err := VerifyFundingRecoveryReceiptOutboxPairAfterVerifiedTakeover(receipt, takeoverReceipt, outbox, projected); err == nil {
		t.Fatal("takeover pair accepted mismatched takeover-operation receipt")
	}
}

func TestRecoveryReceiptOutboxPairRequiresHashBoundRuntimeRevision(t *testing.T) {
	created := time.Date(2026, 10, 13, 8, 0, 0, 0, time.UTC)
	episode := accounting.RecoveryEpisode{ID: "episode-legacy-runtime-revision", Bot: "bot-a", Strategy: "funding_carry",
		AccountScopes: []string{strings.Repeat("a", 64)}, OwnerGeneration: "generation-a", Revision: 2,
		RecoveryGeneration: 1, State: accounting.RecoveryLedgerCommitted, CreatedAt: created, UpdatedAt: created}
	payload, err := json.Marshal(fundingRecoveryProjection{Episode: episode, Children: []accounting.RecoveryChild{}, Events: []accounting.RecoveryEvent{}})
	if err != nil {
		t.Fatal("marshal legacy projection fixture:", err)
	}
	hash, err := fundingRecoveryProjectionHash(payload)
	if err != nil {
		t.Fatal("hash legacy projection fixture:", err)
	}
	outbox := accounting.RecoveryOutbox{EpisodeID: episode.ID, OperationID: "legacy-operation", ProjectionPayload: payload,
		ExpectedRuntimeRevision: "runtime-revision-unproven", TargetEpisodeRevision: episode.Revision, ProjectionHash: hash,
		State: accounting.RecoveryProjectionPending}
	receipt := accounting.RecoveryReceipt{EpisodeID: episode.ID, OperationID: outbox.OperationID, EpisodeRevision: episode.Revision,
		State: accounting.RecoveryLedgerCommitted, ProjectionHash: hash}
	if err := VerifyFundingRecoveryReceiptOutboxPair(receipt, outbox); err == nil || !strings.Contains(err.Error(), "manual reconciliation") {
		t.Fatalf("legacy payload without authenticated runtime revision was not blocked for manual reconciliation: %v", err)
	}
	if err := VerifyFundingRecoveryProjectionHash(payload, hash); err != nil {
		t.Fatalf("legacy payload hash should remain readable for audit even though automatic projection is blocked: %v", err)
	}
}

type fundingRecoveryIntegritySnapshot struct {
	episode accounting.RecoveryEpisode
	receipt accounting.RecoveryReceipt
	outbox  accounting.RecoveryOutbox
	cursor  accounting.RecoverySourceCursor
	events  []accounting.RecoveryEvent
	hold    accounting.RecoveryHold
}

func readFundingRecoveryIntegritySnapshot(
	t *testing.T,
	store *SQLStorage,
	episodeID, operationID string,
	identity accounting.RecoverySourceIdentity,
	recoveryGeneration uint64,
) fundingRecoveryIntegritySnapshot {
	t.Helper()
	ctx := context.Background()
	var snapshot fundingRecoveryIntegritySnapshot
	var found bool
	var err error
	if snapshot.episode, found, err = store.GetRecoveryEpisode(ctx, episodeID); err != nil || !found {
		t.Fatalf("read episode integrity snapshot: found=%t err=%v", found, err)
	}
	if snapshot.receipt, found, err = store.GetRecoveryReceipt(ctx, episodeID, operationID); err != nil || !found {
		t.Fatalf("read receipt integrity snapshot: found=%t err=%v", found, err)
	}
	if snapshot.outbox, found, err = store.GetRecoveryOutbox(ctx, episodeID, operationID); err != nil || !found {
		t.Fatalf("read outbox integrity snapshot: found=%t err=%v", found, err)
	}
	if snapshot.cursor, found, err = store.GetRecoverySourceCursor(ctx, identity); err != nil || !found {
		t.Fatalf("read cursor integrity snapshot: found=%t err=%v", found, err)
	}
	if snapshot.events, err = store.ListRecoveryEvents(ctx, episodeID); err != nil {
		t.Fatalf("read ledger integrity snapshot: %v", err)
	}
	if snapshot.hold, found, err = store.GetRecoveryHold(ctx, episodeID, recoveryGeneration); err != nil || !found {
		t.Fatalf("read hold integrity snapshot: found=%t err=%v", found, err)
	}
	if len(snapshot.events) != 1 {
		t.Fatalf("seed ledger event count = %d, want 1", len(snapshot.events))
	}
	return snapshot
}
