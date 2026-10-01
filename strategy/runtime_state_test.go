package strategy

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"testing"

	"quantmesh/config"
	"quantmesh/exchange"
	"quantmesh/execution"
	"quantmesh/position"
)

type memoryRuntimeStateStore struct {
	version int
	payload string
	found   bool
	err     error
}

type martingaleCloseReconcileExchange struct {
	*hedgeExchange
	order *exchange.Order
	fills []*exchange.OrderFill
}

type martingaleEntryReconcileExchange struct {
	*hedgeExchange
	order    *exchange.Order
	fills    []*exchange.OrderFill
	orderErr error
}

func (e *martingaleEntryReconcileExchange) GetOrder(context.Context, string, int64) (interface{}, error) {
	return e.order, e.orderErr
}

func (e *martingaleEntryReconcileExchange) GetOrderFills(context.Context, string, int64) (interface{}, error) {
	return e.fills, nil
}

type martingaleCloseOpenOnlyExchange struct {
	*hedgeExchange
	orders []*exchange.Order
}

func (e *martingaleCloseOpenOnlyExchange) GetOpenOrders(context.Context, string) (interface{}, error) {
	return e.orders, nil
}

type martingaleCloseIntentExecutor struct {
	hedgeOrderExecutor
	store *memoryRuntimeStateStore
	calls int
}

func (e *martingaleCloseIntentExecutor) PlaceOrder(req *position.OrderRequest) (*position.Order, error) {
	e.calls++
	var state martingaleRuntimeState
	if err := json.Unmarshal([]byte(e.store.payload), &state); err != nil {
		return nil, err
	}
	if req.ClientOrderID == "" || state.CloseClientOrderID != req.ClientOrderID || state.CloseRequestedQty != req.Quantity || state.CloseReason == "" {
		return nil, errors.New("close identity was not durably persisted before submission")
	}
	return nil, execution.ErrOrderUnknown
}

func (e *martingaleCloseReconcileExchange) GetOrderByClientOrderID(context.Context, string, string) (*exchange.Order, error) {
	return e.order, nil
}

func (e *martingaleCloseReconcileExchange) GetOrderFills(context.Context, string, int64) (interface{}, error) {
	return e.fills, nil
}

func setTestRuntimeStateStore(t *testing.T, strategy interface{ SetRuntimeStateStore(RuntimeStateStore) }) {
	t.Helper()
	strategy.SetRuntimeStateStore(&memoryRuntimeStateStore{})
}

func (m *memoryRuntimeStateStore) LoadRuntimeState(string) (int, string, bool, error) {
	return m.version, m.payload, m.found, m.err
}

func (m *memoryRuntimeStateStore) SaveRuntimeState(_ string, version int, payload string) error {
	if m.err != nil {
		return m.err
	}
	m.version, m.payload, m.found = version, payload, true
	return nil
}

type dcaIntentObservingExecutor struct {
	hedgeOrderExecutor
	store     *memoryRuntimeStateStore
	submitErr error
	observed  bool
}

func (e *dcaIntentObservingExecutor) PlaceOrder(request *position.OrderRequest) (*position.Order, error) {
	var state dcaRuntimeState
	if err := json.Unmarshal([]byte(e.store.payload), &state); err != nil {
		return nil, err
	}
	if request.Side == "BUY" {
		for _, layer := range state.Layers {
			if layer.ClientOrderID == request.ClientOrderID && layer.Status == entryStatusPending &&
				layer.RequestedQuantity == request.Quantity && layer.Quantity == 0 && layer.Cost == 0 {
				e.observed = true
			}
		}
	} else if request.Side == "SELL" && state.IsClosing && state.CloseClientOrderID == request.ClientOrderID &&
		state.CloseOrderID == 0 && state.CloseRequestedQty == request.Quantity {
		e.observed = true
	}
	if !e.observed {
		return nil, errors.New("DCA order intent was not durably recorded before submission")
	}
	if e.submitErr != nil {
		return nil, e.submitErr
	}
	e.orders = append(e.orders, request)
	return &position.Order{OrderID: int64(len(e.orders)), ClientOrderID: request.ClientOrderID,
		Side: request.Side, Quantity: request.Quantity, Price: request.Price}, nil
}

func TestDCAOrderFillPersistsAndRestoresFeeBearingInventory(t *testing.T) {
	store := &memoryRuntimeStateStore{}
	cfg := &config.Config{}
	first := NewDCAEnhancedStrategy("dca", "BTCUSDT", cfg, &hedgeOrderExecutor{}, &hedgeExchange{}, nil)
	first.SetRuntimeStateStore(store)
	if err := first.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	first.layers = []*DCALayer{{Index: 0, Quantity: 0.5, RequestedQuantity: 0.5, OrderID: 77, Status: entryStatusPending}}
	if err := first.OnOrderUpdate(&position.OrderUpdate{
		OrderID: 77, Status: "PARTIALLY_FILLED", ExecutedQty: 0.5, AvgPrice: 100,
		Commission: 0.1, CommissionAsset: "USDT", CommissionKnown: true,
	}); err != nil {
		t.Fatal(err)
	}
	if !store.found {
		t.Fatal("fill transition was not durably stored")
	}

	secondExchange := &dcaRecoveryExchange{hedgeExchange: &hedgeExchange{}, order: &exchange.Order{
		OrderID: 77, Symbol: "BTCUSDT", Side: exchange.SideBuy, Quantity: 0.5,
		ExecutedQty: 0.5, AvgPrice: 100, Status: exchange.OrderStatusPartiallyFilled,
	}}
	second := NewDCAEnhancedStrategy("dca", "BTCUSDT", cfg, &hedgeOrderExecutor{}, secondExchange, nil)
	second.SetRuntimeStateStore(store)
	if err := second.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	second.lastPrice = 110
	positions := second.GetPositions()
	if len(positions) != 1 || positions[0].Size != 0.5 || positions[0].OpeningFee != 0.1 || math.Abs(positions[0].PnL-4.9) > 1e-9 {
		t.Fatalf("restored inventory mismatch: %+v", positions)
	}
}

