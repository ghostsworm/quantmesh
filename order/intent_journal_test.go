package order

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"

	"quantmesh/exchange"
	"quantmesh/execution"
	"quantmesh/lock"
	"quantmesh/utils"
)

type memoryIntentJournal struct {
	mu      sync.Mutex
	records []struct {
		scope  string
		record execution.IntentJournalRecord
	}
	writes        int
	failWrite     int
	failRead      bool
	commitErrorAt int
}

func (m *memoryIntentJournal) SaveExecutionIntent(_ context.Context, scope, cid string, expected int64, payload []byte) (resultErr error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.writes++
	defer func() {
		if resultErr == nil && m.writes == m.commitErrorAt {
			resultErr = errors.New("commit succeeded but acknowledgement lost")
		}
	}()
	if m.writes == m.failWrite {
		return errors.New("injected journal write failure")
	}
	for i := range m.records {
		r := &m.records[i]
		if r.scope != scope || r.record.ClientOrderID != cid {
			continue
		}
		if r.record.Revision != expected {
			return execution.ErrIntentJournalConflict
		}
		r.record.Payload = append([]byte(nil), payload...)
		r.record.Revision++
		return nil
	}
	if expected != 0 {
		return execution.ErrIntentJournalConflict
	}
	m.records = append(m.records, struct {
		scope  string
		record execution.IntentJournalRecord
	}{scope, execution.IntentJournalRecord{ID: int64(len(m.records) + 1), ClientOrderID: cid, Revision: 1, Payload: append([]byte(nil), payload...)}})
	return nil
}

func (m *memoryIntentJournal) LoadExecutionIntents(_ context.Context, scope string, after int64, limit int) ([]execution.IntentJournalRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failRead {
		return nil, errors.New("injected journal read failure")
	}
	var result []execution.IntentJournalRecord
	for _, r := range m.records {
		if r.scope != scope || r.record.ID <= after {
			continue
		}
		copy := r.record
		copy.Payload = append([]byte(nil), copy.Payload...)
		result = append(result, copy)
		if len(result) == limit {
			break
		}
	}
	return result, nil
}

func journalScope() execution.IntentScope {
	return execution.IntentScope{Account: "account-full-digest", Exchange: "fake", Market: "futures", Symbol: "BTCUSDT", Bot: "bot-a"}
}

func TestOwnedIntentIdentityIsScopedToJournalCID(t *testing.T) {
	venue := &ownedTestVenue{orders: make(map[int64]*exchange.Order)}
	oe := NewExchangeOrderExecutor(venue, "BTCUSDT", 0, 0, lock.NewNopLock(), "bot-a")
	const cid = "qm-grid-order"
	oe.intents = make(map[string]*ownedIntent)
	oe.intents[cid] = &ownedIntent{request: OrderRequest{ClientOrderID: cid, StrategyName: "Grid-BTCUSDT", StrategyType: "grid"}}

	canonical, ok := oe.OwnedIntentClientOrderID(cid)
	if !ok || canonical != cid {
		t.Fatalf("canonical ID = %q, ok = %v; want %q, true", canonical, ok, cid)
	}
	name, strategyType, ok := oe.IntentStrategyType(cid)
	if !ok || name != "Grid-BTCUSDT" || strategyType != "grid" {
		t.Fatalf("strategy identity = (%q, %q, %v), want grid identity", name, strategyType, ok)
	}
}

type brokerPrefixedIntentVenue struct{ *ownedTestVenue }

func (*brokerPrefixedIntentVenue) GetName() string { return "binance" }
func (v *brokerPrefixedIntentVenue) GetOrder(ctx context.Context, symbol string, orderID int64) (*exchange.Order, error) {
	order, err := v.ownedTestVenue.GetOrder(ctx, symbol, orderID)
	if err == nil && order != nil {
		order.ClientOrderID = utils.AddBrokerPrefix("binance", order.ClientOrderID)
	}
	return order, err
}

