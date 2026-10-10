package order

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

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
	writes                int
	reads                 int
	failWrite             int
	conflictWrite         int
	failRead              bool
	commitErrorAt         int
	failReadOnCommitError bool
}

func (m *memoryIntentJournal) SaveExecutionIntent(_ context.Context, scope, cid string, expected int64, payload []byte) (resultErr error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.writes++
	defer func() {
		if resultErr == nil && m.writes == m.commitErrorAt {
			if m.failReadOnCommitError {
				m.failRead = true
			}
			resultErr = errors.New("commit succeeded but acknowledgement lost")
		}
	}()
	if m.writes == m.failWrite {
		return errors.New("injected journal write failure")
	}
	if m.writes == m.conflictWrite {
		return execution.ErrIntentJournalConflict
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
	m.reads++
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

func TestSettleZeroFillIntentRejectsFilledStatusWithZeroQuantity(t *testing.T) {
	venue := &ownedTestVenue{orders: map[int64]*exchange.Order{19: {
		OrderID: 19, ClientOrderID: "grid-invalid-filled", Symbol: "BTCUSDT", Side: "BUY",
		Status: exchange.OrderStatusFilled, Quantity: 1, ExecutedQty: 0,
	}}}
	oe := NewExchangeOrderExecutor(venue, "BTCUSDT", 0, 0, lock.NewNopLock(), "bot-a")
	oe.intents = map[string]*ownedIntent{"grid-invalid-filled": {
		request: OrderRequest{Symbol: "BTCUSDT", Side: "BUY", Quantity: 1, ClientOrderID: "grid-invalid-filled"},
		order:   &Order{OrderID: 19, ClientOrderID: "grid-invalid-filled", Symbol: "BTCUSDT", Side: "BUY", Status: "CANCELED", Quantity: 1},
	}}

	if err := oe.SettleZeroFillIntent(t.Context(), "grid-invalid-filled"); err == nil {
		t.Fatal("zero-fill settlement accepted FILLED with zero executed quantity")
	}
	if oe.intents["grid-invalid-filled"].settled {
		t.Fatal("contradictory FILLED status was incorrectly settled")
	}
}

type transientGetOrderVenue struct {
	*ownedTestVenue
	failures int
}

func (v *transientGetOrderVenue) GetOrder(ctx context.Context, symbol string, orderID int64) (*exchange.Order, error) {
	if v.failures > 0 {
		v.failures--
		return nil, errors.New("temporary venue query failure")
	}
	return v.ownedTestVenue.GetOrder(ctx, symbol, orderID)
}

func TestSettleZeroFillIntentRetriesTransientVenueFailure(t *testing.T) {
	venue := &transientGetOrderVenue{
		ownedTestVenue: &ownedTestVenue{orders: map[int64]*exchange.Order{20: {
			OrderID: 20, ClientOrderID: "grid-cancel-retry", Symbol: "BTCUSDT", Side: "BUY",
			Status: exchange.OrderStatusCanceled, Quantity: 1,
		}}},
		failures: 2,
	}
	oe := NewExchangeOrderExecutor(venue, "BTCUSDT", 0, 0, lock.NewNopLock(), "bot-a")
	oe.intents = map[string]*ownedIntent{"grid-cancel-retry": {
		request: OrderRequest{Symbol: "BTCUSDT", Side: "BUY", Quantity: 1, ClientOrderID: "grid-cancel-retry"},
		order:   &Order{OrderID: 20, ClientOrderID: "grid-cancel-retry", Symbol: "BTCUSDT", Side: "BUY", Status: "CANCELED", Quantity: 1},
	}}
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()

	if err := oe.SettleZeroFillIntent(ctx, "grid-cancel-retry"); err != nil {
		t.Fatalf("SettleZeroFillIntent() did not recover after transient query failures: %v", err)
	}
	if !oe.intents["grid-cancel-retry"].settled || venue.failures != 0 {
		t.Fatal("intent was not settled after the venue query recovered")
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

func TestIntentJournalPrecedesSubmissionAndReconcilesOnlyProvenUncommittedWrites(t *testing.T) {
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
			if fail > 0 && (err != nil || oe.IsOpeningPaused()) {
				t.Fatalf("readback-confirmed uncommitted journal write should recover before execution: err=%v paused=%v", err, oe.IsOpeningPaused())
			}
			want := int64(1)
			if venue.nextID != want {
				t.Fatalf("physical submissions=%d want=%d", venue.nextID, want)
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
	settled := restarted.snapshotOwnedIntents()
	if len(settled) != 1 || !settled[0].settled || settled[0].request.ClientOrderID != placed.ClientOrderID {
		t.Fatalf("verified settled owner must remain in memory for idempotent settlement: %+v", settled)
	}
}

func TestConfigureIntentJournalSameBindingSettledReentry(t *testing.T) {
	for _, zeroFill := range []bool{false, true} {
		name := "positive terminal"
		if zeroFill {
			name = "verified zero fill"
		}
		t.Run(name, func(t *testing.T) {
			venue := &ownedTestVenue{orders: make(map[int64]*exchange.Order)}
			journal := &memoryIntentJournal{}
			oe := NewExchangeOrderExecutor(venue, "BTCUSDT", 0, 0, lock.NewNopLock(), "bot-a")
			if err := oe.ConfigureIntentJournal(t.Context(), journal, journalScope()); err != nil {
				t.Fatal(err)
			}
			cid := "same-binding-settled-reentry"
			placed, err := oe.PlaceOrder(&OrderRequest{Symbol: "BTCUSDT", Side: "BUY", Price: 100, Quantity: 1,
				ClientOrderID: cid, StrategyName: "grid", StrategyType: "grid"})
			if err != nil {
				t.Fatal(err)
			}
			venue.mu.Lock()
			if zeroFill {
				venue.orders[placed.OrderID].Status = exchange.OrderStatusCanceled
			} else {
				venue.orders[placed.OrderID].ExecutedQty = 1
				venue.orders[placed.OrderID].AvgPrice = 100
				venue.orders[placed.OrderID].Status = exchange.OrderStatusFilled
			}
			venue.mu.Unlock()
			if zeroFill {
				err = oe.SettleZeroFillIntent(t.Context(), cid)
			} else {
				err = oe.SettleIntent(t.Context(), cid)
			}
			if err != nil {
				t.Fatalf("durable settlement: %v", err)
			}
			if !oe.journalLoaded || oe.intentJournal != journal || len(oe.intents) != 1 || !oe.intents[cid].settled {
				t.Fatal("settlement did not retain the loaded binding and settled owner record")
			}
			settledIntent := oe.intents[cid]
			settledRevision := settledIntent.revision
			journal.mu.Lock()
			writesBeforeDuplicateSettle := journal.writes
			journal.mu.Unlock()
			if zeroFill {
				err = oe.SettleZeroFillIntent(t.Context(), cid)
			} else {
				err = oe.SettleIntent(t.Context(), cid)
			}
			if err != nil {
				t.Fatalf("duplicate settlement must be idempotent: %v", err)
			}
			journal.mu.Lock()
			writesAfterDuplicateSettle := journal.writes
			journal.mu.Unlock()
			if settledIntent.revision != settledRevision || writesAfterDuplicateSettle != writesBeforeDuplicateSettle {
				t.Fatal("duplicate settlement rewrote the already durable intent")
			}
			journal.mu.Lock()
			readsBeforeReentry := journal.reads
			journal.mu.Unlock()

			if err := oe.ConfigureIntentJournal(t.Context(), journal, journalScope()); err != nil {
				t.Fatalf("same-binding reentry after settlement must permit full bootstrap retry: %v", err)
			}
			journal.mu.Lock()
			readsAfterReentry := journal.reads
			journal.mu.Unlock()
			if !oe.journalLoaded || oe.intentJournal != journal || len(oe.intents) != 1 || oe.intents[cid] != settledIntent || !oe.intents[cid].settled {
				t.Fatal("same-binding reentry changed the in-memory settled owner record")
			}
			if readsAfterReentry != readsBeforeReentry {
				t.Fatalf("same-binding no-op unexpectedly reloaded the journal: reads %d -> %d", readsBeforeReentry, readsAfterReentry)
			}
		})
	}
}

func TestIntentJournalHigherCumulativeDuplicateAndOutOfOrderUpdates(t *testing.T) {
	oe, _, _ := newOwnedTestExecutor()
	journal := &memoryIntentJournal{}
	if err := oe.ConfigureIntentJournal(t.Context(), journal, journalScope()); err != nil {
		t.Fatal(err)
	}
	const cid = "higher-cumulative-order"
	if _, err := oe.PlaceOrder(&OrderRequest{Symbol: "BTCUSDT", Side: "BUY", Price: 100, Quantity: 1, ClientOrderID: cid}); err != nil {
		t.Fatal(err)
	}
	updates := []struct {
		order *exchange.Order
		qty   float64
		avg   float64
	}{
		{order: &exchange.Order{OrderID: 1, ClientOrderID: cid, Symbol: "BTCUSDT", Side: exchange.SideBuy, Quantity: 1, ExecutedQty: 0.4, AvgPrice: 99, Status: exchange.OrderStatusPartiallyFilled}, qty: 0.4, avg: 99},
		{order: &exchange.Order{OrderID: 1, ClientOrderID: cid, Symbol: "BTCUSDT", Side: exchange.SideBuy, Quantity: 1, ExecutedQty: 0.75, AvgPrice: 99.5, Status: exchange.OrderStatusPartiallyFilled}, qty: 0.75, avg: 99.5},
		{order: &exchange.Order{OrderID: 1, ClientOrderID: cid, Symbol: "BTCUSDT", Side: exchange.SideBuy, Quantity: 1, ExecutedQty: 0.75, AvgPrice: 99.5, Status: exchange.OrderStatusPartiallyFilled}, qty: 0.75, avg: 99.5},
		{order: &exchange.Order{OrderID: 1, ClientOrderID: cid, Symbol: "BTCUSDT", Side: exchange.SideBuy, Quantity: 1, ExecutedQty: 0.4, AvgPrice: 99, Status: exchange.OrderStatusPartiallyFilled}, qty: 0.75, avg: 99.5},
	}
	for index, update := range updates {
		if !oe.ObserveOrder(update.order) {
			t.Fatalf("update %d was not safely recognized", index)
		}
		intent := oe.snapshotOwnedIntents()[0]
		if intent.order.ExecutedQty != update.qty || intent.order.AvgPrice != update.avg || intent.unknown {
			t.Fatalf("update %d regressed or fenced the higher cumulative cursor: %+v", index, intent)
		}
	}
	key, _ := journalScope().Key()
	page, err := journal.LoadExecutionIntents(t.Context(), key, 0, 10)
	if err != nil || len(page) != 1 {
		t.Fatalf("durable owner records = %d, err=%v", len(page), err)
	}
	var persisted persistedIntent
	if err := json.Unmarshal(page[0].Payload, &persisted); err != nil {
		t.Fatal(err)
	}
	if persisted.Order.ExecutedQty != 0.75 || persisted.Order.AvgPrice != 99.5 || persisted.Unknown {
		t.Fatalf("durable cumulative cursor regressed or became unknown: %+v", persisted)
	}
}

func TestConfigureIntentJournalPendingReentryPreservesLoadedBinding(t *testing.T) {
	venue := &ownedTestVenue{orders: make(map[int64]*exchange.Order)}
	journal := &memoryIntentJournal{}
	oe := NewExchangeOrderExecutor(venue, "BTCUSDT", 0, 0, lock.NewNopLock(), "bot-a")
	if err := oe.ConfigureIntentJournal(t.Context(), journal, journalScope()); err != nil {
		t.Fatal(err)
	}
	placed, err := oe.PlaceOrder(&OrderRequest{Symbol: "BTCUSDT", Side: "BUY", Price: 100, Quantity: 1,
		ClientOrderID: "pending-reentry", StrategyName: "grid", StrategyType: "grid"})
	if err != nil {
		t.Fatal(err)
	}
	key, _ := journalScope().Key()
	if err := oe.ConfigureIntentJournal(t.Context(), journal, journalScope()); err == nil {
		t.Fatal("same-binding reentry with an unresolved intent must reject bootstrap")
	}
	if !oe.journalLoaded || oe.intentJournal != journal || oe.intentScopeKey != key {
		t.Fatal("rejected unresolved reentry polluted the active journal binding")
	}

	partial := &exchange.Order{OrderID: placed.OrderID, ClientOrderID: placed.ClientOrderID, Symbol: "BTCUSDT",
		Side: exchange.SideBuy, Quantity: 1, ExecutedQty: 0.5, AvgPrice: 100, Status: exchange.OrderStatusPartiallyFilled}
	if !oe.ObserveOrder(partial) {
		t.Fatal("loaded journal stopped accepting durable observations after retry rejection")
	}
	venue.mu.Lock()
	venue.orders[placed.OrderID].ExecutedQty = 0.5
	venue.orders[placed.OrderID].AvgPrice = 100
	venue.orders[placed.OrderID].Status = exchange.OrderStatusCanceled
	venue.mu.Unlock()
	terminal := *partial
	terminal.Status = exchange.OrderStatusCanceled
	if !oe.ObserveOrder(&terminal) {
		t.Fatal("loaded journal stopped accepting the terminal observation")
	}
	if err := oe.SettleIntent(t.Context(), placed.ClientOrderID); err != nil {
		t.Fatalf("unresolved intent could not settle after retry rejection: %v", err)
	}
}

func TestConfigureIntentJournalPendingReentryPreservesOtherIntents(t *testing.T) {
	venue := &ownedTestVenue{orders: make(map[int64]*exchange.Order)}
	journal := &memoryIntentJournal{}
	oe := NewExchangeOrderExecutor(venue, "BTCUSDT", 0, 0, lock.NewNopLock(), "bot-a")
	scope := journalScope()
	if err := oe.ConfigureIntentJournal(t.Context(), journal, scope); err != nil {
		t.Fatal(err)
	}
	first, err := oe.PlaceOrder(&OrderRequest{Symbol: scope.Symbol, Side: "BUY", Price: 100, Quantity: 1,
		ClientOrderID: "first-terminal", StrategyName: "grid", StrategyType: "grid"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := oe.PlaceOrder(&OrderRequest{Symbol: scope.Symbol, Side: "BUY", Price: 99, Quantity: 1,
		ClientOrderID: "second-unresolved", StrategyName: "grid", StrategyType: "grid"})
	if err != nil {
		t.Fatal(err)
	}
	venue.mu.Lock()
	venue.orders[first.OrderID].ExecutedQty = 1
	venue.orders[first.OrderID].AvgPrice = 100
	venue.orders[first.OrderID].Status = exchange.OrderStatusFilled
	venue.mu.Unlock()
	if !oe.ObserveOrder(&exchange.Order{OrderID: first.OrderID, ClientOrderID: first.ClientOrderID, Symbol: scope.Symbol,
		Side: exchange.SideBuy, Quantity: 1, ExecutedQty: 1, AvgPrice: 100, Status: exchange.OrderStatusFilled}) {
		t.Fatal("first terminal order observation failed")
	}
	if err := oe.SettleIntent(t.Context(), first.ClientOrderID); err != nil {
		t.Fatalf("settle first intent: %v", err)
	}
	firstIntent := oe.intents[first.ClientOrderID]
	secondIntent := oe.intents[second.ClientOrderID]
	if err := oe.ConfigureIntentJournal(t.Context(), journal, scope); err == nil {
		t.Fatal("bootstrap reentry with another unresolved intent must fail")
	}
	if !oe.journalLoaded || oe.intentJournal != journal || oe.intents[first.ClientOrderID] != firstIntent ||
		oe.intents[second.ClientOrderID] != secondIntent || firstIntent == nil || !firstIntent.settled || secondIntent == nil || secondIntent.settled {
		t.Fatal("rejected reentry changed a settled or unresolved CID")
	}
	venue.mu.Lock()
	venue.orders[second.OrderID].Status = exchange.OrderStatusCanceled
	venue.mu.Unlock()
	if !oe.ObserveOrder(&exchange.Order{OrderID: second.OrderID, ClientOrderID: second.ClientOrderID, Symbol: scope.Symbol,
		Side: exchange.SideBuy, Quantity: 1, Status: exchange.OrderStatusCanceled}) {
		t.Fatal("remaining CID could not be observed after bootstrap retry rejection")
	}
	if err := oe.SettleZeroFillIntent(t.Context(), second.ClientOrderID); err != nil {
		t.Fatalf("remaining CID could not settle after bootstrap retry rejection: %v", err)
	}
}

func TestConfigureIntentJournalRejectsDifferentBindingWithoutSideEffects(t *testing.T) {
	for _, scenario := range []string{"different backend", "different scope"} {
		t.Run(scenario, func(t *testing.T) {
			venue := &ownedTestVenue{orders: make(map[int64]*exchange.Order)}
			original := &memoryIntentJournal{}
			candidate := &memoryIntentJournal{}
			oe := NewExchangeOrderExecutor(venue, "BTCUSDT", 0, 0, lock.NewNopLock(), "bot-a")
			originalScope := journalScope()
			if err := oe.ConfigureIntentJournal(t.Context(), original, originalScope); err != nil {
				t.Fatal(err)
			}
			candidateScope := originalScope
			if scenario == "different scope" {
				candidateScope.Account = "another-account-digest"
			}
			candidateBackend := execution.IntentJournal(original)
			if scenario == "different backend" {
				candidateBackend = candidate
			}
			originalKey, _ := originalScope.Key()
			original.mu.Lock()
			originalReads := original.reads
			original.mu.Unlock()
			if err := oe.ConfigureIntentJournal(t.Context(), candidateBackend, candidateScope); err == nil {
				t.Fatal("different journal backend or scope was rebound")
			}
			if !oe.journalLoaded || oe.intentJournal != original || oe.intentScope != originalScope || oe.intentScopeKey != originalKey {
				t.Fatal("rejected binding changed the active journal or loaded state")
			}
			if candidate.reads != 0 {
				t.Fatalf("rejected binding read candidate journal %d times", candidate.reads)
			}
			original.mu.Lock()
			readsAfterReject := original.reads
			original.mu.Unlock()
			if readsAfterReject != originalReads {
				t.Fatalf("rejected binding read the original journal: reads %d -> %d", originalReads, readsAfterReject)
			}
			if _, err := oe.PlaceOrder(&OrderRequest{Symbol: "BTCUSDT", Side: "BUY", Price: 100, Quantity: 1,
				ClientOrderID: "original-binding-still-usable"}); err != nil {
				t.Fatalf("original binding was unusable after side-effect-free rejection: %v", err)
			}
		})
	}
}

func TestSettleIntentUncertainCASCanReconcileOnSameBinding(t *testing.T) {
	venue := &ownedTestVenue{orders: make(map[int64]*exchange.Order)}
	journal := &memoryIntentJournal{}
	scope := journalScope()
	first := NewExchangeOrderExecutor(venue, "BTCUSDT", 0, 0, lock.NewNopLock(), "bot-a")
	if err := first.ConfigureIntentJournal(t.Context(), journal, scope); err != nil {
		t.Fatal(err)
	}
	placed, err := first.PlaceOrder(&OrderRequest{Symbol: scope.Symbol, Side: "BUY", Price: 100, Quantity: 1,
		ClientOrderID: "cas-conflict-recovery", StrategyName: "grid", StrategyType: "grid"})
	if err != nil {
		t.Fatal(err)
	}
	venue.mu.Lock()
	venue.orders[placed.OrderID].ExecutedQty = 1
	venue.orders[placed.OrderID].AvgPrice = 100
	venue.orders[placed.OrderID].Status = exchange.OrderStatusFilled
	venue.mu.Unlock()
	terminal := &exchange.Order{OrderID: placed.OrderID, ClientOrderID: placed.ClientOrderID, Symbol: scope.Symbol,
		Side: exchange.SideBuy, Quantity: 1, ExecutedQty: 1, AvgPrice: 100, Status: exchange.OrderStatusFilled}
	if !first.ObserveOrder(terminal) {
		t.Fatal("terminal order observation failed")
	}
	journal.mu.Lock()
	journal.conflictWrite = journal.writes + 2 // exact-order observation, then settlement CAS
	conflictWrite := journal.conflictWrite
	readsBeforeConflict := journal.reads
	journal.mu.Unlock()
	if err := first.SettleIntent(t.Context(), placed.ClientOrderID); err != nil {
		t.Fatalf("same-binding readback proving the CAS base should permit a safe retry: %v", err)
	}
	journal.mu.Lock()
	writesAfterRetry, readsAfterConflict := journal.writes, journal.reads
	journal.mu.Unlock()
	if writesAfterRetry != conflictWrite+1 || readsAfterConflict <= readsBeforeConflict {
		t.Fatalf("base readback must be followed by exactly one safe CAS retry: target=%d writes=%d reads %d->%d",
			conflictWrite, writesAfterRetry, readsBeforeConflict, readsAfterConflict)
	}
	if !first.journalLoaded || !first.intents[placed.ClientOrderID].settled || first.intents[placed.ClientOrderID].unknown || first.IsOpeningPaused() {
		t.Fatal("safe CAS retry did not preserve a verified settled owner and open gate")
	}
	settled := first.intents[placed.ClientOrderID]
	if err := first.ConfigureIntentJournal(t.Context(), journal, scope); err != nil {
		t.Fatalf("same-binding settled-only reentry should remain a no-op: %v", err)
	}
	if first.intents[placed.ClientOrderID] != settled || !settled.settled || first.IsOpeningPaused() {
		t.Fatal("same-binding reentry lost the idempotent settled owner")
	}
}

func TestSettleIntentUncertainCommitIsResolvedByRestart(t *testing.T) {
	venue := &ownedTestVenue{orders: make(map[int64]*exchange.Order)}
	journal := &memoryIntentJournal{}
	scope := journalScope()
	first := NewExchangeOrderExecutor(venue, "BTCUSDT", 0, 0, lock.NewNopLock(), "bot-a")
	if err := first.ConfigureIntentJournal(t.Context(), journal, scope); err != nil {
		t.Fatal(err)
	}
	placed, err := first.PlaceOrder(&OrderRequest{Symbol: scope.Symbol, Side: "BUY", Price: 100, Quantity: 1,
		ClientOrderID: "settle-commit-ack-lost", StrategyName: "grid", StrategyType: "grid"})
	if err != nil {
		t.Fatal(err)
	}
	venue.mu.Lock()
	venue.orders[placed.OrderID].ExecutedQty = 1
	venue.orders[placed.OrderID].AvgPrice = 100
	venue.orders[placed.OrderID].Status = exchange.OrderStatusFilled
	venue.mu.Unlock()
	if !first.ObserveOrder(&exchange.Order{OrderID: placed.OrderID, ClientOrderID: placed.ClientOrderID, Symbol: scope.Symbol,
		Side: exchange.SideBuy, Quantity: 1, ExecutedQty: 1, AvgPrice: 100, Status: exchange.OrderStatusFilled}) {
		t.Fatal("terminal order observation failed")
	}
	journal.mu.Lock()
	journal.commitErrorAt = journal.writes + 2 // exact-order observation, then committed settlement with lost acknowledgement
	journal.failReadOnCommitError = true
	journal.mu.Unlock()
	if err := first.SettleIntent(t.Context(), placed.ClientOrderID); err == nil {
		t.Fatal("lost settlement acknowledgement must not be reported as confirmed")
	}
	if first.journalLoaded || !first.IsOpeningPaused() {
		t.Fatal("uncertain settlement commit did not fence the current executor")
	}
	journal.mu.Lock()
	journal.failRead = false
	journal.mu.Unlock()

	restarted := NewExchangeOrderExecutor(venue, "BTCUSDT", 0, 0, lock.NewNopLock(), "bot-a")
	if err := restarted.ConfigureIntentJournal(t.Context(), journal, scope); err != nil {
		t.Fatalf("restart failed to reconcile durable settlement: %v", err)
	}
	if !restarted.journalLoaded || restarted.IsOpeningPaused() || len(restarted.RecoveredOrderRoutes()) != 0 {
		t.Fatal("restart did not trust only the durable settled journal record")
	}
}

func TestSettleReconciledIntentIsIdempotentAfterRestart(t *testing.T) {
	venue := &ownedTestVenue{orders: make(map[int64]*exchange.Order)}
	journal := &memoryIntentJournal{}
	const cid = "spot-short-settled-cid"
	first := NewExchangeOrderExecutor(venue, "BTCUSDT", 0, 0, lock.NewNopLock(), "bot-a")
	if err := first.ConfigureIntentJournal(t.Context(), journal, journalScope()); err != nil {
		t.Fatal(err)
	}
	placed, err := first.PlaceOrder(&OrderRequest{Symbol: "BTCUSDT", Side: "SELL", Price: 100, Quantity: 0.25, ClientOrderID: cid, StrategyName: "spot_short"})
	if err != nil {
		t.Fatal(err)
	}
	venue.mu.Lock()
	venue.orders[placed.OrderID].ExecutedQty = 0.25
	venue.orders[placed.OrderID].AvgPrice = 100
	venue.orders[placed.OrderID].Status = exchange.OrderStatusFilled
	venue.mu.Unlock()
	if err := first.SettleIntent(t.Context(), cid); err != nil {
		t.Fatalf("initial settlement: %v", err)
	}
	restarted := NewExchangeOrderExecutor(venue, "BTCUSDT", 0, 0, lock.NewNopLock(), "bot-a")
	if err := restarted.ConfigureIntentJournal(t.Context(), journal, journalScope()); err != nil {
		t.Fatal(err)
	}
	if err := restarted.SettleReconciledIntent(t.Context(), cid, "spot_short"); err != nil {
		t.Fatalf("idempotent settlement acknowledgement after restart: %v", err)
	}
	if err := restarted.SettleReconciledIntent(t.Context(), cid, "another-strategy"); err == nil {
		t.Fatal("settled intent was accepted for the wrong strategy owner")
	}
}

func TestIntentJournalRestartReleasesOnlyVerifiedZeroFillTerminalOrders(t *testing.T) {
	tests := []struct {
		name        string
		requestCID  string
		order       *Order
		venueOrder  *exchange.Order
		unknown     bool
		wantBlocked bool
	}{
		{name: "verified canceled without fills", order: &Order{OrderID: 7, ClientOrderID: "cancelled", Symbol: "BTCUSDT", Side: "BUY", Quantity: 1, Status: "CANCELED"}},
		{name: "verified expired without fills", order: &Order{OrderID: 8, ClientOrderID: "expired", Symbol: "BTCUSDT", Side: "BUY", Quantity: 1, Status: "EXPIRED"}},
		{name: "stale zero-fill snapshot reveals a late fill", order: &Order{OrderID: 12, ClientOrderID: "late-fill", Symbol: "BTCUSDT", Side: "BUY", Quantity: 1, Status: "CANCELED"},
			venueOrder: &exchange.Order{OrderID: 12, ClientOrderID: "late-fill", Symbol: "BTCUSDT", Side: exchange.SideBuy, Quantity: 1, ExecutedQty: 0.25, AvgPrice: 100, Status: exchange.OrderStatusCanceled}, wantBlocked: true},
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
			oe, venue, _ := newOwnedTestExecutor()
			observed := test.venueOrder
			if observed == nil {
				observed = &exchange.Order{OrderID: test.order.OrderID, ClientOrderID: test.order.ClientOrderID, Symbol: test.order.Symbol,
					Side: exchange.Side(test.order.Side), Quantity: test.order.Quantity, ExecutedQty: test.order.ExecutedQty,
					AvgPrice: test.order.AvgPrice, Status: exchange.OrderStatus(test.order.Status)}
			}
			venue.orders[observed.OrderID] = observed
			err = oe.ConfigureIntentJournal(t.Context(), journal, journalScope())
			if test.wantBlocked {
				if !errors.Is(err, execution.ErrOrderUnknown) || !oe.IsOpeningPaused() || len(oe.snapshotOwnedIntents()) != 1 {
					t.Fatalf("unresolved order did not retain its recovery hold: err=%v intents=%d", err, len(oe.snapshotOwnedIntents()))
				}
				return
			}
			settled := oe.snapshotOwnedIntents()
			if err != nil || oe.IsOpeningPaused() || len(settled) != 1 || !settled[0].settled || settled[0].request.ClientOrderID != cid {
				t.Fatalf("verified no-fill owner must remain settled in memory for idempotency: err=%v intents=%+v", err, settled)
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

func TestIntentJournalObservationWriteFailureRetriesOnlyAfterBaseReadback(t *testing.T) {
	oe, _, _ := newOwnedTestExecutor()
	journal := &memoryIntentJournal{failWrite: 4}
	if err := oe.ConfigureIntentJournal(t.Context(), journal, journalScope()); err != nil {
		t.Fatal(err)
	}
	if _, err := oe.PlaceOrder(&OrderRequest{Symbol: "BTCUSDT", Side: "BUY", Price: 100, Quantity: 1, ClientOrderID: "open"}); err != nil {
		t.Fatal(err)
	}
	if !oe.ObserveOrder(&exchange.Order{OrderID: 1, ClientOrderID: "open", Symbol: "BTCUSDT", Quantity: 1, ExecutedQty: 0.5, Price: 100, Status: exchange.OrderStatusPartiallyFilled}) {
		t.Fatal("readback-confirmed base revision should allow one safe retry")
	}
	if oe.IsOpeningPaused() || len(oe.snapshotOwnedIntents()) != 1 || oe.snapshotOwnedIntents()[0].unknown {
		t.Fatal("verified durable observation should not leave a phantom unknown hold")
	}
}

func TestIntentJournalCommittedWritesWithLostAcknowledgementAreReadBack(t *testing.T) {
	for _, at := range []int{1, 2, 3} {
		t.Run(fmt.Sprintf("commit-%d", at), func(t *testing.T) {
			oe, venue, _ := newOwnedTestExecutor()
			journal := &memoryIntentJournal{commitErrorAt: at}
			if err := oe.ConfigureIntentJournal(t.Context(), journal, journalScope()); err != nil {
				t.Fatal(err)
			}
			_, err := oe.PlaceOrder(&OrderRequest{Symbol: "BTCUSDT", Side: "BUY", Price: 100, Quantity: 1, ClientOrderID: "ambiguous-db", StrategyName: "grid"})
			if err != nil || oe.IsOpeningPaused() || journal.writes != 3 {
				t.Fatalf("committed CAS acknowledgement was not reconciled: err=%v paused=%v writes=%d", err, oe.IsOpeningPaused(), journal.writes)
			}
			want := int64(1)
			if venue.nextID != want {
				t.Fatalf("unexpected venue call count: %d", venue.nextID)
			}
			restarted := NewExchangeOrderExecutor(venue, "BTCUSDT", 0, 0, lock.NewNopLock(), "bot-a")
			if err := restarted.ConfigureIntentJournal(t.Context(), journal, journalScope()); !errors.Is(err, execution.ErrOrderUnknown) {
				t.Fatalf("restart should retain the live order as unresolved: %v", err)
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
			oe, venue, gate := newOwnedTestExecutor()
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
			if !oe.IsOpeningPaused() || !gate.HasBlock(IntentRecoveryBlock) {
				t.Fatalf("missing or mismatched journal must retain a fail-closed opening hold: paused=%v", oe.IsOpeningPaused())
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
	recoveredIntents := restarted.snapshotOwnedIntents()
	if !replayed || restarted.IsOpeningPaused() || len(recoveredIntents) != 1 || !recoveredIntents[0].settled {
		t.Fatalf("trade replay did not clear the recovered hold while retaining settled ownership: replayed=%v paused=%v intents=%+v", replayed, restarted.IsOpeningPaused(), recoveredIntents)
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
