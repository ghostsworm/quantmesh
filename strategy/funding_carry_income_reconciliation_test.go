package strategy

import (
	"errors"
	"strings"
	"testing"
	"time"

	"quantmesh/accounting"
	"quantmesh/exchange/income"
)

func TestValidateFundingIncomeInitialFromDoesNotSkipEpisodeHistory(t *testing.T) {
	created := time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name    string
		from    time.Time
		wantErr bool
	}{
		{name: "same instant", from: created},
		{name: "earlier baseline", from: created.Add(-time.Millisecond)},
		{name: "later than episode", from: created.Add(time.Millisecond), wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateFundingIncomeInitialFrom(test.from, created)
			if (err != nil) != test.wantErr {
				t.Fatalf("initial range validation error=%v, wantErr=%t", err, test.wantErr)
			}
		})
	}
}

func TestValidateFundingIncomeRetryRevisionBindsOriginalRequest(t *testing.T) {
	request := ApplyFundingRecoveryIncomeRequest{EpisodeID: "episode", OperationID: "operation", ExpectedEpisodeRevision: 7}
	projectedEpisode := accounting.RecoveryEpisode{ID: request.EpisodeID, OwnerGeneration: "owner-old", Revision: 8}
	currentEpisode := accounting.RecoveryEpisode{ID: request.EpisodeID, OwnerGeneration: "owner-old", Revision: 9}
	projectedReceipt := accounting.RecoveryReceipt{EpisodeID: request.EpisodeID, OperationID: request.OperationID,
		EpisodeRevision: 9, State: accounting.RecoveryProjected, ProjectionHash: "projection-hash", OwnerGeneration: "owner-old"}
	projectedOutbox := accounting.RecoveryOutbox{EpisodeID: request.EpisodeID, OperationID: request.OperationID,
		TargetEpisodeRevision: 8, State: accounting.RecoveryProjected, ProjectionHash: "projection-hash"}
	if err := validateFundingIncomeRetryRevision(projectedReceipt, projectedOutbox, request, projectedEpisode, currentEpisode); err != nil {
		t.Fatalf("matching projected retry revision rejected: %v", err)
	}
	for _, revision := range []uint64{6, 8, accounting.MaxDurableRecoveryRevision - 1, accounting.MaxDurableRecoveryRevision, ^uint64(0)} {
		request.ExpectedEpisodeRevision = revision
		if err := validateFundingIncomeRetryRevision(projectedReceipt, projectedOutbox, request, projectedEpisode, currentEpisode); !errors.Is(err, accounting.ErrRecoveryOperationConflict) {
			t.Fatalf("mismatched retry revision %d error=%v, want conflict", revision, err)
		}
	}
	pendingReceipt := accounting.RecoveryReceipt{EpisodeID: request.EpisodeID, OperationID: request.OperationID,
		EpisodeRevision: 8, State: accounting.RecoveryLedgerCommitted, ProjectionHash: "projection-hash", OwnerGeneration: "owner-old"}
	pendingOutbox := accounting.RecoveryOutbox{EpisodeID: request.EpisodeID, OperationID: request.OperationID,
		TargetEpisodeRevision: 8, State: accounting.RecoveryProjectionPending, ProjectionHash: "projection-hash"}
	request.ExpectedEpisodeRevision = 7
	if err := validateFundingIncomeRetryRevision(pendingReceipt, pendingOutbox, request, projectedEpisode, currentEpisode); err != nil {
		t.Fatalf("matching pending retry revision rejected: %v", err)
	}
	pendingReceipt.EpisodeRevision++
	if err := validateFundingIncomeRetryRevision(pendingReceipt, pendingOutbox, request, projectedEpisode, currentEpisode); !errors.Is(err, accounting.ErrRecoveryOperationConflict) {
		t.Fatalf("tampered pending receipt revision error=%v, want conflict", err)
	}
	takeoverEpisode := accounting.RecoveryEpisode{ID: request.EpisodeID, OwnerGeneration: "owner-new", Revision: 10,
		OwnerTakeovers: []accounting.RecoveryOwnerTakeoverRecord{{OperationID: "takeover", Actor: "admin", AuditRef: "incident",
			EvidenceHash: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", PreviousOwnerGeneration: "owner-old",
			OwnerGeneration: "owner-new", EpisodeRevision: 9}}}
	projectedReceipt.EpisodeRevision = 10
	if err := validateFundingIncomeRetryRevision(projectedReceipt, projectedOutbox, request, projectedEpisode, takeoverEpisode); err != nil {
		t.Fatalf("receipt revision advanced by audited takeover was rejected: %v", err)
	}
	mismatchedTakeoverOwner := takeoverEpisode
	mismatchedTakeoverOwner.OwnerTakeovers = append([]accounting.RecoveryOwnerTakeoverRecord(nil), takeoverEpisode.OwnerTakeovers...)
	mismatchedTakeoverOwner.OwnerTakeovers[0].OwnerGeneration = "other-owner"
	if err := validateFundingIncomeRetryRevision(projectedReceipt, projectedOutbox, request, projectedEpisode, mismatchedTakeoverOwner); !errors.Is(err, accounting.ErrRecoveryOperationConflict) {
		t.Fatalf("receipt revision advanced by takeover for another owner error=%v, want conflict", err)
	}
	if err := validateFundingIncomeRetryRevision(projectedReceipt, projectedOutbox, request, projectedEpisode, currentEpisode); !errors.Is(err, accounting.ErrRecoveryOperationConflict) {
		t.Fatalf("receipt revision advanced without takeover evidence error=%v, want conflict", err)
	}
}

