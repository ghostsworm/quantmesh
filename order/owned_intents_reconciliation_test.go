package order

import (
	"encoding/json"
	"testing"

	"quantmesh/exchange"
	"quantmesh/execution"
	"quantmesh/lock"
)

func loadReconciliationFixture(t *testing.T, request OrderRequest, order *Order, unknown, ledgerPending, settled bool) (*ExchangeOrderExecutor, *memoryIntentJournal) {
	t.Helper()
	journal := &memoryIntentJournal{}
	scope := journalScope()
	payload, err := json.Marshal(persistedIntent{
		Version: 1, Scope: scope, Request: request, Opening: !request.ReduceOnly,
		Order: order, Unknown: unknown, LedgerPending: ledgerPending,
		LedgerPayload: func() json.RawMessage {
			if ledgerPending {
				return json.RawMessage(`{"trade":"pending"}`)
			}
			return nil
		}(), Settled: settled,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := journal.SaveExecutionIntent(t.Context(), mustScopeKey(t, scope), request.ClientOrderID, 0, payload); err != nil {
		t.Fatal(err)
	}
	venue := &ownedTestVenue{orders: make(map[int64]*exchange.Order)}
	executor := NewExchangeOrderExecutor(venue, request.Symbol, 0, 0, lock.NewNopLock(), "bot-a")
	if err := executor.ConfigureIntentJournal(t.Context(), journal, scope); err != nil && !executor.LoadedIntentRecoveryRequired(err) {
		t.Fatal(err)
	}
	return executor, journal
}

func mustScopeKey(t *testing.T, scope execution.IntentScope) string {
	t.Helper()
	key, err := scope.Key()
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func reconciliationFixtureValues() (OrderRequest, *Order) {
	request := OrderRequest{
		Symbol: "BTCUSDT", Side: "BUY", Price: 100, Quantity: 2, ClientOrderID: "qm-grid-1",
		StrategyName: "Grid-BTCUSDT", StrategyType: "grid", PositionSide: "LONG", OrderSource: "normal", ExposureKey: "slot-1",
	}
	order := &Order{
		OrderID: 41, ClientOrderID: request.ClientOrderID, Symbol: request.Symbol,
		Side: request.Side, Quantity: request.Quantity, ExecutedQty: 1.25,
		Status: "CANCELED",
	}
	return request, order
}

func TestReadOwnedIntentSnapshotRequiresExactUniqueIdentity(t *testing.T) {
	request, venueOrder := reconciliationFixtureValues()
	executor, _ := loadReconciliationFixture(t, request, venueOrder, true, false, false)
	for _, tc := range []struct {
		name string
		id   int64
		cid  string
	}{
		{name: "wrong venue order ID", id: 42, cid: request.ClientOrderID},
		{name: "wrong canonical client order ID", id: venueOrder.OrderID, cid: "other-cid"},
		{name: "missing order ID", id: 0, cid: request.ClientOrderID},
		{name: "missing client order ID", id: venueOrder.OrderID},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := executor.ReadOwnedIntentSnapshot(tc.id, tc.cid); err == nil {
				t.Fatal("expected exact identity check to reject request")
			}
		})
	}
}

func TestReadOwnedIntentSnapshotRejectsAmbiguousVenueOrderID(t *testing.T) {
	request, venueOrder := reconciliationFixtureValues()
	executor, _ := loadReconciliationFixture(t, request, venueOrder, true, false, false)
	otherRequest := request
	otherRequest.ClientOrderID = "qm-grid-2"
	otherOrder := *venueOrder
	otherOrder.ClientOrderID = otherRequest.ClientOrderID
	executor.intents[otherRequest.ClientOrderID] = &ownedIntent{
		request: otherRequest, opening: true, order: &otherOrder, revision: 2,
	}
	if _, err := executor.ReadOwnedIntentSnapshot(venueOrder.OrderID, request.ClientOrderID); err == nil {
		t.Fatal("expected duplicate venue order identity to be rejected")
	}
}

func TestReadOwnedIntentSnapshotRequiresLoadedJournal(t *testing.T) {
	request, venueOrder := reconciliationFixtureValues()
	executor := NewExchangeOrderExecutor(&ownedTestVenue{orders: make(map[int64]*exchange.Order)}, request.Symbol, 0, 0, lock.NewNopLock(), "bot-a")
	executor.intents = map[string]*ownedIntent{request.ClientOrderID: {
		request: request, opening: true, order: venueOrder, revision: 1,
	}}
	if _, err := executor.ReadOwnedIntentSnapshot(venueOrder.OrderID, request.ClientOrderID); err == nil {
		t.Fatal("expected unloaded journal to be rejected")
	}
}

func TestReadOwnedIntentSnapshotReturnsRecoveryAndTerminalStateWithoutMutation(t *testing.T) {
	for _, tc := range []struct {
		name          string
		unknown       bool
		ledgerPending bool
		settled       bool
	}{
		{name: "unknown", unknown: true},
		{name: "ledger pending", ledgerPending: true},
		{name: "settled terminal", settled: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request, venueOrder := reconciliationFixtureValues()
			executor, journal := loadReconciliationFixture(t, request, venueOrder, tc.unknown, tc.ledgerPending, tc.settled)
			writesBefore := journal.writes
			got, err := executor.ReadOwnedIntentSnapshot(venueOrder.OrderID, request.ClientOrderID)
			if err != nil {
				t.Fatal(err)
			}
			if got.BotID != "bot-a" || got.Symbol != request.Symbol || got.StrategyName != request.StrategyName ||
				got.StrategyType != request.StrategyType || got.Side != request.Side || got.PositionSide != request.PositionSide ||
				got.OrderSource != request.OrderSource || got.ExposureKey != request.ExposureKey || got.ReduceOnly != request.ReduceOnly || !got.Opening ||
				got.RequestedQuantity != request.Quantity || got.VenueOrderID != venueOrder.OrderID ||
				got.ClientOrderID != request.ClientOrderID || got.VenueStatus != "CANCELED" || !got.Terminal ||
				got.ExecutedQuantity != venueOrder.ExecutedQty || got.Revision != 1 || got.Unknown != (tc.unknown || tc.ledgerPending) ||
				got.LedgerPending != tc.ledgerPending || got.Settled != tc.settled || got.Rejected {
				t.Fatalf("unexpected durable owner snapshot: %+v", got)
			}
			if journal.writes != writesBefore || executor.intents[request.ClientOrderID].unknown != (tc.unknown || tc.ledgerPending) ||
				executor.intents[request.ClientOrderID].ledgerPending != tc.ledgerPending ||
				executor.intents[request.ClientOrderID].settled != tc.settled {
				t.Fatal("snapshot read mutated durable or in-memory intent state")
			}
		})
	}
}

func TestReadOwnedIntentSnapshotRejectsConflictingPersistedFields(t *testing.T) {
	request, venueOrder := reconciliationFixtureValues()
	venueOrder.Side = "SELL"
	executor, _ := loadReconciliationFixture(t, request, venueOrder, true, false, false)
	if _, err := executor.ReadOwnedIntentSnapshot(venueOrder.OrderID, request.ClientOrderID); err == nil {
		t.Fatal("expected conflicting venue/order fields to be rejected")
	}
}
