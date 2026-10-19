package accounting

import (
	"fmt"
	"testing"
	"time"
)

func TestRecoveryReleaseAuthorizationRequiresAuditedExposureClearance(t *testing.T) {
	base := RecoveryReleaseAuthorization{EpisodeID: "episode", OperationID: "operation", Actor: "admin", AuditRef: "accounting-case-17",
		EvidenceHash: fmt.Sprintf("%064x", 1), ExposureClearanceHash: fmt.Sprintf("%064x", 2), ProjectionHash: fmt.Sprintf("%064x", 3),
		RuntimeRevision: "runtime-rev-4", OwnerGeneration: "owner-4", ExpectedEpisodeRevision: 8, RecoveryGeneration: 2,
		ExpectedHoldRevision: 3, Confirmed: true, ExposureClearanceConfirmed: true}
	if err := base.Validate(); err != nil {
		t.Fatalf("valid audited exposure clearance rejected: %v", err)
	}
	withoutHash := base
	withoutHash.ExposureClearanceHash = ""
	withoutConfirmation := base
	withoutConfirmation.ExposureClearanceConfirmed = false
	for _, candidate := range []RecoveryReleaseAuthorization{withoutHash, withoutConfirmation} {
		if err := candidate.Validate(); err == nil {
			t.Fatal("release authorization without audited no-position/no-liability clearance was accepted")
		}
	}
}

func TestRecoveryDebtEventRequiresExactRawComponentsAndDirection(t *testing.T) {
	base := RecoveryEvent{
		ID: "debt-1", Source: "binance_margin_history", Exchange: "binance", Market: "spot_margin",
		AccountScope: fmt.Sprintf("%064x", 1), Kind: RecoveryBorrowEvent, ExternalID: "tx-1", EpisodeID: "episode-1",
		Asset: "BTC", Amount: "0.300", SourceRevision: "1", SourceHash: fmt.Sprintf("%064x", 2), ObservedAt: time.Now().UTC(),
		DebtEvidence: &RecoveryDebtEvidence{PrincipalRaw: "0.1", InterestRaw: "0.2000"},
	}
	if err := base.Validate(); err != nil {
		t.Fatalf("exact raw borrow components rejected: %v", err)
	}
	tests := []struct {
		name   string
		mutate func(*RecoveryEvent)
	}{
		{name: "float reconstruction shape", mutate: func(e *RecoveryEvent) { e.Amount = "0.30000000000000004" }},
		{name: "borrow wrong direction", mutate: func(e *RecoveryEvent) { e.Amount = "-0.3" }},
		{name: "missing evidence", mutate: func(e *RecoveryEvent) { e.DebtEvidence = nil }},
		{name: "exponent notation", mutate: func(e *RecoveryEvent) { e.DebtEvidence.PrincipalRaw = "1e-1" }},
		{name: "negative component", mutate: func(e *RecoveryEvent) { e.DebtEvidence.InterestRaw = "-0.2" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event := base
			components := *base.DebtEvidence
			event.DebtEvidence = &components
			tt.mutate(&event)
			if err := event.Validate(); err == nil {
				t.Fatal("invalid debt evidence was accepted")
			}
		})
	}
	repay := base
	repay.Kind = RecoveryRepayEvent
	repay.Amount = "-0.3"
	repay.DebtEvidence = &RecoveryDebtEvidence{PrincipalRaw: "0.1", InterestRaw: "0.2"}
	if err := repay.Validate(); err != nil {
		t.Fatalf("exact raw repay components rejected: %v", err)
	}
	repay.Amount = "0.3"
	if err := repay.Validate(); err == nil {
		t.Fatal("repay with positive cash outflow was accepted")
	}
}

func TestRecoverySourceWindowRequiresExplicitCompleteEvidence(t *testing.T) {
	base := RecoverySourceWindow{
		Source: "binance_income_history", Exchange: "binance", Market: "futures", AccountScope: fmt.Sprintf("%064x", 3),
		Kind: RecoveryFundingEvent, Symbol: "BTCUSDT", Asset: "USDT", From: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		Through: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC), Rows: 0, TotalRows: 0,
		CompletionMarker: RecoverySourceWindowCompleteV1, EvidenceHash: fmt.Sprintf("%064x", 4),
	}
	if err := base.Validate(); err != nil {
		t.Fatalf("complete empty source window rejected: %v", err)
	}
	base.CompletionMarker = ""
	if err := base.Validate(); err == nil {
		t.Fatal("source window without completion marker was accepted")
	}
	base.CompletionMarker = RecoverySourceWindowCompleteV1
	base.Rows, base.TotalRows = 1, 2
	if err := base.Validate(); err == nil {
		t.Fatal("incomplete source window was accepted")
	}
	base.Rows, base.TotalRows = 0, 0
	base.From = base.From.Add(time.Nanosecond)
	if err := base.Validate(); err == nil {
		t.Fatal("sub-millisecond source window was accepted")
	}
}