func TestDCAPendingOrdersPersistRequestedSizeWithoutInventingInventory(t *testing.T) {
	tests := []struct {
		name  string
		place func(*DCAEnhancedStrategy) error
	}{
		{name: "base order", place: func(s *DCAEnhancedStrategy) error { return s.openBaseOrder(100) }},
		{name: "safety order", place: func(s *DCAEnhancedStrategy) error {
			s.layers = []*DCALayer{{Index: 0, Price: 100, Quantity: 1, Cost: 100, RequestedQuantity: 1,
				FillProgress: position.FillProgress{Quantity: 1, Notional: 100}, Status: entryStatusFilled}}
			s.totalQty, s.totalCost, s.avgEntryPrice = 1, 100, 100
			s.currentLayer, s.dynamicInterval = 1, 1
			return s.checkSafetyOrder(98)
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := &memoryRuntimeStateStore{}
			s := NewDCAEnhancedStrategy("dca", "BTCUSDT", &config.Config{}, &hedgeOrderExecutor{}, &hedgeExchange{}, nil)
			s.SetRuntimeStateStore(store)
			if err := test.place(s); err != nil {
				t.Fatalf("place pending order: %v", err)
			}
			pending := s.layers[len(s.layers)-1]
			if pending.Status != entryStatusPending || pending.RequestedQuantity <= 0 || pending.Quantity != 0 || pending.Cost != 0 {
				t.Fatalf("pending request was mixed into filled inventory: %+v", pending)
			}
			orders := s.GetOrders()
			if len(orders) == 0 || orders[len(orders)-1].Quantity != pending.RequestedQuantity {
				t.Fatalf("pending order view lost requested quantity: %+v", orders)
			}

			restarted := NewDCAEnhancedStrategy("dca", "BTCUSDT", &config.Config{}, &hedgeOrderExecutor{}, &hedgeExchange{}, nil)
			restarted.SetRuntimeStateStore(store)
			if err := restarted.restoreRuntimeState(); err != nil {
				t.Fatalf("restore persisted pending order: %v", err)
			}
			if restarted.totalQty != s.totalQty || restarted.totalCost != s.totalCost ||
				restarted.layers[len(restarted.layers)-1].RequestedQuantity != pending.RequestedQuantity {
				t.Fatalf("restored pending order/inventory mismatch: %+v", restarted.runtimeStateSnapshotLocked())
			}
		})
	}
}

func TestDCARestoresLegacyPendingLayerWithoutCountingRequestedInventory(t *testing.T) {
	state := dcaRuntimeState{
		StrategyName: "dca", Symbol: "BTCUSDT", CurrentLayer: 1, CloseLayerIndex: -1,
		Layers: []*DCALayer{{Index: 0, Price: 100, Quantity: 1, Cost: 100, OrderID: 77, Status: entryStatusPending}},
	}
	ex := &dcaRecoveryExchange{hedgeExchange: &hedgeExchange{}, order: &exchange.Order{
		OrderID: 77, Symbol: "BTCUSDT", Side: exchange.SideBuy, Quantity: 1, Status: exchange.OrderStatusNew,
	}}
	s := newPersistedDCAStrategy(t, ex, state)
	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("restore and reconcile legacy pending order: %v", err)
	}
	defer s.Stop()
	layer := s.layers[0]
	if layer.RequestedQuantity != 1 || layer.Quantity != 0 || layer.Cost != 0 || s.totalQty != 0 || s.totalCost != 0 {
		t.Fatalf("legacy request was not migrated without inventing fills: layer=%+v qty=%v cost=%v", layer, s.totalQty, s.totalCost)
	}
}

func TestDCAEntryAndCloseIntentsAreDurableBeforeExchangeSubmission(t *testing.T) {
	tests := []struct {
		name  string
		place func(*DCAEnhancedStrategy) error
	}{
		{name: "entry", place: func(s *DCAEnhancedStrategy) error { return s.openBaseOrder(100) }},
		{name: "close", place: func(s *DCAEnhancedStrategy) error {
			s.layers = []*DCALayer{{Index: 0, Price: 100, Quantity: 1, Cost: 100, RequestedQuantity: 1,
				FillProgress: position.FillProgress{Quantity: 1, Notional: 100}, Status: entryStatusFilled}}
			s.totalQty, s.totalCost, s.avgEntryPrice = 1, 100, 100
			return s.closeAllPositions(110, "take profit")
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := &memoryRuntimeStateStore{}
			executor := &dcaIntentObservingExecutor{store: store}
			s := NewDCAEnhancedStrategy("dca", "BTCUSDT", &config.Config{}, executor, &hedgeExchange{}, nil)
			s.SetRuntimeStateStore(store)
			if err := test.place(s); err != nil {
				t.Fatalf("submit order: %v", err)
			}
			if !executor.observed {
				t.Fatal("executor was called without observing a persisted intent")
			}
		})
	}
}

func TestDCARecoversUnknownEntryAndCloseSubmissionByClientOrderID(t *testing.T) {
	tests := []struct {
		name string
		open bool
		side exchange.Side
	}{
		{name: "entry", open: true, side: exchange.SideBuy},
		{name: "close", open: false, side: exchange.SideSell},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := &memoryRuntimeStateStore{}
			executor := &dcaIntentObservingExecutor{store: store, submitErr: errors.New("response lost")}
			ex := &hedgeExchange{}
			s := NewDCAEnhancedStrategy("dca", "BTCUSDT", &config.Config{}, executor, ex, nil)
			s.SetRuntimeStateStore(store)
			if !test.open {
				s.layers = []*DCALayer{{Index: 0, Price: 100, Quantity: 1, Cost: 100, RequestedQuantity: 1,
					FillProgress: position.FillProgress{Quantity: 1, Notional: 100}, Status: entryStatusFilled}}
				s.totalQty, s.totalCost, s.avgEntryPrice = 1, 100, 100
			}
			var submitErr error
			if test.open {
				submitErr = s.openBaseOrder(100)
			} else {
				submitErr = s.closeAllPositions(110, "take profit")
			}
			if submitErr == nil || !executor.observed {
				t.Fatalf("ambiguous placement did not retain a pre-persisted intent: err=%v observed=%v", submitErr, executor.observed)
			}
			var state dcaRuntimeState
			if err := json.Unmarshal([]byte(store.payload), &state); err != nil {
				t.Fatal(err)
			}
			clientOrderID := ""
			quantity := 0.0
			if test.open {
				layer := state.Layers[len(state.Layers)-1]
				clientOrderID, quantity = layer.ClientOrderID, layer.RequestedQuantity
				if layer.Status != position.OrderStatusUnknown || layer.OrderID != 0 {
					t.Fatalf("entry intent did not retain unknown submission state: %+v", layer)
				}
			} else {
				clientOrderID, quantity = state.CloseClientOrderID, state.CloseRequestedQty
				if !state.IsClosing || state.CloseOrderID != 0 {
					t.Fatalf("close intent did not retain unknown submission state: %+v", state)
				}
			}
			if clientOrderID == "" || quantity <= 0 {
				t.Fatalf("persisted intent is missing identity/quantity: %+v", state)
			}

			recoveryExchange := &dcaRecoveryExchange{hedgeExchange: &hedgeExchange{}, lookupOrder: &exchange.Order{
				OrderID: 777, ClientOrderID: clientOrderID, Symbol: "BTCUSDT", Side: test.side,
				Quantity: quantity, Status: exchange.OrderStatusNew,
			}}
			restarted := NewDCAEnhancedStrategy("dca", "BTCUSDT", &config.Config{}, &hedgeOrderExecutor{}, recoveryExchange, nil)
			restarted.SetRuntimeStateStore(store)
			if err := restarted.Start(context.Background()); err != nil {
				t.Fatalf("recover unknown submission by client order ID: %v", err)
			}
			defer restarted.Stop()
			if len(recoveryExchange.lookupClientOrderIDs) != 1 || recoveryExchange.lookupClientOrderIDs[0] != clientOrderID {
				t.Fatalf("recovery queried wrong client identity: %v", recoveryExchange.lookupClientOrderIDs)
			}
			if test.open && (restarted.layers[0].OrderID != 777 || restarted.layers[0].Status != entryStatusPending) {
				t.Fatalf("entry identity was not bound to recovered order: %+v", restarted.layers[0])
			}
			if !test.open && (!restarted.isClosing || restarted.closeOrderID != 777 || restarted.closeClientOrderID != clientOrderID) {
				t.Fatalf("close identity was not bound to recovered order: closing=%v order=%d cid=%s", restarted.isClosing, restarted.closeOrderID, restarted.closeClientOrderID)
			}
		})
	}
}