func TestSettleIntentAcceptsExchangeBrokerPrefix(t *testing.T) {
	venue := &brokerPrefixedIntentVenue{ownedTestVenue: &ownedTestVenue{
		orders: map[int64]*exchange.Order{17: {
			OrderID: 17, ClientOrderID: "grid-cancel-1", Symbol: "BTCUSDT", Side: "BUY",
			Status: exchange.OrderStatusCanceled, Quantity: 1,
		}},
	}}
	oe := NewExchangeOrderExecutor(venue, "BTCUSDT", 0, 0, lock.NewNopLock(), "bot-a")
	oe.intents = map[string]*ownedIntent{"grid-cancel-1": {
		request: OrderRequest{Symbol: "BTCUSDT", Side: "BUY", Quantity: 1, ClientOrderID: "grid-cancel-1"},
		order:   &Order{OrderID: 17, ClientOrderID: "grid-cancel-1", Symbol: "BTCUSDT", Side: "BUY", Status: "CANCELED", Quantity: 1},
	}}

	if err := oe.SettleIntent(t.Context(), "grid-cancel-1"); err != nil {
		t.Fatalf("SettleIntent() rejected matching broker-prefixed terminal ID: %v", err)
	}
	if !oe.intents["grid-cancel-1"].settled {
		t.Fatal("verified zero-fill terminal intent was not marked settled")
	}
}

func TestSettleZeroFillIntentRejectsLateVenueFill(t *testing.T) {
	venue := &ownedTestVenue{orders: map[int64]*exchange.Order{18: {
		OrderID: 18, ClientOrderID: "grid-cancel-late-fill", Symbol: "BTCUSDT", Side: "BUY",
		Status: exchange.OrderStatusCanceled, Quantity: 1, ExecutedQty: 0.25, AvgPrice: 100,
	}}}
	oe := NewExchangeOrderExecutor(venue, "BTCUSDT", 0, 0, lock.NewNopLock(), "bot-a")
	oe.intents = map[string]*ownedIntent{"grid-cancel-late-fill": {
		request: OrderRequest{Symbol: "BTCUSDT", Side: "BUY", Quantity: 1, ClientOrderID: "grid-cancel-late-fill"},
		order:   &Order{OrderID: 18, ClientOrderID: "grid-cancel-late-fill", Symbol: "BTCUSDT", Side: "BUY", Status: "CANCELED", Quantity: 1},
	}}

	if err := oe.SettleZeroFillIntent(t.Context(), "grid-cancel-late-fill"); err == nil {
		t.Fatal("zero-fill settlement accepted a terminal order with a late venue fill")
	}
	if oe.intents["grid-cancel-late-fill"].settled {
		t.Fatal("late-filled intent was incorrectly marked settled")
	}
}

type journalCheckedVenue struct {
	*ownedTestVenue
	t       *testing.T
	journal *memoryIntentJournal
}

func (v *journalCheckedVenue) PlaceOrder(ctx context.Context, req *exchange.OrderRequest) (*exchange.Order, error) {
	key, _ := journalScope().Key()
	page, err := v.journal.LoadExecutionIntents(ctx, key, 0, 500)
	if err != nil {
		v.t.Fatal(err)
	}
	var found bool
	for _, r := range page {
		if r.ClientOrderID != req.ClientOrderID {
			continue
		}
		var intent persistedIntent
		if err := json.Unmarshal(r.Payload, &intent); err != nil {
			v.t.Fatal(err)
		}
		found = intent.Attempts > 0 && intent.AttemptPrice == req.Price && intent.Request.Quantity == req.Quantity
	}
	if !found {
		v.t.Fatal("venue called before durable intent/attempt")
	}
	return v.ownedTestVenue.PlaceOrder(ctx, req)
}

