package order

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"quantmesh/execution"
)

func settledCapitalReleaseFixture(cid string) persistedIntent {
	return persistedIntent{
		Version: 1, Scope: journalScope(), Settled: true,
		Request: OrderRequest{Symbol: "BTCUSDT", Side: "SELL", Price: 100, Quantity: 1, ReduceOnly: true, ClientOrderID: cid},
		Order:   &Order{OrderID: 17, ClientOrderID: cid, Symbol: "BTCUSDT", Side: "SELL", Status: "FILLED", Quantity: 1, ExecutedQty: 1},
	}
}

func saveCapitalReleaseFixture(t *testing.T, journal *memoryIntentJournal, p persistedIntent) {
	t.Helper()
	payload, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	key, err := journalScope().Key()
	if err != nil {
		t.Fatal(err)
	}
	if err := journal.SaveExecutionIntent(t.Context(), key, p.Request.ClientOrderID, 0, payload); err != nil {
		t.Fatal(err)
	}
}

func TestCapitalReleaseIntentsFreshDurableProof(t *testing.T) {
	for _, tc := range []struct {
		name      string
		edit      func(*persistedIntent)
		wantError bool
	}{
		{"settled fill", func(*persistedIntent) {}, false},
		{"known refusal", func(p *persistedIntent) { p.Rejected = true; p.Settled = false; p.Order = nil }, false},
		{"prepared", func(p *persistedIntent) { p.Settled = false; p.Order = nil }, true},
		{"terminal unaccounted", func(p *persistedIntent) { p.Settled = false }, true},
		{"unknown", func(p *persistedIntent) { p.Unknown = true }, true},
		{"trade ledger pending", func(p *persistedIntent) { p.LedgerPending = true }, true},
		{"refusal ledger pending", func(p *persistedIntent) { p.Rejected = true; p.Order = nil; p.LedgerPending = true }, true},
		{"wrong owner", func(p *persistedIntent) { p.Scope.Bot = "another-bot" }, true},
		{"wrong order identity", func(p *persistedIntent) { p.Order.ClientOrderID = "another-cid" }, true},
		{"nonterminal settled", func(p *persistedIntent) { p.Order.Status = "NEW" }, true},
		{"overfill", func(p *persistedIntent) { p.Order.ExecutedQty = 2 }, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			oe, _, _ := newOwnedTestExecutor()
			journal := &memoryIntentJournal{}
			if err := oe.ConfigureIntentJournal(t.Context(), journal, journalScope()); err != nil {
				t.Fatal(err)
			}
			p := settledCapitalReleaseFixture("durable-only")
			tc.edit(&p)
			// Written AFTER startup; the owner's local map remains empty.
			saveCapitalReleaseFixture(t, journal, p)
			err := oe.VerifyCapitalReleaseIntents(t.Context())
			if (err != nil) != tc.wantError {
				t.Fatalf("verification error=%v, wantError=%v", err, tc.wantError)
			}
			if len(oe.intents) != 0 || journal.writes != 1 {
				t.Fatal("verification mutated runtime or durable intents")
			}
		})
	}
}

func TestCapitalReleaseIntentsRequiresEvidence(t *testing.T) {
	oe, _, _ := newOwnedTestExecutor()
	if err := oe.VerifyCapitalReleaseIntents(t.Context()); err == nil {
		t.Fatal("memory-only executor authorized release")
	}
	journal := &memoryIntentJournal{}
	if err := oe.ConfigureIntentJournal(t.Context(), journal, journalScope()); err != nil {
		t.Fatal(err)
	}
	if err := oe.VerifyCapitalReleaseIntents(t.Context()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := oe.VerifyCapitalReleaseIntents(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}
	journal.failRead = true
	if err := oe.VerifyCapitalReleaseIntents(t.Context()); err == nil {
		t.Fatal("storage outage authorized release")
	}
	journal.failRead = false
	oe.intents["local-only"] = &ownedIntent{unknown: true}
	if err := oe.VerifyCapitalReleaseIntents(t.Context()); !errors.Is(err, execution.ErrOrderUnknown) {
		t.Fatalf("local hold ignored: %v", err)
	}
	delete(oe.intents, "local-only")
	// A full first page must not hide a durable hold on the next page.
	for i := 0; i <= intentJournalPageSize; i++ {
		p := settledCapitalReleaseFixture(fmt.Sprintf("page-%d", i))
		if i == intentJournalPageSize {
			p.Settled = false
		}
		saveCapitalReleaseFixture(t, journal, p)
	}
	if err := oe.VerifyCapitalReleaseIntents(t.Context()); !errors.Is(err, execution.ErrOrderUnknown) {
		t.Fatalf("later-page hold ignored: %v", err)
	}
}

type capitalReleaseValueVenue struct {
	*ownedTestVenue
	marker interface{}
}

func TestCapitalReleaseOwnerRejectsDynamicallyUncomparableClient(t *testing.T) {
	oe, venue, _ := newOwnedTestExecutor()
	// The struct type is comparable, but its interface contains a slice.
	// Comparing the two exchange interfaces directly would panic.
	valueVenue := capitalReleaseValueVenue{ownedTestVenue: venue, marker: []string{"state"}}
	oe.exchange = valueVenue
	if err := oe.ConfigureIntentJournal(t.Context(), &memoryIntentJournal{}, journalScope()); err != nil {
		t.Fatal(err)
	}
	if err := oe.VerifyCapitalReleaseOwner(t.Context(), journalScope(), valueVenue); err == nil {
		t.Fatal("uncomparable exchange instance was treated as verified identity")
	}
}

type capitalReleaseReadJournal struct {
	*memoryIntentJournal
	read func(context.Context) ([]execution.IntentJournalRecord, error)
}

func (j *capitalReleaseReadJournal) LoadExecutionIntents(ctx context.Context, _ string, _ int64, _ int) ([]execution.IntentJournalRecord, error) {
	return j.read(ctx)
}

func TestCapitalReleaseIntentsRejectsIncompletePages(t *testing.T) {
	for _, scenario := range []string{"oversized", "zero cursor", "zero revision", "duplicate CID", "malformed payload", "cancelled read"} {
		t.Run(scenario, func(t *testing.T) {
			oe, _, _ := newOwnedTestExecutor()
			journal := &memoryIntentJournal{}
			if err := oe.ConfigureIntentJournal(t.Context(), journal, journalScope()); err != nil {
				t.Fatal(err)
			}
			p := settledCapitalReleaseFixture("settled")
			saveCapitalReleaseFixture(t, journal, p)
			key, _ := journalScope().Key()
			page, err := journal.LoadExecutionIntents(t.Context(), key, 0, intentJournalPageSize)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			oe.intentJournal = &capitalReleaseReadJournal{memoryIntentJournal: journal, read: func(context.Context) ([]execution.IntentJournalRecord, error) {
				switch scenario {
				case "oversized":
					return make([]execution.IntentJournalRecord, intentJournalPageSize+1), nil
				case "zero cursor":
					page[0].ID = 0
				case "zero revision":
					page[0].Revision = 0
				case "duplicate CID":
					duplicate := page[0]
					duplicate.ID++
					page = append(page, duplicate)
				case "malformed payload":
					page[0].Payload = []byte("invalid JSON")
				case "cancelled read":
					cancel()
					return nil, nil
				}
				return page, nil
			}}
			if err := oe.VerifyCapitalReleaseIntents(ctx); err == nil {
				t.Fatal("incomplete/cancelled journal read authorized release")
			}
		})
	}
}