func TestDCARuntimeStateMigrationResetsGrossTrailingPeak(t *testing.T) {
	strategy := NewDCAEnhancedStrategy("dca", "BTCUSDT", &config.Config{}, &hedgeOrderExecutor{}, &hedgeExchange{}, nil)
	legacy := dcaRuntimeState{
		BotID: strategy.effectiveBotID(), StrategyName: "dca", Symbol: "BTCUSDT", CurrentLayer: 0,
		HighestProfit: 2.5, TakeProfitTriggered: true,
		Stats: StrategyStatistics{}, Layers: []*DCALayer{}, CloseLayerIndex: -1,
	}
	payload, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	store := &memoryRuntimeStateStore{version: 1, payload: string(payload), found: true}
	strategy.SetRuntimeStateStore(store)
	if err := strategy.Start(context.Background()); err != nil {
		t.Fatalf("Start() migration error = %v", err)
	}
	defer strategy.Stop()
	if store.version != dcaRuntimeStateSchemaVersion {
		t.Fatalf("runtime state version = %d, want %d", store.version, dcaRuntimeStateSchemaVersion)
	}
	var migrated dcaRuntimeState
	if err := json.Unmarshal([]byte(store.payload), &migrated); err != nil {
		t.Fatal(err)
	}
	if migrated.HighestProfit != 0 || migrated.TakeProfitTriggered {
		t.Fatalf("legacy gross-return trailing state was not reset: peak=%v triggered=%v", migrated.HighestProfit, migrated.TakeProfitTriggered)
	}
}

func TestDCAInvalidPersistedIdentityBlocksStart(t *testing.T) {
	store := &memoryRuntimeStateStore{version: dcaRuntimeStateSchemaVersion, payload: `{ "bot_id": "wrong" }`, found: true}
	dca := NewDCAEnhancedStrategy("dca", "BTCUSDT", &config.Config{}, &hedgeOrderExecutor{}, &hedgeExchange{}, nil)
	dca.SetRuntimeStateStore(store)
	if err := dca.Start(context.Background()); err == nil {
		t.Fatal("expected corrupt/mismatched state to block startup")
	}
	if dca.IsRunning() {
		t.Fatal("strategy started despite invalid persisted state")
	}
}

func TestDCAAndMartingaleRequireDurableRuntimeState(t *testing.T) {
	dca := NewDCAEnhancedStrategy("dca", "BTCUSDT", &config.Config{}, &hedgeOrderExecutor{}, &hedgeExchange{}, nil)
	if err := dca.Start(context.Background()); err == nil {
		t.Fatal("DCA started without durable runtime state")
	}
	if dca.IsRunning() {
		t.Fatal("DCA marked itself running without durable runtime state")
	}

	martin := NewMartingaleStrategy("martingale", "BTCUSDT", &config.Config{}, &hedgeOrderExecutor{}, &hedgeExchange{}, nil)
	if err := martin.Start(context.Background()); err == nil {
		t.Fatal("martingale started without durable runtime state")
	}
	if martin.IsRunning() {
		t.Fatal("martingale marked itself running without durable runtime state")
	}
}

func TestMartingaleReversePendingOrderDoesNotCountRequestedInventory(t *testing.T) {
	executor := &hedgeOrderExecutor{}
	martin := NewMartingaleStrategy("martingale", "BTCUSDT", &config.Config{}, executor, &hedgeExchange{}, map[string]interface{}{
		"reverse_multiplier": 1.5,
		"price_step":         2.0,
	})
	setTestRuntimeStateStore(t, martin)
	martin.entries = []*MartingaleEntry{{Level: 0, Price: 100, Quantity: 1, Cost: 100, Status: entryStatusFilled}}
	martin.currentLevel = 1
	martin.updateTotals()
	if err := martin.checkReverseMartingale(110); err != nil {
		t.Fatal(err)
	}
	if len(martin.entries) != 2 {
		t.Fatalf("entries=%d want 2", len(martin.entries))
	}
	pending := martin.entries[1]
	if pending.Quantity != 0 || pending.Cost != 0 || pending.RequestedQuantity <= 0 {
		t.Fatalf("pending order was pre-counted as filled inventory: %+v", pending)
	}
	martin.handleEntryOrderUpdate(pending, &position.OrderUpdate{
		OrderID: pending.OrderID, Status: "PARTIALLY_FILLED", ExecutedQty: 0.5,
		AvgPrice: 110, Commission: 0.05, CommissionAsset: "USDT", CommissionKnown: true,
	})
	if math.Abs(martin.totalQty-1.5) > 1e-9 || math.Abs(martin.totalCost-155) > 1e-9 {
		t.Fatalf("inventory counted requested amount instead of fill: qty=%v cost=%v", martin.totalQty, martin.totalCost)
	}
}

func TestMartingaleRuntimeStateLoadFailureBlocksStart(t *testing.T) {
	store := &memoryRuntimeStateStore{err: errors.New("database unavailable")}
	martin := NewMartingaleStrategy("martingale", "BTCUSDT", &config.Config{}, &hedgeOrderExecutor{}, &hedgeExchange{}, nil)
	martin.SetRuntimeStateStore(store)
	if err := martin.Start(context.Background()); err == nil {
		t.Fatal("expected state load failure to prevent strategy startup")
	}
	if martin.IsRunning() {
		t.Fatal("strategy started despite state load failure")
	}
}