func TestMakeFundingRecoveryIncomeEventsPreservesRawFactsAndSymbolIdentity(t *testing.T) {
	identity := accounting.RecoverySourceIdentity{Source: accounting.RecoverySourceBinanceFuturesIncomeV1, Exchange: "binance", Market: "futures",
		AccountScope: "scope-futures", Symbol: "BTCUSDT", Kind: accounting.RecoveryFundingEvent, Asset: "USDT"}
	at := time.Date(2026, 10, 12, 12, 30, 0, 123000000, time.UTC)
	rows := []*income.Income{{Symbol: "BTCUSDT", IncomeType: "FUNDING_FEE", Income: -0.01, RawIncome: "-0.010000000000000001",
		Asset: "USDT", Info: "funding marker", TransactionID: 98765, TradeTime: at}}
	events, err := makeFundingRecoveryIncomeEvents("episode-income", identity, "collector-revision", rows)
	if err != nil || len(events) != 1 {
		t.Fatalf("map source row: events=%+v err=%v", events, err)
	}
	event := events[0]
	if event.Kind != accounting.RecoveryFundingEvent || event.Symbol != identity.Symbol || event.AccountScope != identity.AccountScope ||
		event.ExternalID != "98765" || event.Amount != "-0.010000000000000001" || event.ObservedAt.UnixMilli() != at.UnixMilli() ||
		event.SourceRevision != "collector-revision" || event.SourceHash == "" || event.Validate() != nil {
		t.Fatalf("raw funding event was not preserved: %+v", event)
	}
	changedScope := identity
	changedScope.AccountScope = "another-scope"
	other, err := makeFundingRecoveryIncomeEvents("episode-income", changedScope, "collector-revision", rows)
	if err != nil || other[0].ID == event.ID {
		t.Fatalf("scope-distinct event identity was not preserved: events=%+v err=%v", other, err)
	}
}

