package strategy

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"testing"

	"quantmesh/config"
	"quantmesh/exchange"
	"quantmesh/position"
)

type martingaleIntentCheckingExecutor struct {
	hedgeOrderExecutor
	store *memoryRuntimeStateStore
	err   error
}

func (e *martingaleIntentCheckingExecutor) PlaceOrder(req *position.OrderRequest) (*position.Order, error) {
	if req.ClientOrderID == "" || e.store.payload == "" {
		return nil, errors.New("entry intent was not durable before submit")
	}
	var state martingaleRuntimeState
	if err := json.Unmarshal([]byte(e.store.payload), &state); err != nil {
		return nil, err
	}
	if len(state.Entries) != 1 || state.Entries[0].ClientOrderID != req.ClientOrderID || state.Entries[0].OrderID != 0 {
		return nil, errors.New("persisted entry intent does not match submitted CID")
	}
	e.orders = append(e.orders, req)
	if e.err != nil {
		return nil, e.err
	}
	return &position.Order{OrderID: 71, ClientOrderID: req.ClientOrderID, Side: req.Side, Quantity: req.Quantity}, nil
}

type martingaleCIDLookupExchange struct {
	*hedgeExchange
	order *exchange.Order
}

func (e *martingaleCIDLookupExchange) GetOrderByClientOrderID(_ context.Context, _, cid string) (*exchange.Order, error) {
	if e.order == nil || e.order.ClientOrderID != cid {
		return nil, errors.New("order not found")
	}
	copyOrder := *e.order
	return &copyOrder, nil
}

func TestMartingaleEntryIntentPersistsCIDBeforeSubmit(t *testing.T) {
	store := &memoryRuntimeStateStore{}
	executor := &martingaleIntentCheckingExecutor{store: store}
	s := NewMartingaleStrategy("martingale", "BTCUSDT", &config.Config{}, executor, &hedgeExchange{}, nil)
	s.SetRuntimeStateStore(store)
	s.direction = "LONG"
	entry := &MartingaleEntry{Level: 0, Price: 100, RequestedQuantity: 1, Status: entryStatusPending}
	if err := s.submitEntryOrder(entry, "BUY"); err != nil {
		t.Fatal(err)
	}
	if entry.ClientOrderID == "" || entry.OrderID != 71 || len(executor.orders) != 1 || executor.orders[0].ClientOrderID != entry.ClientOrderID {
		t.Fatalf("entry intent was not bound to acknowledgement: entry=%+v request=%+v", entry, executor.orders)
	}
}

func TestMartingaleAmbiguousEntrySubmissionPersistsUnknownIntent(t *testing.T) {
	store := &memoryRuntimeStateStore{}
	executor := &martingaleIntentCheckingExecutor{store: store, err: errors.New("timeout")}
	s := NewMartingaleStrategy("martingale", "BTCUSDT", &config.Config{}, executor, &hedgeExchange{}, nil)
	s.SetRuntimeStateStore(store)
	s.direction = "LONG"
	entry := &MartingaleEntry{Level: 0, Price: 100, RequestedQuantity: 1, Status: entryStatusPending}
	if err := s.submitEntryOrder(entry, "BUY"); err == nil {
		t.Fatal("ambiguous submission unexpectedly succeeded")
	}
	if len(s.entries) != 1 || entry.Status != position.OrderStatusUnknown || entry.ClientOrderID == "" || entry.OrderID != 0 || s.currentLevel != 1 {
		t.Fatalf("ambiguous order intent was discarded or treated as rejected: entry=%+v level=%d", entry, s.currentLevel)
	}
	var persisted martingaleRuntimeState
	if err := json.Unmarshal([]byte(store.payload), &persisted); err != nil || len(persisted.Entries) != 1 || persisted.Entries[0].Status != position.OrderStatusUnknown {
		t.Fatalf("unknown entry was not durably retained: state=%+v err=%v", persisted, err)
	}
}