func TestIntentJournalPrecedesSubmissionAndFailsClosed(t *testing.T) {
	for _, fail := range []int{0, 1, 2, 3} {
		t.Run(fmt.Sprintf("write-%d", fail), func(t *testing.T) {
			journal := &memoryIntentJournal{failWrite: fail}
			venue := &journalCheckedVenue{ownedTestVenue: &ownedTestVenue{orders: make(map[int64]*exchange.Order)}, journal: journal, t: t}
			oe := NewExchangeOrderExecutor(venue, "BTCUSDT", 0, 0, lock.NewNopLock(), "bot-a")
			if err := oe.ConfigureIntentJournal(t.Context(), journal, journalScope()); err != nil {
				t.Fatal(err)
			}
			_, err := oe.PlaceOrder(&OrderRequest{Symbol: "BTCUSDT", Side: "BUY", Price: 100, Quantity: 1, ClientOrderID: "open"})
			if fail == 0 && err != nil {
				t.Fatal(err)
			}
			if fail > 0 && (err == nil || !oe.IsOpeningPaused()) {
				t.Fatal("journal failure did not stop execution")
			}
			want := int64(1)
			if fail == 1 || fail == 2 {
				want = 0
			}
			if venue.nextID != want {
				t.Fatalf("physical submissions=%d want=%d", venue.nextID, want)
			}
			if fail == 3 && !errors.Is(err, execution.ErrOrderUnknown) {
				t.Fatalf("accepted order misreported as rejected: %v", err)
			}
		})
	}
}

func TestIntentJournalRestartRetainsUnknownCloseAndTerminalFill(t *testing.T) {
	oe, venue, _ := newOwnedTestExecutor()
	journal := &memoryIntentJournal{}
	if err := oe.ConfigureIntentJournal(t.Context(), journal, journalScope()); err != nil {
		t.Fatal(err)
	}
	req := &OrderRequest{Symbol: "BTCUSDT", Side: "SELL", Price: 100, Quantity: 1, ReduceOnly: true, ClientOrderID: "close", StrategyName: "grid"}
	if _, err := oe.PlaceOrder(req); err != nil {
		t.Fatal(err)
	}
	oe.ObserveOrder(&exchange.Order{OrderID: 1, ClientOrderID: "close", Symbol: "BTCUSDT", Side: exchange.SideSell, Quantity: 1, ExecutedQty: 1, Price: 100, Status: exchange.OrderStatusFilled})
	restarted := NewExchangeOrderExecutor(venue, "BTCUSDT", 0, 0, lock.NewNopLock(), "bot-a")
	if err := restarted.ConfigureIntentJournal(t.Context(), journal, journalScope()); !errors.Is(err, execution.ErrOrderUnknown) {
		t.Fatalf("terminal fill bypassed economic recovery: %v", err)
	}
	if !restarted.IsOpeningPaused() {
		t.Fatal("restart lost recovery gate")
	}
	req.ClientOrderID = "second-close"
	if _, err := restarted.PlaceOrder(req); !errors.Is(err, execution.ErrIntentPending) || venue.nextID != 1 {
		t.Fatalf("duplicate close: %v", err)
	}
	routes := restarted.RecoveredOrderRoutes()
	if len(routes) != 1 || routes[0].ClientOrderID != "close" || routes[0].OrderID != 1 {
		t.Fatalf("lost scoped route: %v", routes)
	}
}

func TestSettledIntentAllowsVerifiedRestart(t *testing.T) {
	venue := &ownedTestVenue{orders: make(map[int64]*exchange.Order)}
	journal := &memoryIntentJournal{}
	oe := NewExchangeOrderExecutor(venue, "BTCUSDT", 0, 0, lock.NewNopLock(), "bot-a")
	if err := oe.ConfigureIntentJournal(t.Context(), journal, journalScope()); err != nil {
		t.Fatal(err)
	}
	placed, err := oe.PlaceOrder(&OrderRequest{Symbol: "BTCUSDT", Side: "BUY", Price: 100, Quantity: 1, ClientOrderID: "settled-open"})
	if err != nil {
		t.Fatal(err)
	}
	venue.mu.Lock()
	venue.orders[placed.OrderID].ExecutedQty = 1
	venue.orders[placed.OrderID].AvgPrice = 100
	venue.orders[placed.OrderID].Status = exchange.OrderStatusFilled
	venue.mu.Unlock()
	if err := oe.SettleIntent(t.Context(), placed.ClientOrderID); err != nil {
		t.Fatalf("settle durably accounted fill: %v", err)
	}
	restarted := NewExchangeOrderExecutor(venue, "BTCUSDT", 0, 0, lock.NewNopLock(), "bot-a")
	if err := restarted.ConfigureIntentJournal(t.Context(), journal, journalScope()); err != nil {
		t.Fatalf("restore settled intent: %v", err)
	}
	if restarted.IsOpeningPaused() {
		t.Fatal("settled intent incorrectly held restart gate")
	}
}