func TestMakeFundingRecoveryIncomeEventsRetainsZeroAndRejectsCrossScopeRows(t *testing.T) {
	identity := accounting.RecoverySourceIdentity{Source: accounting.RecoverySourceBinanceFuturesIncomeV1, Exchange: "binance", Market: "futures",
		AccountScope: "scope-futures", Symbol: "BTCUSDT", Kind: accounting.RecoveryFundingEvent, Asset: "USDT"}
	row := &income.Income{Symbol: "BTCUSDT", IncomeType: "FUNDING_FEE", RawIncome: "0.000", Asset: "USDT", TransactionID: 1, TradeTime: time.Now().UTC()}
	events, err := makeFundingRecoveryIncomeEvents("episode-zero", identity, "collector", []*income.Income{row})
	if err != nil || len(events) != 1 || events[0].Amount != "0.000" {
		t.Fatalf("zero-valued source fact was not retained exactly: events=%+v err=%v", events, err)
	}
	row.RawIncome = "-0.1"
	row.Asset = "BTC"
	if events, err := makeFundingRecoveryIncomeEvents("episode-wrong-asset", identity, "collector", []*income.Income{row}); err == nil || events != nil {
		t.Fatalf("cross-asset row was accepted: events=%+v err=%v", events, err)
	}
}

func TestVerifyFundingRecoveryIncomeHistoryRejectsLateInsertedOrChangedRows(t *testing.T) {
	identity := accounting.RecoverySourceIdentity{Source: accounting.RecoverySourceBinanceFuturesIncomeV1, Exchange: "binance", Market: "futures",
		AccountScope: "scope-futures", Symbol: "BTCUSDT", Kind: accounting.RecoveryFundingEvent, Asset: "USDT"}
	from := time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC)
	through := from.Add(time.Hour)
	row := &income.Income{Symbol: "BTCUSDT", IncomeType: "FUNDING_FEE", RawIncome: "-0.01", Asset: "USDT", TransactionID: 1, TradeTime: from.Add(time.Minute)}
	expected, err := makeFundingRecoveryIncomeEvents("episode-history", identity, "r1", []*income.Income{row})
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyFundingRecoveryIncomeHistory("episode-history", identity, from, through, []*income.Income{row}, expected); err != nil {
		t.Fatalf("unchanged accepted history rejected: %v", err)
	}
	late := *row
	late.TransactionID = 2
	if err := verifyFundingRecoveryIncomeHistory("episode-history", identity, from, through, []*income.Income{row, &late}, expected); err == nil {
		t.Fatal("late historical row was not detected")
	}
	changed := *row
	changed.RawIncome = "-0.02"
	if err := verifyFundingRecoveryIncomeHistory("episode-history", identity, from, through, []*income.Income{&changed}, expected); err == nil {
		t.Fatal("changed historical row was not detected")
	}
}