func TestMartingaleRuntimeStateRejectsAveragePriceMismatch(t *testing.T) {
	state := martingaleRuntimeState{
		StrategyName: "martingale", Symbol: "BTCUSDT", Direction: "LONG",
		Entries:  []*MartingaleEntry{{Level: 1, Price: 100, Quantity: 1, Cost: 100, Status: entryStatusFilled}},
		TotalQty: 1, TotalCost: 100, AvgEntryPrice: 90,
	}
	payload, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	store := &memoryRuntimeStateStore{version: martingaleRuntimeStateSchemaVersion, payload: string(payload), found: true}
	martin := NewMartingaleStrategy("martingale", "BTCUSDT", &config.Config{}, &hedgeOrderExecutor{}, &hedgeExchange{}, nil)
	martin.SetRuntimeStateStore(store)
	if err := martin.Start(context.Background()); err == nil {
		t.Fatal("martingale started with an average entry price inconsistent with inventory cost")
	}
	if martin.IsRunning() {
		t.Fatal("martingale marked itself running despite an invalid restored risk basis")
	}
}

func TestMartingaleRuntimeStateRejectsInvalidEntrySnapshots(t *testing.T) {
	validEntry := func() *MartingaleEntry {
		return &MartingaleEntry{Level: 1, Price: 100, Quantity: 1, RequestedQuantity: 1, Cost: 100,
			FillProgress: position.FillProgress{Quantity: 1, Notional: 100}, OrderID: 81, Status: entryStatusFilled}
	}
	tests := []struct {
		name    string
		entries []*MartingaleEntry
	}{
		{name: "duplicate levels", entries: []*MartingaleEntry{validEntry(), validEntry()}},
		{name: "unknown status", entries: []*MartingaleEntry{{Level: 1, Quantity: 1, Cost: 100, Status: "mystery"}}},
		{name: "fill exceeds request", entries: []*MartingaleEntry{{Level: 1, Price: 100, Quantity: 1.1, RequestedQuantity: 1, Cost: 110, FillProgress: position.FillProgress{Quantity: 1.1, Notional: 110}, Status: entryStatusFilled}}},
		{name: "partial fill without progress", entries: []*MartingaleEntry{{Level: 1, Price: 100, Quantity: 1, Cost: 100, Status: entryStatusPartiallyFilled}}},
		{name: "duplicate active order IDs", entries: []*MartingaleEntry{
			{Level: 1, RequestedQuantity: 1, OrderID: 82, Status: entryStatusPending},
			{Level: 2, RequestedQuantity: 1, OrderID: 82, Status: entryStatusPending},
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			state := martingaleRuntimeState{StrategyName: "martingale", Symbol: "BTCUSDT", Direction: "LONG", Entries: tc.entries}
			for _, entry := range tc.entries {
				if martingaleEntryHasAttributedFill(entry) {
					state.TotalQty += entry.Quantity
					state.TotalCost += entry.Cost
				}
			}
			if state.TotalQty > 0 {
				state.AvgEntryPrice = state.TotalCost / state.TotalQty
			}
			payload, err := json.Marshal(state)
			if err != nil {
				t.Fatal(err)
			}
			store := &memoryRuntimeStateStore{version: martingaleRuntimeStateSchemaVersion, payload: string(payload), found: true}
			martin := NewMartingaleStrategy("martingale", "BTCUSDT", &config.Config{}, &hedgeOrderExecutor{}, &hedgeExchange{}, nil)
			martin.SetRuntimeStateStore(store)
			if err := martin.Start(context.Background()); err == nil {
				t.Fatal("martingale started with an invalid persisted entry")
			}
			if martin.IsRunning() {
				t.Fatal("martingale entered running state despite an invalid persisted entry")
			}
		})
	}
}

func TestMartingaleRuntimeStateRejectsInconsistentCloseProgress(t *testing.T) {
	tests := []struct {
		name  string
		state martingaleRuntimeState
	}{
		{
			name: "active close without requested quantity",
			state: martingaleRuntimeState{
				StrategyName: "martingale", Symbol: "BTCUSDT", Direction: "LONG", IsClosing: true, CloseOrderID: 9,
				Entries:  []*MartingaleEntry{{Level: 1, Price: 100, Quantity: 1, Cost: 100, Status: entryStatusFilled}},
				TotalQty: 1, TotalCost: 100, AvgEntryPrice: 100,
			},
		},
		{
			name: "progress without active close",
			state: martingaleRuntimeState{
				StrategyName: "martingale", Symbol: "BTCUSDT", Direction: "LONG", CloseRequestedQty: 1,
				Entries:  []*MartingaleEntry{{Level: 1, Price: 100, Quantity: 1, Cost: 100, Status: entryStatusFilled}},
				TotalQty: 1, TotalCost: 100, AvgEntryPrice: 100,
			},
		},
		{
			name: "pending intent without inventory",
			state: martingaleRuntimeState{
				StrategyName: "martingale", Symbol: "BTCUSDT", Direction: "LONG", PendingCloseReason: "止损",
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			payload, err := json.Marshal(tc.state)
			if err != nil {
				t.Fatal(err)
			}
			store := &memoryRuntimeStateStore{version: martingaleRuntimeStateSchemaVersion, payload: string(payload), found: true}
			martin := NewMartingaleStrategy("martingale", "BTCUSDT", &config.Config{}, &hedgeOrderExecutor{}, &hedgeExchange{}, nil)
			martin.SetRuntimeStateStore(store)
			if err := martin.Start(context.Background()); err == nil {
				t.Fatal("martingale started with inconsistent persisted close state")
			}
			if martin.IsRunning() {
				t.Fatal("martingale entered running state despite inconsistent close state")
			}
		})
	}
}