func TestIntentJournalRestartReleasesOnlyVerifiedZeroFillTerminalOrders(t *testing.T) {
	tests := []struct {
		name        string
		requestCID  string
		order       *Order
		unknown     bool
		wantBlocked bool
	}{
		{name: "verified canceled without fills", order: &Order{OrderID: 7, ClientOrderID: "cancelled", Symbol: "BTCUSDT", Side: "BUY", Quantity: 1, Status: "CANCELED"}},
		{name: "verified expired without fills", order: &Order{OrderID: 8, ClientOrderID: "expired", Symbol: "BTCUSDT", Side: "BUY", Quantity: 1, Status: "EXPIRED"}},
		{name: "terminal fill remains unresolved", order: &Order{OrderID: 9, ClientOrderID: "filled", Symbol: "BTCUSDT", Side: "BUY", Quantity: 1, ExecutedQty: 1, Status: "FILLED"}, wantBlocked: true},
		{name: "unknown terminal remains unresolved", order: &Order{OrderID: 10, ClientOrderID: "unknown", Symbol: "BTCUSDT", Side: "BUY", Quantity: 1, Status: "CANCELED"}, unknown: true, wantBlocked: true},
		{name: "mismatched terminal identity remains unresolved", requestCID: "expected", order: &Order{OrderID: 11, ClientOrderID: "foreign", Symbol: "BTCUSDT", Side: "BUY", Quantity: 1, Status: "CANCELED"}, wantBlocked: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			journal := &memoryIntentJournal{}
			key, _ := journalScope().Key()
			cid := test.order.ClientOrderID
			if test.requestCID != "" {
				cid = test.requestCID
			}
			p := persistedIntent{Version: 1, Scope: journalScope(), Request: OrderRequest{Symbol: "BTCUSDT", Side: "BUY", Price: 100, Quantity: 1, ClientOrderID: cid}, Opening: true, Order: test.order, Unknown: test.unknown}
			payload, err := json.Marshal(p)
			if err != nil {
				t.Fatal(err)
			}
			if err := journal.SaveExecutionIntent(t.Context(), key, cid, 0, payload); err != nil {
				t.Fatal(err)
			}
			oe, _, _ := newOwnedTestExecutor()
			err = oe.ConfigureIntentJournal(t.Context(), journal, journalScope())
			if test.wantBlocked {
				if !errors.Is(err, execution.ErrOrderUnknown) || !oe.IsOpeningPaused() || len(oe.snapshotOwnedIntents()) != 1 {
					t.Fatalf("unresolved order did not retain its recovery hold: err=%v intents=%d", err, len(oe.snapshotOwnedIntents()))
				}
				return
			}
			if err != nil || oe.IsOpeningPaused() || len(oe.snapshotOwnedIntents()) != 0 {
				t.Fatalf("verified no-fill order blocked restart: err=%v intents=%d", err, len(oe.snapshotOwnedIntents()))
			}
		})
	}
}