func TestDiffFundingRecoveryIncomeHistoryPreservesOriginalAndCalculatesExactAdjustments(t *testing.T) {
	identity := accounting.RecoverySourceIdentity{Source: accounting.RecoverySourceBinanceFuturesIncomeV1, Exchange: "binance", Market: "futures",
		AccountScope: "scope-futures", Symbol: "BTCUSDT", Kind: accounting.RecoveryFundingEvent, Asset: "USDT"}
	from := time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC)
	through := from.Add(time.Hour)
	originalRow := &income.Income{Symbol: identity.Symbol, IncomeType: "FUNDING_FEE", RawIncome: "-1.000000000000000001", Asset: identity.Asset, TransactionID: 101, TradeTime: from.Add(time.Minute)}
	originals, err := makeFundingRecoveryIncomeEvents("episode-anomaly", identity, "collector-v1", []*income.Income{originalRow})
	if err != nil {
		t.Fatal("make original source fact:", err)
	}
	changedRow := *originalRow
	changedRow.RawIncome = "-1.25"
	lateRow := &income.Income{Symbol: identity.Symbol, IncomeType: "FUNDING_FEE", RawIncome: "0.2", Asset: identity.Asset, TransactionID: 102, TradeTime: from.Add(2 * time.Minute)}
	facts, err := diffFundingRecoveryIncomeHistory("episode-anomaly", identity, from, through, []*income.Income{&changedRow, lateRow}, originals)
	if err != nil || len(facts) != 2 {
		t.Fatalf("diff late and changed rows: facts=%+v err=%v", facts, err)
	}
	byID := map[string]accounting.RecoverySourceAnomalyFact{}
	for _, fact := range facts {
		byID[fact.ExternalID] = fact
	}
	changed := byID["101"]
	if changed.Kind != accounting.RecoverySourceAnomalyChanged || changed.DeltaAmount != "-0.249999999999999999" || changed.OriginalEvent.Amount != originalRow.RawIncome {
		t.Fatalf("changed-row exact delta/original not preserved: %+v", changed)
	}
	late := byID["102"]
	if late.Kind != accounting.RecoverySourceAnomalyLate || late.DeltaAmount != "0.2" || late.OriginalEvent != nil {
		t.Fatalf("late row was not represented as an additive fact: %+v", late)
	}
	observed, err := makeFundingRecoveryIncomeEvents("episode-anomaly", identity, "historical-verification", []*income.Income{&changedRow, lateRow})
	if err != nil {
		t.Fatal("make observed source facts:", err)
	}
	adjustments := make([]accounting.RecoveryEvent, 0, len(facts))
	for _, fact := range facts {
		observedEvent := byID[fact.ExternalID].ObservedEvent
		adjustment := accounting.RecoveryEvent{ID: "adjustment-event:" + fact.ExternalID, EpisodeID: "episode-anomaly", Source: accounting.RecoveryManualFundingAdjustmentSource,
			Exchange: identity.Exchange, Market: identity.Market, AccountScope: identity.AccountScope, Symbol: identity.Symbol,
			Kind: accounting.RecoveryFundingEvent, ExternalID: "adjustment:" + fact.ExternalID, RelatedExternalID: fact.ExternalID,
			Asset: identity.Asset, Amount: fact.DeltaAmount, SourceRevision: "anomaly:1", SourceHash: strings.Repeat("b", 64), ObservedAt: through,
			FundingAdjustment: &accounting.RecoveryFundingAdjustmentEvidence{AnomalyID: strings.Repeat("a", 64), OriginalExternalID: fact.ExternalID,
				OriginalSourceHash: sourceHash(fact.OriginalEvent), ObservedSourceHash: observedEvent.SourceHash, OriginalAmountRaw: amount(fact.OriginalEvent),
				ObservedAmountRaw: observedEvent.Amount, ObservedRowAt: observedEvent.ObservedAt, DeltaAmountRaw: fact.DeltaAmount,
				EvidenceHash: strings.Repeat("b", 64), Actor: "operator", AuditRef: "incident-101", ConfirmedAt: through}}
		adjustments = append(adjustments, adjustment)
	}
	ledger := append(append([]accounting.RecoveryEvent(nil), originals...), adjustments...)
	effective, err := effectiveFundingRecoveryHistoryEvents("episode-anomaly", identity, from, through, ledger)
	if err != nil {
		t.Fatal("reconstruct append-only effective funding history:", err)
	}
	if err := verifyFundingRecoveryIncomeHistory("episode-anomaly", identity, from, through, []*income.Income{&changedRow, lateRow}, ledger); err != nil {
		t.Fatalf("exact adjustment ledger failed full-history verification: %v", err)
	}
	if len(originals) != 1 || originals[0].Amount != originalRow.RawIncome || len(observed) != 2 || len(effective) != 2 {
		t.Fatalf("source facts were overwritten or not reconstructed: originals=%+v observed=%+v effective=%+v", originals, observed, effective)
	}
}

func sourceHash(event *accounting.RecoveryEvent) string {
	if event == nil {
		return ""
	}
	return event.SourceHash
}

func amount(event *accounting.RecoveryEvent) string {
	if event == nil {
		return ""
	}
	return event.Amount
}