func TestMartingaleStartReconcilesPersistedCloseCIDAndFills(t *testing.T) {
	state := martingaleRuntimeState{
		StrategyName: "martingale", Symbol: "BTCUSDT", Direction: "LONG",
		Entries:  []*MartingaleEntry{{Level: 1, Price: 100, Quantity: 1, Cost: 100, Status: entryStatusFilled}},
		TotalQty: 1, TotalCost: 100, AvgEntryPrice: 100,
		CloseClientOrderID: "close-intent-1", CloseReason: "止损", CloseRequestedQty: 1,
	}
	ex := &martingaleCloseReconcileExchange{
		hedgeExchange: &hedgeExchange{},
		order: &exchange.Order{OrderID: 77, ClientOrderID: "close-intent-1", Symbol: "BTCUSDT", Side: exchange.SideSell,
			Quantity: 1, ExecutedQty: 0.4, AvgPrice: 110, Status: exchange.OrderStatusPartiallyFilled},
		fills: []*exchange.OrderFill{{OrderID: 77, TradeID: "trade-1", Symbol: "BTCUSDT", Side: exchange.SideSell,
			Price: 110, Quantity: 0.4, Commission: 0.01, CommissionAsset: "USDT"}},
	}
	executor := &hedgeOrderExecutor{}
	martin := NewMartingaleStrategy("martingale", "BTCUSDT", &config.Config{}, executor, ex, nil)
	state.BotID = martin.effectiveBotID()
	payload, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	store := &memoryRuntimeStateStore{version: martingaleRuntimeStateSchemaVersion, payload: string(payload), found: true}
	martin.SetRuntimeStateStore(store)
	if err := martin.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !martin.isClosing || martin.closeOrderID != 77 || martin.closeClientOrderID != "close-intent-1" ||
		math.Abs(martin.closeProgress.Quantity-0.4) > 1e-9 || math.Abs(martin.totalQty-0.6) > 1e-9 || math.Abs(martin.stats.TotalPnL-3.99) > 1e-9 {
		t.Fatalf("close order/fill was not reconciled: closing=%v id=%d cid=%s progress=%+v qty=%v pnl=%v",
			martin.isClosing, martin.closeOrderID, martin.closeClientOrderID, martin.closeProgress, martin.totalQty, martin.stats.TotalPnL)
	}
	if len(executor.orders) != 0 {
		t.Fatalf("reconciliation unexpectedly submitted %d new order(s)", len(executor.orders))
	}
}

func TestMartingaleStartReconcilesPersistedEntryFillAndFee(t *testing.T) {
	ex := &martingaleEntryReconcileExchange{
		hedgeExchange: &hedgeExchange{},
		order: &exchange.Order{OrderID: 91, Symbol: "BTCUSDT", Side: exchange.SideBuy, Quantity: 1,
			ExecutedQty: 0.5, AvgPrice: 100, Status: exchange.OrderStatusPartiallyFilled},
		fills: []*exchange.OrderFill{{OrderID: 91, TradeID: "trade-91", Symbol: "BTCUSDT", Side: exchange.SideBuy,
			Price: 100, Quantity: 0.5, Commission: 0.05, CommissionAsset: "USDT"}},
	}
	martin := NewMartingaleStrategy("martingale", "BTCUSDT", &config.Config{}, &hedgeOrderExecutor{}, ex, nil)
	state := martingaleRuntimeState{BotID: martin.effectiveBotID(), StrategyName: "martingale", Symbol: "BTCUSDT", Direction: "LONG",
		Entries: []*MartingaleEntry{{Level: 0, Price: 100, RequestedQuantity: 1, OrderID: 91, Status: entryStatusPending}}}
	payload, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	martin.SetRuntimeStateStore(&memoryRuntimeStateStore{version: martingaleRuntimeStateSchemaVersion, payload: string(payload), found: true})
	if err := martin.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(martin.entries) != 1 || martin.entries[0].Status != entryStatusPartiallyFilled ||
		math.Abs(martin.totalQty-0.5) > 1e-9 || math.Abs(martin.totalCost-50) > 1e-9 ||
		math.Abs(martin.entries[0].OpeningFee-0.05) > 1e-9 || math.Abs(martin.entries[0].FillProgress.Quantity-0.5) > 1e-9 {
		t.Fatalf("persisted entry fill/fee was not recovered: entries=%+v qty=%v cost=%v", martin.entries, martin.totalQty, martin.totalCost)
	}
}

func TestMartingaleStartBlocksWhenPersistedEntryCannotBeReconciled(t *testing.T) {
	ex := &martingaleEntryReconcileExchange{hedgeExchange: &hedgeExchange{}}
	martin := NewMartingaleStrategy("martingale", "BTCUSDT", &config.Config{}, &hedgeOrderExecutor{}, ex, nil)
	state := martingaleRuntimeState{BotID: martin.effectiveBotID(), StrategyName: "martingale", Symbol: "BTCUSDT", Direction: "LONG",
		Entries: []*MartingaleEntry{{Level: 0, Price: 100, RequestedQuantity: 1, OrderID: 92, Status: entryStatusPending}}}
	payload, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	martin.SetRuntimeStateStore(&memoryRuntimeStateStore{version: martingaleRuntimeStateSchemaVersion, payload: string(payload), found: true})
	if err := martin.Start(context.Background()); err == nil {
		t.Fatal("strategy started without exchange evidence for persisted entry")
	}
	if martin.IsRunning() {
		t.Fatal("strategy entered running state without reconciling persisted entry")
	}
}

func TestMartingaleStartResolvesUnknownEntryWithAuthoritativeOpenOrder(t *testing.T) {
	ex := &martingaleEntryReconcileExchange{hedgeExchange: &hedgeExchange{}, order: &exchange.Order{
		OrderID: 93, Symbol: "BTCUSDT", Side: exchange.SideBuy, Quantity: 1, Status: exchange.OrderStatusNew,
	}}
	martin := NewMartingaleStrategy("martingale", "BTCUSDT", &config.Config{}, &hedgeOrderExecutor{}, ex, nil)
	state := martingaleRuntimeState{BotID: martin.effectiveBotID(), StrategyName: "martingale", Symbol: "BTCUSDT", Direction: "LONG",
		Entries: []*MartingaleEntry{{Level: 0, Price: 100, RequestedQuantity: 1, OrderID: 93, Status: position.OrderStatusUnknown}}}
	payload, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	martin.SetRuntimeStateStore(&memoryRuntimeStateStore{version: martingaleRuntimeStateSchemaVersion, payload: string(payload), found: true})
	if err := martin.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(martin.entries) != 1 || martin.entries[0].Status != entryStatusPending {
		t.Fatalf("verified open entry was not restored to pending: %+v", martin.entries)
	}
}