func TestIntentJournalRestoresBeyond2000AndRejectsCorruptScope(t *testing.T) {
	journal := &memoryIntentJournal{}
	key, _ := journalScope().Key()
	for i := 0; i < 2101; i++ {
		cid := fmt.Sprintf("prepared-%d", i)
		p := persistedIntent{Version: 1, Scope: journalScope(), Opening: true, Request: OrderRequest{Symbol: "BTCUSDT", Side: "BUY", Price: 100, Quantity: 1, ClientOrderID: cid, StrategyName: "dca"}}
		payload, err := json.Marshal(p)
		if err != nil {
			t.Fatal(err)
		}
		if err := journal.SaveExecutionIntent(t.Context(), key, cid, 0, payload); err != nil {
			t.Fatal(err)
		}
	}
	oe, _, _ := newOwnedTestExecutor()
	if err := oe.ConfigureIntentJournal(t.Context(), journal, journalScope()); !errors.Is(err, execution.ErrOrderUnknown) {
		t.Fatal(err)
	}
	if len(oe.RecoveredOrderRoutes()) != 2101 {
		t.Fatal("restoration truncated at old 2000 limit")
	}
	var corrupt persistedIntent
	if err := json.Unmarshal(journal.records[0].record.Payload, &corrupt); err != nil {
		t.Fatal(err)
	}
	corrupt.Scope.Account = "different-account"
	journal.records[0].record.Payload, _ = json.Marshal(corrupt)
	other, _, _ := newOwnedTestExecutor()
	if err := other.ConfigureIntentJournal(t.Context(), journal, journalScope()); err == nil || len(other.RecoveredOrderRoutes()) != 0 || !other.IsOpeningPaused() {
		t.Fatal("corrupt owner scope accepted")
	}
}

func TestIntentJournalObservationWriteFailureBlocksExecution(t *testing.T) {
	oe, _, _ := newOwnedTestExecutor()
	journal := &memoryIntentJournal{failWrite: 4}
	if err := oe.ConfigureIntentJournal(t.Context(), journal, journalScope()); err != nil {
		t.Fatal(err)
	}
	if _, err := oe.PlaceOrder(&OrderRequest{Symbol: "BTCUSDT", Side: "BUY", Price: 100, Quantity: 1, ClientOrderID: "open"}); err != nil {
		t.Fatal(err)
	}
	if oe.ObserveOrder(&exchange.Order{OrderID: 1, ClientOrderID: "open", Symbol: "BTCUSDT", Quantity: 1, ExecutedQty: 0.5, Price: 100, Status: exchange.OrderStatusPartiallyFilled}) {
		t.Fatal("failed durable observation admitted downstream consumers")
	}
	if !oe.IsOpeningPaused() || !oe.snapshotOwnedIntents()[0].unknown {
		t.Fatal("lost observation did not retain unknown ownership")
	}
}

func TestIntentJournalUncertainCommitIsNotRetriedOrForgotten(t *testing.T) {
	for _, at := range []int{1, 2, 3} {
		t.Run(fmt.Sprintf("commit-%d", at), func(t *testing.T) {
			oe, venue, _ := newOwnedTestExecutor()
			journal := &memoryIntentJournal{commitErrorAt: at}
			if err := oe.ConfigureIntentJournal(t.Context(), journal, journalScope()); err != nil {
				t.Fatal(err)
			}
			_, err := oe.PlaceOrder(&OrderRequest{Symbol: "BTCUSDT", Side: "BUY", Price: 100, Quantity: 1, ClientOrderID: "ambiguous-db", StrategyName: "grid"})
			if err == nil || !oe.IsOpeningPaused() || journal.writes != at {
				t.Fatalf("uncertain commit retried or cleared: %v writes=%d", err, journal.writes)
			}
			want := int64(0)
			if at == 3 {
				want = 1
			}
			if venue.nextID != want {
				t.Fatalf("unexpected venue call count: %d", venue.nextID)
			}
			restarted := NewExchangeOrderExecutor(venue, "BTCUSDT", 0, 0, lock.NewNopLock(), "bot-a")
			if err := restarted.ConfigureIntentJournal(t.Context(), journal, journalScope()); !errors.Is(err, execution.ErrOrderUnknown) {
				t.Fatalf("restart forgot committed intent: %v", err)
			}
			if len(restarted.RecoveredOrderRoutes()) != 1 {
				t.Fatal("committed intent identity lost")
			}
		})
	}
}

