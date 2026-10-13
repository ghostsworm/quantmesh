package accounting

import (
	"strings"
	"testing"
	"time"
)

func TestRecoveryFundingAmountDeltaRawIsExact(t *testing.T) {
	tests := []struct {
		name, original, observed, want string
	}{
		{name: "late row", observed: "-0.00000003", want: "-0.00000003"},
		{name: "changed negative amount", original: "-1.20", observed: "-1.35", want: "-0.15"},
		{name: "deleted negative amount", original: "-2.4", want: "2.4"},
		{name: "positive correction", original: "0.000000000000000001", observed: "0.000000000000000003", want: "0.000000000000000002"},
		{name: "metadata-only correction", original: "4.125", observed: "4.125", want: "0"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := RecoveryFundingAmountDeltaRaw(tt.original, tt.observed)
			if err != nil || got != tt.want {
				t.Fatalf("delta(%q, %q) = %q, %v; want %q", tt.original, tt.observed, got, err, tt.want)
			}
		})
	}
}

func TestRecoveryFundingAmountDeltaRejectsNonDecimalInput(t *testing.T) {
	for _, tt := range []struct{ original, observed string }{
		{original: "NaN", observed: "1"},
		{original: "1.0", observed: "1e-8"},
		{original: "-1", observed: "--2"},
	} {
		if _, err := RecoveryFundingAmountDeltaRaw(tt.original, tt.observed); err == nil {
			t.Fatalf("accepted non-decimal source values original=%q observed=%q", tt.original, tt.observed)
		}
	}
}

func TestRecoverySourceAnomalyValidatesImmutableSourceFactsAndDelta(t *testing.T) {
	identity := RecoverySourceIdentity{Source: RecoverySourceBinanceFuturesIncomeV1, Exchange: "binance", Market: "futures",
		AccountScope: strings.Repeat("a", 64), Symbol: "BTCUSDT", Kind: RecoveryFundingEvent, Asset: "USDT"}
	observedAt := time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC)
	original := RecoveryEvent{ID: "original", Source: identity.Source, Exchange: identity.Exchange, Market: identity.Market,
		AccountScope: identity.AccountScope, Symbol: identity.Symbol, Kind: identity.Kind, ExternalID: "trade-1", EpisodeID: "episode-1",
		Asset: identity.Asset, Amount: "-1.25", SourceRevision: "1", SourceHash: strings.Repeat("b", 64), ObservedAt: observedAt}
	observed := original
	observed.ID, observed.Amount, observed.SourceRevision, observed.SourceHash = "observed", "-1.5", "2", strings.Repeat("c", 64)
	delta, err := RecoveryFundingAmountDelta(&original, &observed)
	if err != nil {
		t.Fatal(err)
	}
	anomaly := RecoverySourceAnomaly{ID: "anomaly-1", EpisodeID: "episode-1", RecoveryGeneration: 1, Identity: identity,
		CursorRevision: 1, InitialFrom: observedAt.Add(-time.Hour), Through: observedAt.Add(time.Hour),
		ExpectedHistoryHash: strings.Repeat("d", 64), ObservedHistoryHash: strings.Repeat("e", 64),
		Facts:    []RecoverySourceAnomalyFact{{Kind: RecoverySourceAnomalyChanged, ExternalID: "trade-1", OriginalEvent: &original, ObservedEvent: &observed, DeltaAmount: delta}},
		Revision: 1, State: RecoverySourceAnomalyOpen, DetectedActor: "operator", DetectedAuditRef: "ticket-1", DetectedAt: observedAt.Add(time.Hour)}
	anomaly.EpisodeRevision = 2
	anomaly.ID, err = anomaly.Fingerprint()
	if err != nil {
		t.Fatal("fingerprint source anomaly:", err)
	}
	if err := anomaly.Validate(); err != nil {
		t.Fatalf("valid anomaly rejected: %v", err)
	}
	anomaly.Facts[0].DeltaAmount = "0.24"
	if err := anomaly.Validate(); err == nil {
		t.Fatal("accepted a delta that does not exactly equal observed minus original")
	}
}

func TestRecoveryFundingAdjustmentEvidenceRequiresExactDeltaAndAudit(t *testing.T) {
	evidence := RecoveryFundingAdjustmentEvidence{AnomalyID: "anomaly-1", OriginalExternalID: "trade-1",
		OriginalSourceHash: strings.Repeat("b", 64), ObservedSourceHash: strings.Repeat("c", 64),
		OriginalAmountRaw: "-1.25", ObservedAmountRaw: "-1.5", ObservedRowAt: time.Now().UTC(), DeltaAmountRaw: "-0.25",
		EvidenceHash: strings.Repeat("d", 64), Actor: "operator", AuditRef: "ticket-1", ConfirmedAt: time.Now().UTC()}
	if err := evidence.Validate(); err != nil {
		t.Fatalf("valid funding adjustment evidence rejected: %v", err)
	}
	evidence.DeltaAmountRaw = "-0.24"
	if err := evidence.Validate(); err == nil {
		t.Fatal("accepted an adjustment delta inconsistent with exact raw amounts")
	}
}

func TestRecoveryEventManualFundingAdjustmentBindsOriginalAndAuditEvidence(t *testing.T) {
	evidence := RecoveryFundingAdjustmentEvidence{AnomalyID: "anomaly-1", OriginalExternalID: "trade-1",
		OriginalSourceHash: strings.Repeat("b", 64), ObservedSourceHash: strings.Repeat("c", 64),
		OriginalAmountRaw: "-1.25", ObservedAmountRaw: "-1.5", ObservedRowAt: time.Now().UTC(), DeltaAmountRaw: "-0.25",
		EvidenceHash: strings.Repeat("d", 64), Actor: "operator", AuditRef: "ticket-1", ConfirmedAt: time.Now().UTC()}
	event := RecoveryEvent{ID: "adjustment-event-1", Source: RecoveryManualFundingAdjustmentSource, Exchange: "binance", Market: "futures",
		AccountScope: strings.Repeat("a", 64), Symbol: "BTCUSDT", Kind: RecoveryFundingEvent, ExternalID: "adjustment:anomaly-1:trade-1",
		RelatedExternalID: "trade-1", EpisodeID: "episode-1", Asset: "USDT", Amount: "-0.25", SourceRevision: "anomaly-1:1",
		SourceHash: evidence.EvidenceHash, ObservedAt: evidence.ConfirmedAt, FundingAdjustment: &evidence}
	if err := event.Validate(); err != nil {
		t.Fatalf("valid manual funding adjustment event rejected: %v", err)
	}
	event.RelatedExternalID = "another-trade"
	if err := event.Validate(); err == nil {
		t.Fatal("accepted an adjustment detached from the original source row")
	}
}