func TestMartingaleStartBlocksWhenPersistedCloseCIDIsNotFound(t *testing.T) {
	state := martingaleRuntimeState{
		StrategyName: "martingale", Symbol: "BTCUSDT", Direction: "LONG",
		Entries:  []*MartingaleEntry{{Level: 1, Price: 100, Quantity: 1, Cost: 100, Status: entryStatusFilled}},
		TotalQty: 1, TotalCost: 100, AvgEntryPrice: 100,
		CloseClientOrderID: "uncertain-close", CloseReason: "止损", CloseRequestedQty: 1,
	}
	ex := &martingaleCloseReconcileExchange{hedgeExchange: &hedgeExchange{}}
	executor := &hedgeOrderExecutor{}
	martin := NewMartingaleStrategy("martingale", "BTCUSDT", &config.Config{}, executor, ex, nil)
	state.BotID = martin.effectiveBotID()
	payload, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	store := &memoryRuntimeStateStore{version: martingaleRuntimeStateSchemaVersion, payload: string(payload), found: true}
	martin.SetRuntimeStateStore(store)
	if err := martin.Start(context.Background()); err == nil {
		t.Fatal("strategy started although order lookup returned no evidence")
	}
	if martin.IsRunning() || len(executor.orders) != 0 {
		t.Fatalf("uncertain close was retried or strategy started: running=%v orders=%d", martin.IsRunning(), len(executor.orders))
	}
}

func TestMartingaleCloseLookupFallsBackToExactOpenOrderCID(t *testing.T) {
	ex := &martingaleCloseOpenOnlyExchange{
		hedgeExchange: &hedgeExchange{},
		orders: []*exchange.Order{{OrderID: 81, ClientOrderID: "open-close-cid", Symbol: "BTCUSDT", Side: exchange.SideSell,
			Quantity: 1, Status: exchange.OrderStatusNew}},
	}
	martin := NewMartingaleStrategy("martingale", "BTCUSDT", &config.Config{}, &hedgeOrderExecutor{}, ex, nil)
	order, err := martin.lookupCloseOrder(context.Background(), "BTCUSDT", "open-close-cid")
	if err != nil || order == nil || order.OrderID != 81 {
		t.Fatalf("open-order fallback result=%+v err=%v", order, err)
	}
}

func TestMartingaleClosePersistsCIDBeforeSubmitAndBlocksRetriesWhenUnknown(t *testing.T) {
	store := &memoryRuntimeStateStore{}
	executor := &martingaleCloseIntentExecutor{store: store}
	martin := NewMartingaleStrategy("martingale", "BTCUSDT", &config.Config{}, executor, &hedgeExchange{}, nil)
	martin.SetRuntimeStateStore(store)
	martin.isRunning = true
	martin.entries = []*MartingaleEntry{{Level: 1, Price: 100, Quantity: 1, Cost: 100, Status: entryStatusFilled}}
	martin.updateTotals()
	martin.pendingCloseReason = "止损"
	martin.mu.Lock()
	err := martin.closeAllPositions(90, "止损")
	martin.mu.Unlock()
	if !errors.Is(err, execution.ErrOrderUnknown) {
		t.Fatalf("uncertain submission error=%v want ErrOrderUnknown", err)
	}
	if martin.closeClientOrderID == "" || executor.calls != 1 {
		t.Fatalf("durable close marker was not retained: cid=%q calls=%d", martin.closeClientOrderID, executor.calls)
	}
	if err := martin.onPrice(91, true); err == nil {
		t.Fatal("new strategy decisions were not blocked while close submission was unresolved")
	}
	if executor.calls != 1 {
		t.Fatalf("uncertain close was submitted again: calls=%d", executor.calls)
	}
}

func TestSpotShortPendingRepaymentRestoresBeforeStart(t *testing.T) {
	cfg := &config.Config{}
	cfg.Trading.BotID = "bot-spot-short"
	cfg.Trading.Symbol = "BTCUSDT"
	store := &memoryRuntimeStateStore{}
	first := newSpotShortForTest(&signalTestExecutor{}, &signalTestExchange{}, &mockMarginExchange{})
	first.cfg = cfg
	first.SetRuntimeStateStore(store)
	if err := first.decreaseShort(context.Background(), 0.25); err != nil {
		t.Fatal(err)
	}
	if !store.found {
		t.Fatal("buy order was accepted without persisting its pending repayment")
	}

	second := NewSpotShortStrategy("spot_short", cfg, &signalTestExecutor{}, &spotShortReconcileExchange{
		order: &exchange.Order{OrderID: 1, Symbol: "BTCUSDT", Side: exchange.SideBuy, Status: exchange.OrderStatusNew, Quantity: 0.25},
	}, &mockMarginExchange{}, map[string]interface{}{})
	second.SetRuntimeStateStore(store)
	if err := second.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if second.pendingRepay[1].OrderQuantity != 0.25 {
		t.Fatalf("pending repayment not restored: %+v", second.pendingRepay)
	}
}

func TestSpotShortRefusesStartWithoutDurableStateStore(t *testing.T) {
	cfg := &config.Config{}
	cfg.Trading.Symbol = "BTCUSDT"
	s := NewSpotShortStrategy("spot_short", cfg, &signalTestExecutor{}, &signalTestExchange{}, &mockMarginExchange{}, nil)
	if err := s.Start(context.Background()); err == nil {
		t.Fatal("spot short started without durable debt/order recovery")
	}
}

func TestDCAStateSnapshotRoundTripsCloseOrderLayerIdentity(t *testing.T) {
	dca := NewDCAEnhancedStrategy("dca", "BTCUSDT", &config.Config{}, nil, nil, nil)
	dca.layers = []*DCALayer{{Index: 0, Quantity: 1, Cost: 100, RequestedQuantity: 1, FillProgress: position.FillProgress{Quantity: 1, Notional: 100}, Status: entryStatusFilled}}
	dca.totalQty, dca.totalCost, dca.avgEntryPrice = 1, 100, 100
	dca.closeLayer = dca.layers[0]
	dca.isClosing, dca.closeOrderID = true, 99
	dca.closeRequestedQty, dca.closeLimitPrice = 1, 110
	state := dca.runtimeStateSnapshotLocked()
	data, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	store := &memoryRuntimeStateStore{version: dcaRuntimeStateSchemaVersion, payload: string(data), found: true}
	copy := NewDCAEnhancedStrategy("dca", "BTCUSDT", &config.Config{}, nil, nil, nil)
	copy.SetRuntimeStateStore(store)
	if err := copy.restoreRuntimeState(); err != nil {
		t.Fatal(err)
	}
	if copy.closeLayer == nil || copy.closeLayer.Index != 0 {
		t.Fatalf("close layer identity was not restored: %+v", copy.closeLayer)
	}
}