func TestIntentJournalRejectsUnavailableOrMismatchedStoreBeforeSubmit(t *testing.T) {
	for _, scenario := range []string{"missing", "read_error", "scope_mismatch"} {
		t.Run(scenario, func(t *testing.T) {
			oe, venue, _ := newOwnedTestExecutor()
			var journal execution.IntentJournal = &memoryIntentJournal{failRead: scenario == "read_error"}
			if scenario == "missing" {
				journal = nil
			}
			scope := journalScope()
			if scenario == "scope_mismatch" {
				scope.Bot = "other"
			}
			if err := oe.ConfigureIntentJournal(t.Context(), journal, scope); err == nil {
				t.Fatal("invalid setup accepted")
			}
			// Closing requests also need a complete identity set to exclude duplicate closes.
			if _, err := oe.PlaceOrder(&OrderRequest{Symbol: "BTCUSDT", Side: "SELL", Price: 100, Quantity: 1, ReduceOnly: true}); err == nil || venue.nextID != 0 {
				t.Fatal("unverified journal allowed physical call")
			}
		})
	}
}

func TestIntentJournalObservationRequiresConsistentOwnedIdentity(t *testing.T) {
	oe, _, _ := newOwnedTestExecutor()
	journal := &memoryIntentJournal{}
	if err := oe.ConfigureIntentJournal(t.Context(), journal, journalScope()); err != nil {
		t.Fatal(err)
	}
	if _, err := oe.PlaceOrder(&OrderRequest{Symbol: "BTCUSDT", Side: "BUY", Price: 100, Quantity: 1, ClientOrderID: "owned"}); err != nil {
		t.Fatal(err)
	}
	update := &exchange.Order{OrderID: 1, Symbol: "BTCUSDT", Side: exchange.SideBuy, Status: exchange.OrderStatusPartiallyFilled, Quantity: 1, ExecutedQty: 0.5}
	if !oe.ObserveOrder(update) {
		t.Fatal("known numeric identity rejected when venue omitted CID")
	}
	if oe.snapshotOwnedIntents()[0].order.ClientOrderID != "owned" {
		t.Fatal("omitted CID erased owned identity")
	}
	update.ClientOrderID = "foreign"
	if oe.ObserveOrder(update) {
		t.Fatal("contradictory foreign CID matched by numeric fallback")
	}
	update.ClientOrderID, update.OrderID = "owned", 2
	if oe.ObserveOrder(update) || !oe.IsOpeningPaused() {
		t.Fatal("identity conflict was delivered or not blocked")
	}
	if oe.snapshotOwnedIntents()[0].order.OrderID != 1 {
		t.Fatal("conflict replaced established identity")
	}
}

func TestIntentJournalOlderAcknowledgementsCannotEraseFills(t *testing.T) {
	oe, _, _ := newOwnedTestExecutor()
	journal := &memoryIntentJournal{}
	if err := oe.ConfigureIntentJournal(t.Context(), journal, journalScope()); err != nil {
		t.Fatal(err)
	}
	req := &OrderRequest{Symbol: "BTCUSDT", Side: "BUY", Price: 100, Quantity: 1, ClientOrderID: "out-of-order"}
	if _, err := oe.PlaceOrder(req); err != nil {
		t.Fatal(err)
	}
	if !oe.ObserveOrder(&exchange.Order{OrderID: 1, ClientOrderID: req.ClientOrderID, Symbol: "BTCUSDT", Quantity: 1, ExecutedQty: 0.5, AvgPrice: 99, Status: exchange.OrderStatusPartiallyFilled}) {
		t.Fatal("partial fill not claimed")
	}
	if err := oe.finishIntent(req, &Order{OrderID: 1, ClientOrderID: req.ClientOrderID, Quantity: 1, Status: "NEW"}, nil); err != nil {
		t.Fatal(err)
	}
	if !oe.ObserveOrder(&exchange.Order{OrderID: 1, ClientOrderID: req.ClientOrderID, Symbol: "BTCUSDT", Status: exchange.OrderStatusNew}) {
		t.Fatal("known old update not claimed")
	}
	key, _ := journalScope().Key()
	page, err := journal.LoadExecutionIntents(t.Context(), key, 0, 500)
	if err != nil {
		t.Fatal(err)
	}
	var saved persistedIntent
	if err := json.Unmarshal(page[0].Payload, &saved); err != nil {
		t.Fatal(err)
	}
	if saved.Order.ExecutedQty != 0.5 || saved.Order.AvgPrice != 99 || saved.Order.Status != "PARTIALLY_FILLED" {
		t.Fatalf("fill evidence erased: %+v", saved.Order)
	}
	if oe.ObserveOrder(&exchange.Order{OrderID: 1, ClientOrderID: req.ClientOrderID, Symbol: "BTCUSDT", Status: exchange.OrderStatusCanceled}) || !oe.IsOpeningPaused() {
		t.Fatal("regressing terminal released execution")
	}
	if oe.snapshotOwnedIntents()[0].order.ExecutedQty != 0.5 {
		t.Fatal("conflicting terminal erased fills")
	}
}

