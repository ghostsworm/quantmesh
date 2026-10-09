package order

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"quantmesh/exchange"
	"quantmesh/execution"
	"quantmesh/lock"
)

type expiryAtSubmissionJournal struct {
	*memoryIntentJournal
	healthy     *atomic.Bool
	writes      int
	expireWrite int
}

func (j *expiryAtSubmissionJournal) SaveExecutionIntent(ctx context.Context, scope, cid string, revision int64, payload []byte) error {
	err := j.memoryIntentJournal.SaveExecutionIntent(ctx, scope, cid, revision, payload)
	j.writes++
	expireWrite := j.expireWrite
	if expireWrite == 0 {
		expireWrite = 2
	}
	if j.writes == expireWrite {
		j.healthy.Store(false)
	}
	return err
}

type healthRetryVenue struct {
	*ownedTestVenue
	firstError error
	calls      int
	healthy    *atomic.Bool
	ambiguous  bool
}

func (v *healthRetryVenue) PlaceOrder(ctx context.Context, req *exchange.OrderRequest) (*exchange.Order, error) {
	v.calls++
	if v.calls == 1 {
		if v.ambiguous {
			v.healthy.Store(false)
		}
		return nil, v.firstError
	}
	return v.ownedTestVenue.PlaceOrder(ctx, req)
}

func (*healthRetryVenue) GetPriceDecimals() int { return 2 }

func TestHealthExpiryAtRetryJournalPreservesSubmissionOutcome(t *testing.T) {
	for _, mode := range []string{"rate_open", "postonly_open", "rate_close", "postonly_close", "ambiguous_open"} {
		t.Run(mode, func(t *testing.T) {
			var healthy atomic.Bool
			healthy.Store(true)
			postOnly := mode == "postonly_open" || mode == "postonly_close"
			closeOrder := mode == "rate_close" || mode == "postonly_close"
			ambiguous := mode == "ambiguous_open"
			firstError := errors.New("code=-1003 rate limit")
			if postOnly {
				firstError = errors.New("code=-5022 Post Only order will be rejected")
			}
			if ambiguous {
				firstError = errors.New("transport acknowledgement lost")
			}
			venue := &healthRetryVenue{ownedTestVenue: &ownedTestVenue{orders: make(map[int64]*exchange.Order)}, firstError: firstError, healthy: &healthy, ambiguous: ambiguous}
			oe := NewExchangeOrderExecutor(venue, "BTCUSDT", 0, 0, lock.NewNopLock(), "bot-a")
			var gate execution.OpeningGate
			gate.SetAdmissionCheck(healthy.Load)
			oe.SetOpeningGate(&gate, "LONG")
			journal := &expiryAtSubmissionJournal{memoryIntentJournal: &memoryIntentJournal{}, healthy: &healthy, expireWrite: 3}
			if err := oe.ConfigureIntentJournal(t.Context(), journal, journalScope()); err != nil {
				t.Fatal(err)
			}
			placed, err := oe.PlaceOrder(&OrderRequest{Symbol: "BTCUSDT", Side: "BUY", Price: 100, Quantity: 1, PriceDecimals: 2, ReduceOnly: closeOrder, PostOnly: postOnly, ClientOrderID: "retry-health"})
			if healthy.Load() {
				t.Fatal("fixture never invalidated observation")
			}
			if closeOrder {
				if err != nil || placed == nil || venue.calls != 2 {
					t.Fatalf("protective retry blocked: calls=%d error=%v", venue.calls, err)
				}
				return
			}
			if placed != nil || venue.calls != 1 {
				t.Fatal("unhealthy opening was resent")
			}
			restarted := NewExchangeOrderExecutor(venue, "BTCUSDT", 0, 0, lock.NewNopLock(), "bot-a")
			restoreErr := restarted.ConfigureIntentJournal(t.Context(), journal, journalScope())
			if ambiguous {
				intents := oe.snapshotOwnedIntents()
				if !errors.Is(err, execution.ErrOrderUnknown) || len(intents) != 1 || !intents[0].unknown || !errors.Is(restoreErr, execution.ErrOrderUnknown) {
					t.Fatalf("ambiguous submission was released: submit=%v restore=%v", err, restoreErr)
				}
			} else if !errors.Is(err, execution.ErrOpeningPaused) || len(oe.snapshotOwnedIntents()) != 0 || restoreErr != nil {
				t.Fatalf("known unsubmitted retry became unknown: submit=%v restore=%v", err, restoreErr)
			}
		})
	}
}

func TestHealthExpiryDuringSubmissionJournalBlocksOnlyOpening(t *testing.T) {
	for _, closeOrder := range []bool{false, true} {
		name := "opening"
		if closeOrder {
			name = "protective_close"
		}
		t.Run(name, func(t *testing.T) {
			oe, venue, gate := newOwnedTestExecutor()
			var healthy atomic.Bool
			healthy.Store(true)
			gate.SetAdmissionCheck(healthy.Load)
			journal := &expiryAtSubmissionJournal{memoryIntentJournal: &memoryIntentJournal{}, healthy: &healthy}
			if err := oe.ConfigureIntentJournal(t.Context(), journal, journalScope()); err != nil {
				t.Fatal(err)
			}
			placed, err := oe.PlaceOrder(&OrderRequest{Symbol: "BTCUSDT", Side: "BUY", Price: 100, Quantity: 1, ReduceOnly: closeOrder, ClientOrderID: "expiry-at-journal"})
			if healthy.Load() {
				t.Fatal("submission journal did not invalidate observation")
			}
			if closeOrder {
				if err != nil || placed == nil || venue.nextID != 1 {
					t.Fatalf("protective close blocked: %v", err)
				}
				return
			}
			if !errors.Is(err, execution.ErrOpeningPaused) || placed != nil || venue.nextID != 0 {
				t.Fatalf("expired observation reached venue after journal: placed=%v calls=%d error=%v", placed, venue.nextID, err)
			}
			if len(oe.snapshotOwnedIntents()) != 0 {
				t.Fatal("unsubmitted order remained UNKNOWN")
			}
			restarted := NewExchangeOrderExecutor(venue, "BTCUSDT", 0, 0, lock.NewNopLock(), "bot-a")
			if err := restarted.ConfigureIntentJournal(context.Background(), journal, journalScope()); err != nil {
				t.Fatalf("durable unsubmitted rejection could not restart: %v", err)
			}
		})
	}
}