func TestMartingaleRestoresEntryOrderByPersistedCID(t *testing.T) {
	store := &memoryRuntimeStateStore{}
	first := NewMartingaleStrategy("martingale", "BTCUSDT", &config.Config{}, &hedgeOrderExecutor{}, &hedgeExchange{}, nil)
	first.SetRuntimeStateStore(store)
	first.direction = "LONG"
	entry := &MartingaleEntry{Level: 0, Price: 100, RequestedQuantity: 1, Status: entryStatusPending, ClientOrderID: "stable-cid"}
	first.entries = []*MartingaleEntry{entry}
	first.currentLevel = 1
	if err := first.persistRuntimeStateLocked(); err != nil {
		t.Fatal(err)
	}
	venue := &martingaleCIDLookupExchange{hedgeExchange: &hedgeExchange{}, order: &exchange.Order{
		OrderID: 72, ClientOrderID: "stable-cid", Symbol: "BTCUSDT", Side: exchange.SideBuy,
		Quantity: 1, Status: exchange.OrderStatusNew,
	}}
	restarted := NewMartingaleStrategy("martingale", "BTCUSDT", &config.Config{}, &hedgeOrderExecutor{}, venue, nil)
	restarted.SetRuntimeStateStore(store)
	restarted.direction = "LONG"
	if err := restarted.restoreRuntimeState(); err != nil {
		t.Fatal(err)
	}
	if err := restarted.reconcilePersistedEntryOrders(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(restarted.entries) != 1 || restarted.entries[0].OrderID != 72 || restarted.entries[0].ClientOrderID != "stable-cid" {
		t.Fatalf("entry was not rebound to exact venue order: %+v", restarted.entries)
	}
}

type martingaleReconciliationExecutor struct {
	hedgeOrderExecutor
	marked bool
}

func (e *martingaleReconciliationExecutor) MarkOrderReconciliationRequired(int64, string, string) error {
	e.marked = true
	return nil
}

func TestMartingaleRealizedPnLIncludesQuoteValuedEntryAndExitFees(t *testing.T) {
	s := NewMartingaleStrategy("martingale", "BTCUSDT", &config.Config{}, &hedgeOrderExecutor{}, &hedgeExchange{}, nil)
	setTestRuntimeStateStore(t, s)
	s.direction = "LONG"
	entry := &MartingaleEntry{Level: 1, OrderID: 11, Status: entryStatusPending}
	s.entries = []*MartingaleEntry{entry}
	s.handleEntryOrderUpdate(entry, &position.OrderUpdate{
		OrderID: 11, Status: "FILLED", ExecutedQty: 1, AvgPrice: 100,
		Commission: 0.2, CommissionAsset: "USDT",
	})
	if entry.OpeningFee != 0.2 || s.totalQty != 1 {
		t.Fatalf("entry fee/quantity not booked: entry=%+v total=%v", entry, s.totalQty)
	}
	s.isClosing, s.closeOrderID = true, 12
	if err := s.OnOrderUpdate(&position.OrderUpdate{
		OrderID: 12, Status: "FILLED", ExecutedQty: 1, AvgPrice: 110,
		Commission: 0.1, CommissionAsset: "USDT",
	}); err != nil {
		t.Fatal(err)
	}
	if s.stats.TotalPnL != 9.7 || s.totalQty != 0 {
		t.Fatalf("net PnL should be 10 gross - 0.2 entry fee - 0.1 exit fee: stats=%+v qty=%v", s.stats, s.totalQty)
	}
}

func TestMartingaleNonFiniteEntryFillRetainsOrderForReconciliation(t *testing.T) {
	s := NewMartingaleStrategy("martingale", "BTCUSDT", &config.Config{}, &hedgeOrderExecutor{}, &hedgeExchange{}, nil)
	setTestRuntimeStateStore(t, s)
	s.direction = "LONG"
	entry := &MartingaleEntry{Level: 1, OrderID: 31, Status: entryStatusPending}
	s.entries = []*MartingaleEntry{entry}
	if err := s.OnOrderUpdate(&position.OrderUpdate{
		OrderID: 31, Status: "FILLED", ExecutedQty: math.NaN(), AvgPrice: 100,
	}); err != nil {
		t.Fatal(err)
	}
	if len(s.entries) != 1 || s.entries[0] != entry || entry.Status != position.OrderStatusUnknown || entry.Quantity != 0 || s.totalQty != 0 {
		t.Fatalf("non-finite fill must retain an UNKNOWN entry for reconciliation: entries=%+v total=%v", s.entries, s.totalQty)
	}
}

func TestMartingaleNonFiniteCloseFillDoesNotConsumeInventory(t *testing.T) {
	tests := []struct {
		name   string
		update position.OrderUpdate
	}{
		{name: "quantity", update: position.OrderUpdate{ExecutedQty: math.NaN(), AvgPrice: 110}},
		{name: "price", update: position.OrderUpdate{ExecutedQty: 1, AvgPrice: math.Inf(1)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewMartingaleStrategy("martingale", "BTCUSDT", &config.Config{}, &hedgeOrderExecutor{}, &hedgeExchange{}, nil)
			setTestRuntimeStateStore(t, s)
			s.direction = "LONG"
			s.entries = []*MartingaleEntry{{Level: 1, Price: 100, Quantity: 1, Cost: 100, Status: entryStatusFilled}}
			s.updateTotals()
			s.isClosing, s.closeOrderID, s.closeRequestedQty = true, 41, 1
			tt.update.OrderID, tt.update.Status = 41, "PARTIALLY_FILLED"
			if err := s.OnOrderUpdate(&tt.update); err != nil {
				t.Fatal(err)
			}
			if !s.isClosing || s.totalQty != 1 || s.closeProgress.Quantity != 0 || s.stats.TotalPnL != 0 {
				t.Fatalf("invalid close fill consumed state: closing=%v qty=%v progress=%+v pnl=%v", s.isClosing, s.totalQty, s.closeProgress, s.stats.TotalPnL)
			}
		})
	}
}

func TestMartingaleCloseBeyondAttributedInventoryRequestsReconciliation(t *testing.T) {
	executor := &martingaleReconciliationExecutor{}
	s := NewMartingaleStrategy("martingale", "BTCUSDT", &config.Config{}, executor, &hedgeExchange{}, nil)
	setTestRuntimeStateStore(t, s)
	s.direction = "LONG"
	s.entries = []*MartingaleEntry{{Level: 1, Price: 100, Quantity: 0.5, Cost: 50, Status: entryStatusFilled}}
	s.updateTotals()
	s.isClosing, s.closeOrderID, s.closeRequestedQty = true, 42, 1
	if err := s.OnOrderUpdate(&position.OrderUpdate{
		OrderID: 42, Status: "PARTIALLY_FILLED", ExecutedQty: 1, AvgPrice: 110,
	}); err != nil {
		t.Fatal(err)
	}
	if !executor.marked || !s.isClosing || s.totalQty != 0.5 || s.closeProgress.Quantity != 0 || s.stats.TotalPnL != 0 {
		t.Fatalf("over-close must request reconciliation without consuming inventory: marked=%v closing=%v qty=%v progress=%+v pnl=%v", executor.marked, s.isClosing, s.totalQty, s.closeProgress, s.stats.TotalPnL)
	}
}

func TestMartingaleUnknownEntryPreservesPreviouslyVerifiedPartialInventoryAcrossRestore(t *testing.T) {
	store := &memoryRuntimeStateStore{}
	s := NewMartingaleStrategy("martingale", "BTCUSDT", &config.Config{}, &hedgeOrderExecutor{}, &hedgeExchange{}, nil)
	s.SetRuntimeStateStore(store)
	s.direction = "LONG"
	entry := &MartingaleEntry{
		Level: 1, OrderID: 43, Price: 100, Quantity: 0.4, RequestedQuantity: 1,
		Cost: 40, OpeningFee: 0.1, FillProgress: position.FillProgress{Quantity: 0.4, Notional: 40},
		Status: entryStatusPartiallyFilled,
	}
	s.entries = []*MartingaleEntry{entry}
	s.updateTotals()
	if err := s.OnOrderUpdate(&position.OrderUpdate{
		OrderID: 43, Status: "PARTIALLY_FILLED", ExecutedQty: 0.8, AvgPrice: math.NaN(),
	}); err != nil {
		t.Fatal(err)
	}
	if entry.Status != position.OrderStatusUnknown || s.totalQty != 0.4 || s.totalCost != 40 || s.openingFeeTotal() != 0.1 || !s.hasPendingEntry() {
		t.Fatalf("unknown order lost verified inventory or failed to block entries: entry=%+v qty=%v cost=%v fee=%v pending=%v", entry, s.totalQty, s.totalCost, s.openingFeeTotal(), s.hasPendingEntry())
	}
	s.updateTotals()
	if s.totalQty != 0.4 || s.totalCost != 40 {
		t.Fatalf("recalculation dropped verified unknown-entry inventory: qty=%v cost=%v", s.totalQty, s.totalCost)
	}

	restored := NewMartingaleStrategy("martingale", "BTCUSDT", &config.Config{}, &hedgeOrderExecutor{}, &hedgeExchange{}, nil)
	restored.direction = "LONG"
	restored.SetRuntimeStateStore(store)
	if err := restored.restoreRuntimeState(); err != nil {
		t.Fatalf("restore unknown entry: %v", err)
	}
	if restored.totalQty != 0.4 || restored.totalCost != 40 || restored.openingFeeTotal() != 0.1 || !restored.hasPendingEntry() {
		t.Fatalf("restored unknown entry lost verified position or lock: entry=%+v qty=%v cost=%v fee=%v pending=%v", restored.entries[0], restored.totalQty, restored.totalCost, restored.openingFeeTotal(), restored.hasPendingEntry())
	}
}

func TestMartingaleUnknownEntryBlocksAutomaticCloseUntilReconciled(t *testing.T) {
	executor := &hedgeOrderExecutor{}
	s := NewMartingaleStrategy("martingale", "BTCUSDT", &config.Config{}, executor, &hedgeExchange{}, nil)
	s.direction = "LONG"
	s.entries = []*MartingaleEntry{{
		Level: 1, OrderID: 44, Price: 100, Quantity: 0.4, RequestedQuantity: 1,
		Cost: 40, FillProgress: position.FillProgress{Quantity: 0.4, Notional: 40},
		Status: position.OrderStatusUnknown,
	}}
	s.updateTotals()
	if err := s.closeAllPositions(90, "止损"); err == nil {
		t.Fatal("must not submit a close while an entry order has unresolved execution")
	}
	if len(executor.orders) != 0 || s.totalQty != 0.4 || s.isClosing {
		t.Fatalf("unresolved entry should block auto-close without changing verified inventory: orders=%d qty=%v closing=%v", len(executor.orders), s.totalQty, s.isClosing)
	}
}

func TestMartingaleCloseWaitsForPendingEntryTerminalBeforeSubmitting(t *testing.T) {
	executor := &cancelRecordingExecutor{}
	s := NewMartingaleStrategy("martingale", "BTCUSDT", &config.Config{}, executor, &hedgeExchange{}, nil)
	setTestRuntimeStateStore(t, s)
	s.direction = "LONG"
	s.entries = []*MartingaleEntry{
		{Level: 1, Price: 100, Quantity: 1, Cost: 100, Status: entryStatusFilled},
		{Level: 2, OrderID: 45, Price: 90, RequestedQuantity: 0.5, Status: entryStatusPending},
	}
	s.updateTotals()
	if err := s.closeAllPositions(80, "止损"); err != nil {
		t.Fatal(err)
	}
	if len(executor.canceled) != 1 || executor.canceled[0] != 45 || len(executor.orders) != 0 || s.isClosing {
		t.Fatalf("close submitted before pending entry reached terminal state: canceled=%v orders=%d closing=%v", executor.canceled, len(executor.orders), s.isClosing)
	}
	if s.pendingCloseReason != "止损" {
		t.Fatalf("pending close reason was not retained: %q", s.pendingCloseReason)
	}
	if err := s.closeAllPositions(80, "止损"); err != nil {
		t.Fatal(err)
	}
	if len(executor.canceled) != 1 {
		t.Fatalf("repeated close evaluation should throttle duplicate cancel requests: %v", executor.canceled)
	}
	if err := s.OnOrderUpdate(&position.OrderUpdate{OrderID: 45, Status: "CANCELED"}); err != nil {
		t.Fatal(err)
	}
	if len(s.entries) != 1 || s.totalQty != 1 {
		t.Fatalf("canceled unfilled entry not released: entries=%+v qty=%v", s.entries, s.totalQty)
	}
	if err := s.closeAllPositions(80, "止损"); err != nil {
		t.Fatal(err)
	}
	if len(executor.orders) != 1 || executor.orders[0].Quantity != 1 || !s.isClosing {
		t.Fatalf("close was not submitted against confirmed inventory after terminal cancel: orders=%+v qty=%v closing=%v", executor.orders, s.totalQty, s.isClosing)
	}
}

func TestMartingalePendingCloseIntentSurvivesRestartAndUsesFreshPrice(t *testing.T) {
	store := &memoryRuntimeStateStore{}
	firstExecutor := &cancelRecordingExecutor{}
	first := NewMartingaleStrategy("martingale", "BTCUSDT", &config.Config{}, firstExecutor, &hedgeExchange{}, nil)
	setTestRuntimeStateStore(t, first)
	first.direction = "LONG"
	first.entries = []*MartingaleEntry{
		{Level: 1, Price: 100, Quantity: 1, Cost: 100, Status: entryStatusFilled},
		{Level: 2, OrderID: 46, Price: 90, RequestedQuantity: 0.5, Status: entryStatusPending},
	}
	first.updateTotals()
	first.SetRuntimeStateStore(store)
	if err := first.closeAllPositions(80, "止损"); err != nil {
		t.Fatal(err)
	}
	var persisted martingaleRuntimeState
	if !store.found || json.Unmarshal([]byte(store.payload), &persisted) != nil || persisted.PendingCloseReason != "止损" {
		t.Fatalf("close intent was not durably persisted before cancellation: %s", store.payload)
	}

	secondExecutor := &hedgeOrderExecutor{}
	secondExchange := &martingaleEntryReconcileExchange{hedgeExchange: &hedgeExchange{}, order: &exchange.Order{
		OrderID: 46, Symbol: "BTCUSDT", Side: exchange.SideBuy, Quantity: 0.5, Status: exchange.OrderStatusNew,
	}}
	second := NewMartingaleStrategy("martingale", "BTCUSDT", &config.Config{}, secondExecutor, secondExchange, nil)
	second.SetRuntimeStateStore(store)
	if err := second.Start(t.Context()); err != nil {
		t.Fatalf("restore pending close: %v", err)
	}
	if second.pendingCloseReason != "止损" || !second.hasPendingEntry() {
		t.Fatalf("restored state lost pending close/entry order: reason=%q entries=%+v", second.pendingCloseReason, second.entries)
	}
	if err := second.OnOrderUpdate(&position.OrderUpdate{OrderID: 46, Status: "CANCELED"}); err != nil {
		t.Fatal(err)
	}
	if err := second.OnPriceChange(85); err != nil {
		t.Fatalf("resume pending close at fresh price: %v", err)
	}
	if len(secondExecutor.orders) != 1 || secondExecutor.orders[0].Price != 85 || secondExecutor.orders[0].Quantity != 1 || !second.isClosing || second.pendingCloseReason != "" {
		t.Fatalf("pending stop was not resumed with fresh price and confirmed inventory: orders=%+v qty=%v closing=%v pending=%q", secondExecutor.orders, second.totalQty, second.isClosing, second.pendingCloseReason)
	}
}

func TestMartingaleUnknownFeeAssetDoesNotConsumeFill(t *testing.T) {
	s := NewMartingaleStrategy("martingale", "BTCUSDT", &config.Config{}, &hedgeOrderExecutor{}, &hedgeExchange{}, nil)
	setTestRuntimeStateStore(t, s)
	s.direction = "LONG"
	entry := &MartingaleEntry{Level: 1, OrderID: 21, Status: entryStatusPending}
	s.entries = []*MartingaleEntry{entry}
	s.handleEntryOrderUpdate(entry, &position.OrderUpdate{
		OrderID: 21, Status: "PARTIALLY_FILLED", ExecutedQty: 0.5, AvgPrice: 100,
		Commission: 0.01, CommissionAsset: "BNB",
	})
	if entry.FillProgress.Quantity != 0 || entry.Quantity != 0 || s.totalQty != 0 {
		t.Fatalf("unknown fee currency consumed fill: entry=%+v total=%v", entry, s.totalQty)
	}
}