func TestTradeLedgerFailurePersistsIntentReconciliationHoldAcrossRestart(t *testing.T) {
	oe, venue, _ := newOwnedTestExecutor()
	journal := &memoryIntentJournal{}
	if err := oe.ConfigureIntentJournal(t.Context(), journal, journalScope()); err != nil {
		t.Fatal(err)
	}
	if _, err := oe.PlaceOrder(&OrderRequest{Symbol: "BTCUSDT", Side: "SELL", Price: 101, Quantity: 1, ClientOrderID: "ledger-failure"}); err != nil {
		t.Fatal(err)
	}
	if !oe.ObserveOrder(&exchange.Order{OrderID: 1, ClientOrderID: "ledger-failure", Symbol: "BTCUSDT", Side: exchange.SideSell, Quantity: 1, ExecutedQty: 1, AvgPrice: 101, Status: exchange.OrderStatusFilled}) {
		t.Fatal("terminal fill not observed")
	}
	if err := oe.MarkTradeLedgerReconciliationRequired(1, "ledger-failure", "trade storage unavailable"); err != nil {
		t.Fatal(err)
	}
	if oe.snapshotOwnedIntents()[0].unknown || !oe.openingGate.HasBlock("trade_ledger_unverified") {
		t.Fatal("economic ledger hold must not change the known terminal venue-order state")
	}
	page, err := journal.LoadExecutionIntents(t.Context(), func() string { key, _ := journalScope().Key(); return key }(), 0, 10)
	if err != nil || len(page) != 1 {
		t.Fatalf("load persisted ledger hold: rows=%d err=%v", len(page), err)
	}
	var saved persistedIntent
	if err := json.Unmarshal(page[0].Payload, &saved); err != nil || !saved.LedgerPending || saved.LedgerReason != "trade storage unavailable" {
		t.Fatalf("economic hold missing from durable intent: %+v err=%v", saved, err)
	}
	restarted := NewExchangeOrderExecutor(venue, "BTCUSDT", 0, 0, lock.NewNopLock(), "bot-a")
	if err := restarted.ConfigureIntentJournal(t.Context(), journal, journalScope()); !errors.Is(err, execution.ErrOrderUnknown) {
		t.Fatalf("restart did not retain reconciliation hold: %v", err)
	}
	if !restarted.IsOpeningPaused() || len(restarted.snapshotOwnedIntents()) != 1 || !restarted.snapshotOwnedIntents()[0].unknown {
		t.Fatal("restart lost the held order or allowed opening")
	}
}