func TestRecoveryOpeningBalanceIsAuditedRawBaselineNotIncome(t *testing.T) {
	event := RecoveryEvent{
		ID: "opening-1", Source: RecoveryManualOpeningBalanceSource, Exchange: "binance", Market: "spot_margin",
		AccountScope: fmt.Sprintf("%064x", 7), Kind: RecoveryOpeningBalanceEvent, ExternalID: "snapshot-op-1", EpisodeID: "episode-1",
		Asset: "BTC", Amount: "-0.125000000000000001", SourceRevision: "operator-entry-v1", SourceHash: fmt.Sprintf("%064x", 8),
		ObservedAt: time.Date(2026, 10, 13, 1, 0, 0, 0, time.UTC),
		OpeningBalance: &RecoveryOpeningBalanceEvidence{
			Actor: "operator@example.invalid", AuditRef: "incident-42", SnapshotHash: fmt.Sprintf("%064x", 8),
			SnapshotAt: time.Date(2026, 10, 13, 1, 0, 0, 0, time.UTC),
		},
	}
	if err := event.Validate(); err != nil {
		t.Fatalf("valid audited raw balance baseline rejected: %v", err)
	}
	earlierSnapshot := event
	earlierEvidence := *event.OpeningBalance
	earlierSnapshot.OpeningBalance = &earlierEvidence
	earlierSnapshot.ObservedAt = event.ObservedAt.Add(2 * time.Minute)
	if err := earlierSnapshot.Validate(); err != nil {
		t.Fatalf("snapshot earlier than operator observation must be valid: %v", err)
	}
	mutations := []struct {
		name   string
		mutate func(*RecoveryEvent)
	}{
		{name: "source", mutate: func(e *RecoveryEvent) { e.Source = "exchange_history" }},
		{name: "operator", mutate: func(e *RecoveryEvent) { e.OpeningBalance.Actor = " " }},
		{name: "audit reference", mutate: func(e *RecoveryEvent) { e.OpeningBalance.AuditRef = "" }},
		{name: "snapshot hash", mutate: func(e *RecoveryEvent) { e.OpeningBalance.SnapshotHash = "not-a-hash" }},
		{name: "snapshot time", mutate: func(e *RecoveryEvent) { e.OpeningBalance.SnapshotAt = time.Time{} }},
		{name: "snapshot after observation", mutate: func(e *RecoveryEvent) { e.OpeningBalance.SnapshotAt = e.ObservedAt.Add(time.Second) }},
		{name: "exponent notation", mutate: func(e *RecoveryEvent) { e.Amount = "1.25e-1" }},
	}
	for _, test := range mutations {
		t.Run(test.name, func(t *testing.T) {
			candidate := event
			evidence := *event.OpeningBalance
			candidate.OpeningBalance = &evidence
			test.mutate(&candidate)
			if err := candidate.Validate(); err == nil {
				t.Fatal("invalid opening balance evidence was accepted")
			}
		})
	}
}

func TestRecoveryOpeningBalanceSnapshotHashIsCanonicalAndRejectsAmbiguousAssets(t *testing.T) {
	base := RecoveryOpeningBalanceSnapshot{Exchange: "binance", Market: "spot_margin", AccountScope: fmt.Sprintf("%064x", 9),
		SnapshotAt: time.Date(2026, 10, 13, 2, 0, 0, 0, time.FixedZone("UTC+8", 8*60*60)),
		Assets:     []RecoveryOpeningBalanceEntry{{Asset: "USDT", AmountRaw: "10.000"}, {Asset: "BTC", AmountRaw: "0.125000000000000001"}}}
	first, err := base.CanonicalHash()
	if err != nil {
		t.Fatal(err)
	}
	base.Assets[0], base.Assets[1] = base.Assets[1], base.Assets[0]
	second, err := base.CanonicalHash()
	if err != nil || first != second {
		t.Fatalf("asset order changed canonical snapshot hash: %q != %q err=%v", first, second, err)
	}
	base.Assets = append(base.Assets, RecoveryOpeningBalanceEntry{Asset: "BTC", AmountRaw: "0.1"})
	if _, err := base.CanonicalHash(); err == nil {
		t.Fatal("duplicate asset snapshot was accepted")
	}
	base.Assets = []RecoveryOpeningBalanceEntry{{Asset: "BTC", AmountRaw: "1e-3"}}
	if _, err := base.CanonicalHash(); err == nil {
		t.Fatal("exponent-form balance was accepted")
	}
}
