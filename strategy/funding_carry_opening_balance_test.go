package strategy

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"quantmesh/accounting"
	"quantmesh/execution"
)

func TestFundingCarryOpeningBalanceProjectsAuditedRawSnapshotAndRetainsHold(t *testing.T) {
	fixture := newFundingRecoveryFillFixture(t, nil, true, true, true)
	snapshot := accounting.RecoveryOpeningBalanceSnapshot{
		Exchange: "binance", Market: "spot", AccountScope: fixture.episode.AccountScopes[0],
		SnapshotAt: time.Date(2026, 10, 13, 3, 4, 5, 0, time.UTC),
		Assets:     []accounting.RecoveryOpeningBalanceEntry{{Asset: "BTC", AmountRaw: "0.125000000000000001"}, {Asset: "USDT", AmountRaw: "1000.00"}},
	}
	evidenceHash, err := snapshot.CanonicalHash()
	if err != nil {
		t.Fatal(err)
	}
	request := ApplyFundingRecoveryOpeningBalanceRequest{EpisodeID: fixture.episode.ID, OperationID: "opening-op-171",
		ExpectedEpisodeRevision: fixture.episode.Revision, Snapshot: snapshot, EvidenceHash: evidenceHash,
		Actor: "operator", AuditRef: "incident-opening-171", Confirmed: true}
	result, err := fixture.strategy.ApplyFundingRecoveryOpeningBalance(fixture.ctx, request)
	if err != nil || !result.Projected || !result.HoldRetained || result.Replayed || len(result.Events) != len(snapshot.Assets) {
		t.Fatalf("opening balance result=%+v err=%v", result, err)
	}
	for index, event := range result.Events {
		if err := event.Validate(); err != nil {
			t.Fatalf("opening event %d invalid: %v", index, err)
		}
		if event.SourceHash != evidenceHash || event.OpeningBalance == nil || event.OpeningBalance.Actor != request.Actor ||
			event.OpeningBalance.AuditRef != request.AuditRef || event.OpeningBalance.SnapshotHash != evidenceHash {
			t.Fatalf("opening event is not bound to operation snapshot evidence: %+v", event)
		}
	}
	retry, err := fixture.strategy.ApplyFundingRecoveryOpeningBalance(fixture.ctx, request)
	if err != nil || !retry.Replayed || !retry.Projected || len(retry.Events) != len(result.Events) {
		t.Fatalf("same-operation retry did not read back exactly: result=%+v err=%v", retry, err)
	}
	conflictingRevision := request
	conflictingRevision.ExpectedEpisodeRevision++
	if _, err := fixture.strategy.ApplyFundingRecoveryOpeningBalance(fixture.ctx, conflictingRevision); !errors.Is(err, accounting.ErrRecoveryOperationConflict) {
		t.Fatalf("same operation with a different expected episode revision should conflict, got %v", err)
	}
	bad := request
	bad.EvidenceHash = fmt.Sprintf("%064x", 172)
	if _, err := fixture.strategy.ApplyFundingRecoveryOpeningBalance(fixture.ctx, bad); err == nil {
		t.Fatal("mismatched canonical snapshot hash was accepted")
	}
	hold, found, err := fixture.store.GetRecoveryHold(fixture.ctx, fixture.episode.ID, fixture.episode.RecoveryGeneration)
	if err != nil || !found || hold.State != "held" || !fixture.gate.HasBlock(execution.RecoveryEpisodeGateSource(fixture.episode.ID, fixture.episode.RecoveryGeneration)) {
		t.Fatalf("opening balance changed hold/Gate: hold=%+v found=%v err=%v", hold, found, err)
	}
	stored, err := fixture.store.ListRecoveryEvents(fixture.ctx, fixture.episode.ID)
	if err != nil || len(stored) != len(snapshot.Assets) {
		t.Fatalf("durable opening balance events=%d err=%v", len(stored), err)
	}
}

func TestFundingCarryOpeningBalanceRejectsScopeMismatch(t *testing.T) {
	fixture := newFundingRecoveryFillFixture(t, nil, true, true, true)
	snapshot := accounting.RecoveryOpeningBalanceSnapshot{Exchange: "binance", Market: "spot", AccountScope: fmt.Sprintf("%064x", 999),
		SnapshotAt: time.Now().UTC(), Assets: []accounting.RecoveryOpeningBalanceEntry{{Asset: "BTC", AmountRaw: "1"}}}
	evidenceHash, err := snapshot.CanonicalHash()
	if err != nil {
		t.Fatal(err)
	}
	_, err = fixture.strategy.ApplyFundingRecoveryOpeningBalance(fixture.ctx, ApplyFundingRecoveryOpeningBalanceRequest{
		EpisodeID: fixture.episode.ID, OperationID: "opening-scope-mismatch", ExpectedEpisodeRevision: fixture.episode.Revision,
		Snapshot: snapshot, EvidenceHash: evidenceHash, Actor: "operator", AuditRef: "audit-1", Confirmed: true,
	})
	if err == nil {
		t.Fatal("snapshot from a different account scope was accepted")
	}
	if _, found, readErr := fixture.store.GetRecoveryReceipt(fixture.ctx, fixture.episode.ID, "opening-scope-mismatch"); readErr != nil || found {
		t.Fatalf("scope mismatch wrote a receipt: found=%v err=%v", found, readErr)
	}
}