func TestTradeLedgerRecoveryReplaysDurablePayloadBeforeOpening(t *testing.T) {
	oe, venue, _ := newOwnedTestExecutor()
	journal := &memoryIntentJournal{}
	if err := oe.ConfigureIntentJournal(t.Context(), journal, journalScope()); err != nil {
		t.Fatal(err)
	}
	if _, err := oe.PlaceOrder(&OrderRequest{Symbol: "BTCUSDT", Side: "SELL", Price: 101, Quantity: 1, ClientOrderID: "ledger-replay"}); err != nil {
		t.Fatal(err)
	}
	if !oe.ObserveOrder(&exchange.Order{OrderID: 1, ClientOrderID: "ledger-replay", Symbol: "BTCUSDT", Side: exchange.SideSell,
		Quantity: 1, ExecutedQty: 1, AvgPrice: 101, Status: exchange.OrderStatusFilled}) {
		t.Fatal("terminal fill not observed")
	}
	tradePayload := []byte(`{"ExecutionKey":"grid-key","SellOrderID":1,"Quantity":1}`)
	if err := oe.MarkTradeLedgerRecordReconciliationRequired(1, "ledger-replay", "trade write unavailable", tradePayload); err != nil {
		t.Fatal(err)
	}
	blocked := NewExchangeOrderExecutor(venue, "BTCUSDT", 0, 0, lock.NewNopLock(), "bot-a")
	blocked.SetTradeLedgerRecoveryHandler(func(context.Context, execution.IntentScope, int64, float64, []byte) error {
		return errors.New("trade database remains unavailable")
	})
	if err := blocked.ConfigureIntentJournal(t.Context(), journal, journalScope()); !errors.Is(err, execution.ErrOrderUnknown) || !blocked.IsOpeningPaused() {
		t.Fatalf("failed ledger replay must preserve the startup hold: err=%v paused=%v", err, blocked.IsOpeningPaused())
	}
	restarted := NewExchangeOrderExecutor(venue, "BTCUSDT", 0, 0, lock.NewNopLock(), "bot-a")
	replayed := false
	restarted.SetTradeLedgerRecoveryHandler(func(_ context.Context, scope execution.IntentScope, orderID int64, cumulativeQty float64, payload []byte) error {
		replayed = true
		if scope != journalScope() || orderID != 1 || cumulativeQty != 1 || string(payload) != string(tradePayload) {
			t.Fatalf("replay identity mismatch: scope=%+v order=%d qty=%v payload=%s", scope, orderID, cumulativeQty, payload)
		}
		return nil
	})
	if err := restarted.ConfigureIntentJournal(t.Context(), journal, journalScope()); err != nil {
		t.Fatalf("idempotent trade replay should settle a pending ledger record: %v", err)
	}
	if !replayed || restarted.IsOpeningPaused() || len(restarted.snapshotOwnedIntents()) != 0 {
		t.Fatalf("trade replay did not clear the recovered hold: replayed=%v paused=%v intents=%+v", replayed, restarted.IsOpeningPaused(), restarted.snapshotOwnedIntents())
	}
	page, err := journal.LoadExecutionIntents(t.Context(), func() string { key, _ := journalScope().Key(); return key }(), 0, 10)
	if err != nil || len(page) != 1 {
		t.Fatalf("load settled replay intent: records=%d err=%v", len(page), err)
	}
	var saved persistedIntent
	if err := json.Unmarshal(page[0].Payload, &saved); err != nil || saved.LedgerPending || len(saved.LedgerPayload) != 0 || !saved.Settled {
		t.Fatalf("replayed intent was not durably settled: %+v err=%v", saved, err)
	}
}

func TestTradeLedgerHoldCannotBeSkippedAsZeroFillTerminal(t *testing.T) {
	intent := &ownedIntent{
		request:       OrderRequest{ClientOrderID: "ledger-pending", Symbol: "BTCUSDT", Side: "BUY", Quantity: 1},
		ledgerPending: true,
		order:         &Order{OrderID: 2, ClientOrderID: "ledger-pending", Symbol: "BTCUSDT", Side: "BUY", Quantity: 1, ExecutedQty: 0, Status: "CANCELED"},
	}
	if isVerifiedZeroFillTerminalIntent(intent, "BTCUSDT") {
		t.Fatal("an explicit economic reconciliation hold must survive zero-fill terminal cleanup")
	}
}