func TestDCARestoreRejectsInvalidCloseLayerAndProgress(t *testing.T) {
	base := dcaRuntimeState{
		BotID: "", StrategyName: "dca", Symbol: "BTCUSDT", TotalCost: 100, TotalQty: 1, AvgEntryPrice: 100,
		CurrentLayer: 1, IsClosing: true, CloseOrderID: 99, CloseLayerIndex: 3, CloseRequestedQty: 1,
		Layers: []*DCALayer{{Index: 0, Quantity: 1, Cost: 100, RequestedQuantity: 1, FillProgress: position.FillProgress{Quantity: 1, Notional: 100}, Status: entryStatusFilled}},
	}
	tests := []struct {
		name  string
		state dcaRuntimeState
	}{
		{name: "missing targeted layer", state: base},
		{name: "fill exceeds requested quantity", state: func() dcaRuntimeState { x := base; x.CloseLayerIndex = -1; x.CloseProgress.Quantity = 2; return x }()},
		{name: "negative opening fee", state: func() dcaRuntimeState {
			x := base
			x.CloseLayerIndex = -1
			x.Layers = []*DCALayer{{Index: 0, Quantity: 1, Cost: 100, OpeningFee: -0.1, RequestedQuantity: 1, FillProgress: position.FillProgress{Quantity: 1, Notional: 100}, Status: entryStatusFilled}}
			return x
		}()},
		{name: "duplicate layer index", state: func() dcaRuntimeState {
			x := base
			x.CloseLayerIndex = -1
			x.Layers = append(x.Layers, &DCALayer{Index: 0, Status: entryStatusPending})
			return x
		}()},
		{name: "unknown layer status", state: func() dcaRuntimeState {
			x := base
			x.CloseLayerIndex = -1
			x.Layers[0].Status = "mystery"
			return x
		}()},
		{name: "duplicate active order identity", state: func() dcaRuntimeState {
			return dcaRuntimeState{StrategyName: "dca", Symbol: "BTCUSDT", CurrentLayer: 2, CloseLayerIndex: -1,
				Layers: []*DCALayer{{Index: 0, OrderID: 91, RequestedQuantity: 1, Status: entryStatusPending},
					{Index: 1, OrderID: 91, RequestedQuantity: 1, Status: entryStatusPending}}}
		}()},
		{name: "targeted close exceeds its layer", state: func() dcaRuntimeState {
			x := base
			x.TotalQty, x.TotalCost, x.AvgEntryPrice = 1, 100, 100
			x.CloseLayerIndex = 0
			x.CloseRequestedQty = 0.75
			x.CurrentLayer = 2
			x.Layers = []*DCALayer{
				{Index: 0, Quantity: 0.5, Cost: 50, RequestedQuantity: 0.5, FillProgress: position.FillProgress{Quantity: 0.5, Notional: 50}, Status: entryStatusFilled},
				{Index: 1, Quantity: 0.5, Cost: 50, RequestedQuantity: 0.5, FillProgress: position.FillProgress{Quantity: 0.5, Notional: 50}, Status: entryStatusFilled},
			}
			return x
		}()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payload, err := json.Marshal(tt.state)
			if err != nil {
				t.Fatal(err)
			}
			store := &memoryRuntimeStateStore{version: dcaRuntimeStateSchemaVersion, payload: string(payload), found: true}
			s := NewDCAEnhancedStrategy("dca", "BTCUSDT", &config.Config{}, &hedgeOrderExecutor{}, &hedgeExchange{}, nil)
			s.SetRuntimeStateStore(store)
			if err := s.restoreRuntimeState(); err == nil {
				t.Fatal("expected invalid persisted DCA state to be rejected")
			}
			if len(s.layers) != 0 || s.closeOrderID != 0 {
				t.Fatalf("invalid state partially restored: layers=%+v closeOrderID=%d", s.layers, s.closeOrderID)
			}
		})
	}
}

func TestSignalStrategiesRestoreFeeBearingPositionAndActiveOrder(t *testing.T) {
	for _, strategyName := range []string{"trend", "mean_reversion", "momentum"} {
		t.Run(strategyName, func(t *testing.T) {
			cfg := &config.Config{}
			cfg.Trading.BotID = "bot-signal-restore"
			cfg.Trading.Symbol = "BTCUSDT"
			ex := &martingaleEntryReconcileExchange{hedgeExchange: &hedgeExchange{}, order: &exchange.Order{
				OrderID: 83, Symbol: "BTCUSDT", Side: exchange.SideSell, Quantity: 0.5, Status: exchange.OrderStatusNew,
			}}
			store := &memoryRuntimeStateStore{}
			state := signalRuntimeState{
				BotID: cfg.Trading.BotID, StrategyName: strategyName, Symbol: "BTCUSDT",
				Position:   &Position{Symbol: "BTCUSDT", Size: 0.5, EntryPrice: 100, OpeningFee: 0.1, CurrentPrice: 110, PnL: 4.9},
				EntryPrice: 100, PendingAction: signalActionCloseLong,
				ActiveOrder: &Order{OrderID: 83, ClientOrderID: "close-83", Symbol: "BTCUSDT", Side: "SELL", Quantity: 0.5, Price: 110, Status: position.OrderStatusUnknown},
				Statistics:  StrategyStatistics{TotalPnL: 12, TotalVolume: 210, TotalTrades: 1, WinRate: 1},
			}
			payload, err := json.Marshal(state)
			if err != nil {
				t.Fatal(err)
			}
			store.version, store.payload, store.found = signalRuntimeStateSchemaVersion, string(payload), true

			var s Strategy
			switch strategyName {
			case "trend":
				v := NewTrendFollowingStrategy(strategyName, cfg, nil, ex, nil)
				v.SetRuntimeStateStore(store)
				s = v
			case "mean_reversion":
				v := NewMeanReversionStrategy(strategyName, cfg, nil, ex, nil)
				v.SetRuntimeStateStore(store)
				s = v
			case "momentum":
				v := NewMomentumStrategy(strategyName, cfg, nil, ex, nil)
				v.SetRuntimeStateStore(store)
				s = v
			}
			if err := s.Start(context.Background()); err != nil {
				t.Fatal(err)
			}
			positions, orders := s.GetPositions(), s.GetOrders()
			if len(positions) != 1 || positions[0].Size != 0.5 || positions[0].OpeningFee != 0.1 || len(orders) != 1 || orders[0].OrderID != 83 {
				t.Fatalf("restored position/order mismatch: positions=%+v orders=%+v", positions, orders)
			}
			if !signalOrderMatches(orders[0], &position.OrderUpdate{ClientOrderID: "close-83"}) {
				t.Fatal("restored active order lost its matching identity")
			}
		})
	}
}

