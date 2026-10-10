package storage

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"quantmesh/accounting"
)

func TestOrderReconciliationSQLiteDurableCaseAndAudit(t *testing.T) {
	ctx := context.Background()
	s, err := NewSQLStorage(filepath.Join(t.TempDir(), "reconciliation.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.MigrateOrderReconciliations(ctx); err != nil {
		t.Fatal(err)
	}
	input := NewOrderReconciliationCase{ID: "case-1", IdempotencyKey: "request-1", Owner: testReconciliationOwner(), ExpectedIntentRevision: "intent-rev-2", ExpectedStrategyRevision: "strategy-rev-7", Reason: "manual review", Actor: "admin", EvidenceRefs: []string{"incident-42"}}
	created, replayed, err := s.CreateOrderReconciliationCase(ctx, input)
	if err != nil || replayed {
		t.Fatalf("create failed: replayed=%t err=%v", replayed, err)
	}
	if created.Status != OrderReconciliationPrepared || created.Revision != 1 || created.Receipt != nil {
		t.Fatalf("unexpected prepared case: %+v", created)
	}
	retry := input
	retry.ID = "another-id"
	got, replayed, err := s.CreateOrderReconciliationCase(ctx, retry)
	if err != nil || !replayed || got.ID != created.ID {
		t.Fatalf("idempotent retry mismatch: got=%+v replayed=%t err=%v", got, replayed, err)
	}
	fill := accounting.VerifiedFill{TradeID: "trade-1", CursorSequence: 1, Side: "BUY", PositionSide: "LONG", OrderRole: "entry", Price: "10", Quantity: "0.1", CommissionAmount: "0", CommissionAsset: "USDT", TradeTime: time.Now().UTC()}
	manual := accounting.ReconcileRequest{OperationID: created.ID, Owner: input.Owner, ExpectedRevision: input.ExpectedStrategyRevision, From: accounting.Cursor{}, Target: accounting.Cursor{Sequence: 1, TradeID: "trade-1"}, Fills: []accounting.VerifiedFill{fill}, Evidence: accounting.EvidenceSummary{Source: "fake", Summary: "verified", VerifiedBy: "admin", VerifiedAt: time.Now().UTC()}}
	running, err := s.StartOrderReconciliation(ctx, created.ID, created.Revision, "admin", manual, strings.Repeat("a", 64))
	if err != nil || running.Revision != 2 || running.OperationRequest == nil || running.OperationRequest.OperationID != created.ID {
		t.Fatalf("start transition did not durably bind request: item=%+v err=%v", running, err)
	}
	receipt := OrderReconciliationReceipt{Outcome: "reconciled", OperationID: created.ID, Owner: input.Owner, ExpectedRevision: input.ExpectedStrategyRevision, From: manual.From, Target: manual.Target, Fills: manual.Fills, Evidence: manual.Evidence, Revision: "strategy-rev-8", Result: accounting.EconomicResult{NetQuantity: "0.1"}, CompletedAt: time.Now().UTC()}
	ready, err := s.TransitionOrderReconciliation(ctx, created.ID, running.Revision, OrderReconciliationRunning, OrderReconciliationReady, "admin", "test_transition", "verified", []byte(`{"ok":true}`), &receipt, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	if err != nil || ready.Revision != 3 {
		t.Fatalf("verified transition failed: item=%+v err=%v", ready, err)
	}
	if _, err := s.TransitionOrderReconciliation(ctx, created.ID, running.Revision, OrderReconciliationRunning, OrderReconciliationReady, "admin", "duplicate", "verified", []byte(`{}`), &receipt, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"); err != ErrOrderReconciliationConflict {
		t.Fatalf("stale transition should conflict, got %v", err)
	}
	audit, err := s.ListOrderReconciliationAudit(ctx, created.ID)
	if err != nil || len(audit) != 3 || audit[0].Action != "case_created" || audit[1].Action != "reconcile_confirmed" || audit[2].Action != "test_transition" {
		t.Fatalf("unexpected audit: %+v err=%v", audit, err)
	}
	readback, found, err := s.GetOrderReconciliation(ctx, created.ID)
	if err != nil || !found || readback.Status != OrderReconciliationReady || readback.OperationRequest == nil || readback.Receipt == nil {
		t.Fatalf("durable readback failed: %+v found=%t err=%v", readback, found, err)
	}
}

func TestOrderReconciliationMigrationIdempotentAndMySQLReceiptNullable(t *testing.T) {
	ctx := context.Background()
	s, err := NewSQLStorage(filepath.Join(t.TempDir(), "repeat.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.MigrateOrderReconciliations(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.MigrateOrderReconciliations(ctx); err != nil {
		t.Fatalf("migration should be idempotent: %v", err)
	}
	item, found, err := s.scanOrderReconciliation(s.db.QueryRowContext(ctx, orderReconciliationSelect+` WHERE case_id=?`, "missing"))
	if err != nil || found || item.ID != "" {
		t.Fatalf("nullable receipt/missing-row scan failed: %+v found=%t err=%v", item, found, err)
	}
}

func testReconciliationOwner() accounting.OrderOwner {
	return accounting.OrderOwner{Exchange: "fake", Market: "spot", AccountScope: "acct-1", Bot: "bot-1", Symbol: "BTCUSDT", StrategyName: "trend", StrategyType: "trend", ClientOrderID: "cid-1", VenueOrderID: "42"}
}
