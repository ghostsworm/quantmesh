package daily

import (
	"strings"
	"testing"
	"time"

	"quantmesh/exchange/accounting"
)

func TestBuildFuturesWalletEvidenceKeepsTheDayIncomplete(t *testing.T) {
	start := time.Date(2026, 11, 4, 16, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)
	observed := end.Add(time.Hour)
	in := FuturesEvidenceImport{
		Query:    Query{Exchange: "Binance", AccountScope: "scope-1", Date: "2026-11-05"},
		Revision: 3,
		Now:      observed.Add(time.Minute),
		Snapshot: accounting.Snapshot{
			Currency:   "USDT",
			ObservedAt: observed,
			Wallet: accounting.Wallet{
				Balance: "100.5",
				From:    start.Add(-time.Hour),
				Through: end,
			},
			Entries: []accounting.Entry{
				{ID: "TRANSFER:1", Kind: "transfer_in", Currency: "USDT", Amount: "10", At: start.Add(time.Hour)},
				{ID: "FUNDING_FEE:2", Kind: "funding", Currency: "USDT", Amount: "-0.25", Symbol: "BTCUSDT", At: start.Add(2 * time.Hour)},
				{ID: "OLD", Kind: "funding", Currency: "USDT", Amount: "1", At: start.Add(-time.Minute)},
			},
		},
	}

	evidence, reasons, err := BuildFuturesWalletEvidence(in)
	if err != nil {
		t.Fatalf("build futures evidence: %v", err)
	}
	if evidence.Scope.Exchange != "binance" || evidence.Scope.AccountScope != "scope-1" {
		t.Fatalf("scope was not canonicalized: %+v", evidence.Scope)
	}
	if evidence.Opening.Complete || evidence.Closing.Complete {
		t.Fatal("candidate sample must not become a complete day endpoint")
	}
	if evidence.Opening.SourceID != FuturesEvidenceSource || evidence.Closing.SourceID != FuturesEvidenceSource {
		t.Fatalf("source id = %q/%q", evidence.Opening.SourceID, evidence.Closing.SourceID)
	}
	if len(evidence.Closing.Balances) != 1 || evidence.Closing.Balances[0].Wallet != WalletFutures {
		t.Fatalf("closing balances = %+v", evidence.Closing.Balances)
	}
	if len(evidence.Opening.Balances) != 0 {
		t.Fatalf("opening balances must stay empty, got %+v", evidence.Opening.Balances)
	}
	if len(evidence.Events) != 2 {
		t.Fatalf("events outside the day must be dropped, got %+v", evidence.Events)
	}
	if !hasReason(reasons, ReasonFuturesEvidenceCandidate) ||
		!hasReason(reasons, ReasonWalletEvidenceMissing+":"+WalletSpot) ||
		!hasReason(reasons, ReasonWalletEvidenceMissing+":"+WalletMargin) {
		t.Fatalf("missing incompleteness reasons: %v", reasons)
	}
}

func TestBuildFuturesWalletEvidenceRejectsUnclassifiedIncome(t *testing.T) {
	start := time.Date(2026, 11, 4, 16, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)
	_, _, err := BuildFuturesWalletEvidence(FuturesEvidenceImport{
		Query: Query{Exchange: "binance", AccountScope: "scope-1", Date: "2026-11-05"},
		Now:   end.Add(time.Hour),
		Snapshot: accounting.Snapshot{
			Currency:   "USDT",
			ObservedAt: end.Add(time.Minute),
			Wallet:     accounting.Wallet{Balance: "1", From: start, Through: end},
			Entries:    []accounting.Entry{{ID: "X:1", Kind: "mystery", Currency: "USDT", Amount: "1", At: start.Add(time.Hour)}},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "not classified") {
		t.Fatalf("expected unclassified income to fail closed, got %v", err)
	}
}

func TestBuildFuturesWalletEvidenceRejectsAnOpenDay(t *testing.T) {
	start := time.Date(2026, 11, 4, 16, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)
	_, _, err := BuildFuturesWalletEvidence(FuturesEvidenceImport{
		Query: Query{Exchange: "binance", AccountScope: "scope-1", Date: "2026-11-05"},
		Now:   end.Add(time.Minute),
		Snapshot: accounting.Snapshot{
			Currency:   "USDT",
			ObservedAt: end.Add(time.Minute),
			Wallet:     accounting.Wallet{Balance: "1", From: start, Through: end},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "safety lag") {
		t.Fatalf("expected an open day to be rejected, got %v", err)
	}
}

func hasReason(reasons []string, want string) bool {
	for _, reason := range reasons {
		if reason == want {
			return true
		}
	}
	return false
}