func TestSignalStrategyStartReplaysMissedActiveOrderFillAndFee(t *testing.T) {
	cfg := &config.Config{}
	cfg.Trading.BotID, cfg.Trading.Symbol = "signal-replay", "BTCUSDT"
	ex := &martingaleEntryReconcileExchange{hedgeExchange: &hedgeExchange{}, order: &exchange.Order{
		OrderID: 84, Symbol: "BTCUSDT", Side: exchange.SideBuy, Quantity: 1, ExecutedQty: 0.4,
		AvgPrice: 100, Status: exchange.OrderStatusPartiallyFilled,
	}, fills: []*exchange.OrderFill{{OrderID: 84, TradeID: "signal-fill-84", Symbol: "BTCUSDT", Side: exchange.SideBuy,
		Price: 100, Quantity: 0.4, Commission: 0.04, CommissionAsset: "USDT"}}}
	state := signalRuntimeState{BotID: cfg.Trading.BotID, StrategyName: "trend", Symbol: "BTCUSDT", PendingAction: signalActionOpenLong,
		ActiveOrder: &Order{OrderID: 84, ClientOrderID: "signal-cid-84", Symbol: "BTCUSDT", Side: "BUY", Price: 100, Quantity: 1, Status: position.OrderStatusUnknown}}
	payload, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	store := &memoryRuntimeStateStore{version: signalRuntimeStateSchemaVersion, payload: string(payload), found: true}
	trend := NewTrendFollowingStrategy("trend", cfg, nil, ex, nil)
	trend.SetRuntimeStateStore(store)
	if err := trend.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	positions, orders := trend.GetPositions(), trend.GetOrders()
	if len(positions) != 1 || math.Abs(positions[0].Size-0.4) > 1e-9 || math.Abs(positions[0].OpeningFee-0.04) > 1e-9 ||
		len(orders) != 1 || math.Abs(orders[0].FillProgress.Quantity-0.4) > 1e-9 || math.Abs(orders[0].FeeVerifiedQty-0.4) > 1e-9 {
		t.Fatalf("missed signal fill or fee was not recovered: positions=%+v orders=%+v", positions, orders)
	}
}

func TestSignalStrategyStartBlocksWhenActiveOrderEvidenceIsMissing(t *testing.T) {
	cfg := &config.Config{}
	cfg.Trading.BotID, cfg.Trading.Symbol = "signal-missing", "BTCUSDT"
	state := signalRuntimeState{BotID: cfg.Trading.BotID, StrategyName: "trend", Symbol: "BTCUSDT", PendingAction: signalActionOpenLong,
		ActiveOrder: &Order{OrderID: 85, ClientOrderID: "signal-cid-85", Symbol: "BTCUSDT", Side: "BUY", Price: 100, Quantity: 1, Status: position.OrderStatusUnknown}}
	payload, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	trend := NewTrendFollowingStrategy("trend", cfg, nil, &hedgeExchange{}, nil)
	trend.SetRuntimeStateStore(&memoryRuntimeStateStore{version: signalRuntimeStateSchemaVersion, payload: string(payload), found: true})
	if err := trend.Start(context.Background()); err == nil {
		t.Fatal("strategy started without exchange evidence for active order")
	}
	if trend.IsRunning() {
		t.Fatal("strategy entered running state without active order reconciliation")
	}
}

func TestSignalRuntimeStateRejectsFilledOrderWithoutFeeCursor(t *testing.T) {
	cfg := &config.Config{}
	cfg.Trading.BotID, cfg.Trading.Symbol = "signal-old-cursor", "BTCUSDT"
	state := signalRuntimeState{BotID: cfg.Trading.BotID, StrategyName: "trend", Symbol: "BTCUSDT", PendingAction: signalActionOpenLong,
		ActiveOrder: &Order{OrderID: 86, ClientOrderID: "signal-cid-86", Symbol: "BTCUSDT", Side: "BUY", Price: 100, Quantity: 1,
			FillProgress: position.FillProgress{Quantity: 0.5, Notional: 50}, Status: position.OrderStatusPartiallyFilled}}
	payload, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := loadSignalRuntimeState(&memoryRuntimeStateStore{version: signalRuntimeStateSchemaVersion, payload: string(payload), found: true},
		cfg, &hedgeExchange{}, "trend", "BTCUSDT"); err == nil {
		t.Fatal("restored partially-filled signal order without fee evidence cursor")
	}
}

func TestSignalRuntimeStateRejectsActiveOrderIdentityOrActionMismatch(t *testing.T) {
	cfg := &config.Config{}
	cfg.Trading.BotID, cfg.Trading.Symbol = "signal-invalid-active-order", "BTCUSDT"
	validPosition := &Position{Symbol: "BTCUSDT", Size: 0.5, EntryPrice: 100, CurrentPrice: 100}
	tests := []struct {
		name  string
		state signalRuntimeState
	}{
		{
			name: "open-long cannot restore a sell order",
			state: signalRuntimeState{BotID: cfg.Trading.BotID, StrategyName: "trend", Symbol: "BTCUSDT",
				PendingAction: signalActionOpenLong,
				ActiveOrder:   &Order{OrderID: 901, ClientOrderID: "open-901", Symbol: "BTCUSDT", Side: "SELL", Price: 100, Quantity: 1}},
		},
		{
			name: "close-long cannot restore a buy order",
			state: signalRuntimeState{BotID: cfg.Trading.BotID, StrategyName: "trend", Symbol: "BTCUSDT",
				Position: validPosition, EntryPrice: 100, PendingAction: signalActionCloseLong,
				ActiveOrder: &Order{OrderID: 902, ClientOrderID: "close-902", Symbol: "BTCUSDT", Side: "BUY", Price: 100, Quantity: 0.5}},
		},
		{
			name: "active order requires exchange order id",
			state: signalRuntimeState{BotID: cfg.Trading.BotID, StrategyName: "trend", Symbol: "BTCUSDT",
				PendingAction: signalActionOpenLong,
				ActiveOrder:   &Order{ClientOrderID: "open-no-id", Symbol: "BTCUSDT", Side: "BUY", Price: 100, Quantity: 1}},
		},
		{
			name: "active order requires client order id",
			state: signalRuntimeState{BotID: cfg.Trading.BotID, StrategyName: "trend", Symbol: "BTCUSDT",
				PendingAction: signalActionOpenLong,
				ActiveOrder:   &Order{OrderID: 904, Symbol: "BTCUSDT", Side: "BUY", Price: 100, Quantity: 1}},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			payload, err := json.Marshal(tc.state)
			if err != nil {
				t.Fatal(err)
			}
			store := &memoryRuntimeStateStore{version: signalRuntimeStateSchemaVersion, payload: string(payload), found: true}
			if _, _, err := loadSignalRuntimeState(store, cfg, &hedgeExchange{}, "trend", "BTCUSDT"); err == nil {
				t.Fatal("accepted persisted active order with inconsistent action or incomplete identity")
			}
		})
	}
}
