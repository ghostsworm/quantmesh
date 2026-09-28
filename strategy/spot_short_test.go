package strategy

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"sync"
	"testing"

	"quantmesh/config"
	"quantmesh/event"
	"quantmesh/exchange"
	"quantmesh/position"
)

// mockMarginExchange 記錄借還調用
type mockMarginExchange struct {
	mockFCExchange
	marginMu     sync.Mutex
	borrowed     []float64
	repaid       []float64
	borrowErr    error
	repayErr     error
	beforeBorrow func()
}

type failNthRuntimeStateSave struct {
	saves   int
	failAt  int
	version int
	payload string
	found   bool
}

func (s *failNthRuntimeStateSave) LoadRuntimeState(string) (int, string, bool, error) {
	return s.version, s.payload, s.found, nil
}

func (s *failNthRuntimeStateSave) SaveRuntimeState(_ string, version int, payload string) error {
	s.saves++
	if s.saves == s.failAt {
		return errors.New("injected runtime state write failure")
	}
	s.version, s.payload, s.found = version, payload, true
	return nil
}

func (m *mockMarginExchange) Borrow(ctx context.Context, asset string, amount float64) (int64, error) {
	if m.beforeBorrow != nil {
		m.beforeBorrow()
	}
	m.marginMu.Lock()
	defer m.marginMu.Unlock()
	m.borrowed = append(m.borrowed, amount)
	return 1, m.borrowErr
}

func TestSpotShortPersistsBorrowIntentBeforeExchangeSideEffect(t *testing.T) {
	store := &memoryRuntimeStateStore{}
	margin := &mockMarginExchange{}
	margin.beforeBorrow = func() {
		var state spotShortRuntimeState
		if !store.found {
			t.Fatal("borrow reached exchange before its intent was persisted")
		}
		if err := json.Unmarshal([]byte(store.payload), &state); err != nil {
			t.Fatalf("decode persisted pre-borrow state: %v", err)
		}
		if len(state.PendingBorrow) != 1 {
			t.Fatalf("pre-borrow state must contain exactly one intent: %+v", state.PendingBorrow)
		}
		for _, intent := range state.PendingBorrow {
			if intent.Phase != "prepared" || intent.Amount <= 0 {
				t.Fatalf("pre-borrow intent is not durable/prepared: %+v", intent)
			}
		}
	}
	s := newSpotShortForTest(&signalTestExecutor{}, &signalTestExchange{}, margin)
	s.SetRuntimeStateStore(store)
	if err := s.increaseShort(context.Background(), 0.5); err != nil {
		t.Fatalf("increase short: %v", err)
	}
	if len(margin.borrowed) != 1 {
		t.Fatalf("borrow count=%d want 1", len(margin.borrowed))
	}
}

func TestSpotShortDoesNotBorrowWhenIntentPersistenceFails(t *testing.T) {
	store := &memoryRuntimeStateStore{err: errors.New("storage unavailable")}
	margin := &mockMarginExchange{}
	s := newSpotShortForTest(&signalTestExecutor{}, &signalTestExchange{}, margin)
	s.SetRuntimeStateStore(store)
	if err := s.increaseShort(context.Background(), 0.5); err == nil || !strings.Contains(err.Error(), "persist borrow intent") {
		t.Fatalf("expected pre-borrow persistence error, got %v", err)
	}
	if len(margin.borrowed) != 0 {
		t.Fatalf("must not call exchange without durable borrow intent: %v", margin.borrowed)
	}
}

func TestSpotShortBorrowedStateWriteFailureKeepsPreparedIntentAndBlocksSell(t *testing.T) {
	store := &failNthRuntimeStateSave{failAt: 2}
	margin := &mockMarginExchange{}
	executor := &signalTestExecutor{}
	s := newSpotShortForTest(executor, &signalTestExchange{}, margin)
	s.SetRuntimeStateStore(store)
	var reported error
	s.SetUnresolvedDebtHandler(func(err error) { reported = err })

	err := s.increaseShort(context.Background(), 0.5)
	if err == nil || !strings.Contains(err.Error(), "transfer id could not be durably recorded") {
		t.Fatalf("expected durable borrow-state failure, got %v", err)
	}
	if len(margin.borrowed) != 1 || len(executor.orders) != 0 {
		t.Fatalf("borrow should have happened once but sell must remain blocked: borrow=%v orders=%+v", margin.borrowed, executor.orders)
	}
	if reported == nil {
		t.Fatal("unresolved debt was not reported to runtime risk gate")
	}
	var persisted spotShortRuntimeState
	if !store.found || json.Unmarshal([]byte(store.payload), &persisted) != nil || len(persisted.PendingBorrow) != 1 {
		t.Fatalf("last durable pre-borrow intent was not retained: found=%v payload=%s", store.found, store.payload)
	}
	for _, intent := range persisted.PendingBorrow {
		if intent.Phase != "prepared" {
			t.Fatalf("failed second write must leave durable prepared intent, got %+v", intent)
		}
	}

	restarted := newSpotShortForTest(&signalTestExecutor{}, &signalTestExchange{}, &mockMarginExchange{})
	restarted.SetRuntimeStateStore(store)
	if err := restarted.Start(context.Background()); err == nil || !strings.Contains(err.Error(), "unresolved borrow intent") {
		t.Fatalf("restart must block while borrow transfer is unverified, got %v", err)
	}
}

func (m *mockMarginExchange) Repay(ctx context.Context, asset string, amount float64) (int64, error) {
	m.marginMu.Lock()
	defer m.marginMu.Unlock()
	m.repaid = append(m.repaid, amount)
	return 1, m.repayErr
}

type failingPriceExchange struct {
	signalTestExchange
	err error
}

func (e *failingPriceExchange) GetLatestPrice(ctx context.Context, symbol string) (float64, error) {
	return 0, e.err
}

type failingOrderExecutor struct {
	signalTestExecutor
	err error
}

type spotShortPositionExchange struct {
	signalTestExchange
	positions interface{}
	err       error
}

func (e *spotShortPositionExchange) GetPositions(context.Context, string) (interface{}, error) {
	return e.positions, e.err
}

func (e *failingOrderExecutor) PlaceOrder(req *position.OrderRequest) (*position.Order, error) {
	return nil, e.err
}

func newSpotShortForTest(executor position.OrderExecutorInterface, ex position.IExchange, margin *mockMarginExchange) *SpotShortStrategy {
	cfg := &config.Config{}
	cfg.Trading.Symbol = "BTCUSDT"
	s := NewSpotShortStrategy("spot_short", cfg, executor, ex, margin, map[string]interface{}{})
	s.SetRuntimeStateStore(&memoryRuntimeStateStore{})
	return s
}

func TestSpotShortIncreaseShortDoesNotBorrowWithoutPriceEvidence(t *testing.T) {
	errPrice := errors.New("price feed down")
	margin := &mockMarginExchange{}
	s := newSpotShortForTest(&signalTestExecutor{}, &failingPriceExchange{err: errPrice}, margin)
	err := s.increaseShort(context.Background(), 0.5)
	if !errors.Is(err, errPrice) {
		t.Fatalf("err=%v, want wrapped %v", err, errPrice)
	}
	if len(margin.borrowed) != 0 || len(margin.repaid) != 0 {
		t.Fatalf("price failure must happen before borrowing: borrowed=%v repaid=%v", margin.borrowed, margin.repaid)
	}
}

func TestSpotShortSellFailureRetainsPersistentBorrowIntent(t *testing.T) {
	errSell := errors.New("sell rejected")
	margin := &mockMarginExchange{}
	s := newSpotShortForTest(&failingOrderExecutor{err: errSell}, &signalTestExchange{}, margin)
	store := &memoryRuntimeStateStore{}
	s.SetRuntimeStateStore(store)
	var reported error
	s.SetUnresolvedDebtHandler(func(err error) { reported = err })

	err := s.increaseShort(context.Background(), 0.5)
	if !errors.Is(err, errSell) {
		t.Fatalf("err=%v, want sell error", err)
	}
	if reported == nil || !strings.Contains(reported.Error(), "未核") {
		t.Fatalf("unresolved debt was not synchronously reported to the opening risk gate: %v", reported)
	}
	if len(margin.borrowed) != 1 || len(margin.repaid) != 0 || len(s.pendingBorrow) != 1 || !store.found {
		t.Fatalf("ambiguous order result must keep debt intent without repaying: borrowed=%v repaid=%v pending=%v", margin.borrowed, margin.repaid, s.pendingBorrow)
	}
	restarted := newSpotShortForTest(&signalTestExecutor{}, &signalTestExchange{}, &mockMarginExchange{})
	restarted.SetRuntimeStateStore(store)
	if err := restarted.Start(context.Background()); err == nil || !strings.Contains(err.Error(), "unresolved borrow intent") {
		t.Fatalf("restart must be blocked until borrow/order is reconciled, got %v", err)
	}
}

func TestSpotShortAmbiguousBorrowFailureReportsUnresolvedDebt(t *testing.T) {
	borrowErr := errors.New("borrow response timed out")
	margin := &mockMarginExchange{borrowErr: borrowErr}
	s := newSpotShortForTest(&signalTestExecutor{}, &signalTestExchange{}, margin)
	var reported error
	s.SetUnresolvedDebtHandler(func(err error) { reported = err })

	err := s.increaseShort(context.Background(), 0.5)
	if !errors.Is(err, borrowErr) {
		t.Fatalf("borrow error was not propagated: %v", err)
	}
	if reported == nil || !strings.Contains(reported.Error(), "未核") {
		t.Fatalf("ambiguous borrow result was not sent to the risk gate: %v", reported)
	}
	if len(s.pendingBorrow) != 1 {
		t.Fatal("ambiguous borrow result must preserve its durable intent")
	}
}

func TestSpotShortUnresolvedBorrowIntentBlocksSubsequentBorrow(t *testing.T) {
	margin := &mockMarginExchange{borrowErr: errors.New("must not be reached")}
	executor := &signalTestExecutor{}
	s := newSpotShortForTest(executor, &signalTestExchange{}, margin)
	s.pendingBorrow["ambiguous-client-order"] = spotShortPendingBorrow{
		Amount: 0.5, Phase: "prepared", CreatedAtUnixMilli: 1,
	}
	var reported error
	s.SetUnresolvedDebtHandler(func(err error) { reported = err })

	err := s.increaseShort(context.Background(), 0.25)
	if err == nil || !strings.Contains(err.Error(), "unresolved borrow intent") {
		t.Fatalf("increaseShort error=%v, want unresolved-intent rejection", err)
	}
	if reported == nil || !strings.Contains(reported.Error(), "ambiguous-client-order") {
		t.Fatalf("unresolved debt was not reported to the opening risk gate: %v", reported)
	}
	if len(margin.borrowed) != 0 || len(executor.orders) != 0 {
		t.Fatalf("unresolved intent must block before borrowing: borrow=%v orders=%+v", margin.borrowed, executor.orders)
	}
}

func TestSpotShortLegacyRuntimeStateMigratesWithoutDroppingDebtOrders(t *testing.T) {
	state := spotShortRuntimeState{BotID: "", Strategy: "spot_short", Symbol: "BTCUSDT", BaseAsset: "BTC", PendingRepay: map[int64]spotShortPendingRepay{71: {OrderQuantity: 0.8, ExecutedQty: 0.2}}}
	payload, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	store := &memoryRuntimeStateStore{version: 3, payload: string(payload), found: true}
	s := newSpotShortForTest(&signalTestExecutor{}, &signalTestExchange{}, &mockMarginExchange{})
	s.SetRuntimeStateStore(store)
	s.mu.Lock()
	err = s.restoreRuntimeStateLocked()
	s.mu.Unlock()
	if err != nil {
		t.Fatalf("legacy runtime state migration: %v", err)
	}
	if store.version != spotShortRuntimeStateSchemaVersion || s.pendingRepay[71].ExecutedQty != 0.2 {
		t.Fatalf("legacy state not migrated losslessly: version=%d pending=%+v", store.version, s.pendingRepay)
	}
}

func TestSpotShortPositionEvidenceFailureNeverBorrowsOrTrades(t *testing.T) {
	queryErr := errors.New("position endpoint unavailable")
	cases := []struct {
		name      string
		positions interface{}
		err       error
	}{
		{name: "query error", positions: []*position.PositionInfo{}, err: queryErr},
		{name: "nil response", positions: nil},
		{name: "unsupported response", positions: map[string]float64{"BTCUSDT": 0}},
		{name: "nil entry", positions: []*position.PositionInfo{nil}},
		{name: "non finite size", positions: []*position.PositionInfo{{Symbol: "BTCUSDT", Size: math.NaN()}}},
		{name: "contradictory long position", positions: []*position.PositionInfo{{Symbol: "BTCUSDT", Size: 0.25}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			margin := &mockMarginExchange{}
			executor := &signalTestExecutor{}
			s := newSpotShortForTest(executor, &spotShortPositionExchange{positions: tc.positions, err: tc.err}, margin)
			s.onHedgeSignal(&event.Event{Data: map[string]interface{}{"symbol": "BTCUSDT", "target_spot_short": 0.5}})
			if len(margin.borrowed) != 0 || len(executor.orders) != 0 {
				t.Fatalf("incomplete position evidence must not trigger borrow/order: borrowed=%v orders=%+v", margin.borrowed, executor.orders)
			}
		})
	}
}

func TestSpotShortIncreaseShortSuccessDoesNotRepay(t *testing.T) {
	margin := &mockMarginExchange{}
	executor := &signalTestExecutor{}
	s := newSpotShortForTest(executor, &signalTestExchange{}, margin)

	if err := s.increaseShort(context.Background(), 0.5); err != nil {
		t.Fatalf("increaseShort: %v", err)
	}
	if len(margin.repaid) != 0 || len(executor.orders) != 1 || executor.orders[0].Side != "SELL" {
		t.Fatalf("repaid=%v orders=%d", margin.repaid, len(executor.orders))
	}
	if len(s.pendingBorrow) != 0 {
		t.Fatalf("verified accepted sell should clear the borrow intent after executor durability: %+v", s.pendingBorrow)
	}
}

func TestSpotShortFilledBuyReturnsRepayFailureAndRetainsPendingDebt(t *testing.T) {
	repayErr := errors.New("repay endpoint unavailable")
	margin := &mockMarginExchange{repayErr: repayErr}
	s := newSpotShortForTest(&signalTestExecutor{}, &signalTestExchange{}, margin)
	s.SetRuntimeStateStore(&memoryRuntimeStateStore{})
	s.pendingRepay[42] = spotShortPendingRepay{OrderQuantity: 0.5}

	err := s.OnOrderUpdate(&position.OrderUpdate{OrderID: 42, Symbol: "BTCUSDT", Side: "BUY", Status: "FILLED", ExecutedQty: 0.5})
	if !errors.Is(err, repayErr) {
		t.Fatalf("expected repay failure to propagate to Bot risk gate, got %v", err)
	}
	if got := s.pendingRepay[42]; !got.RepayUncertain || got.ExecutedQty != 0 {
		t.Fatalf("pending repay amount=%v, want debt retained for reconciliation", got)
	}
}

func TestSpotShortFilledBuyWithoutRepayExecutorFailsClosed(t *testing.T) {
	s := newSpotShortForTest(&signalTestExecutor{}, &signalTestExchange{}, nil)
	s.SetRuntimeStateStore(&memoryRuntimeStateStore{})
	s.pendingRepay[43] = spotShortPendingRepay{OrderQuantity: 0.25}
	if s.smEx != nil {
		t.Fatal("typed-nil raw exchange must not create a repay executor")
	}
	err := s.OnOrderUpdate(&position.OrderUpdate{OrderID: 43, Symbol: "BTCUSDT", Side: "BUY", Status: "FILLED", ExecutedQty: 0.25})
	if err == nil {
		t.Fatal("missing repay executor must not be reported as successful repayment")
	}
	if got := s.pendingRepay[43]; got.ExecutedQty != 0 {
		t.Fatalf("pending repay amount=%v, want debt retained", got)
	}
}

func TestSpotShortPartialFillRepaysOnlyNewFillAndWaitsForTerminalUpdate(t *testing.T) {
	margin := &mockMarginExchange{}
	s := newSpotShortForTest(&signalTestExecutor{}, &signalTestExchange{}, margin)
	store := &memoryRuntimeStateStore{}
	s.SetRuntimeStateStore(store)
	s.pendingRepay[51] = spotShortPendingRepay{OrderQuantity: 1}

	partial := &position.OrderUpdate{OrderID: 51, Side: "BUY", Status: "PARTIALLY_FILLED", ExecutedQty: 0.4}
	if err := s.OnOrderUpdate(partial); err != nil {
		t.Fatal(err)
	}
	if err := s.OnOrderUpdate(partial); err != nil {
		t.Fatal(err)
	}
	if len(margin.repaid) != 1 || margin.repaid[0] != 0.4 {
		t.Fatalf("duplicate cumulative update repaid twice: %v", margin.repaid)
	}
	if _, ok := s.pendingRepay[51]; !ok {
		t.Fatal("partially filled buy should remain tracked until terminal status")
	}
	if err := s.OnOrderUpdate(&position.OrderUpdate{OrderID: 51, Side: "BUY", Status: "CANCELED", ExecutedQty: 0.4}); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.pendingRepay[51]; ok {
		t.Fatal("terminal canceled order was not removed after persisting its final fill")
	}
}

func TestSpotShortUncertainRepaymentBlocksRuntimeRestore(t *testing.T) {
	cfg := &config.Config{}
	cfg.Trading.Symbol = "BTCUSDT"
	state := spotShortRuntimeState{
		Strategy: "spot_short", Symbol: "BTCUSDT", BaseAsset: "BTC",
		PendingRepay: map[int64]spotShortPendingRepay{55: {OrderQuantity: 1, RepayUncertain: true}},
	}
	payload, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	store := &memoryRuntimeStateStore{version: spotShortRuntimeStateSchemaVersion, payload: string(payload), found: true}
	s := NewSpotShortStrategy("spot_short", cfg, &signalTestExecutor{}, &signalTestExchange{}, &mockMarginExchange{}, nil)
	s.SetRuntimeStateStore(store)
	if err := s.Start(context.Background()); err == nil {
		t.Fatal("startup should fail closed while repayment outcome requires reconciliation")
	}
}

type spotShortReconcileExchange struct {
	signalTestExchange
	order    *exchange.Order
	fills    []*exchange.OrderFill
	fillsErr error
}

func (e *spotShortReconcileExchange) GetOrder(context.Context, string, int64) (interface{}, error) {
	return e.order, nil
}

func (e *spotShortReconcileExchange) GetOrderFills(context.Context, string, int64) (interface{}, error) {
	if e.fillsErr != nil {
		return nil, e.fillsErr
	}
	return e.fills, nil
}

func spotShortRestoreFixture(t *testing.T, venue position.IExchange, margin *mockMarginExchange) (*SpotShortStrategy, *memoryRuntimeStateStore) {
	t.Helper()
	cfg := &config.Config{}
	cfg.Trading.BotID = "spot-short-reconcile"
	cfg.Trading.Symbol = "BTCUSDT"
	state := spotShortRuntimeState{
		BotID: cfg.Trading.BotID, Strategy: "spot_short", Symbol: "BTCUSDT", BaseAsset: "BTC",
		PendingRepay: map[int64]spotShortPendingRepay{71: {OrderQuantity: 1}},
	}
	payload, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	store := &memoryRuntimeStateStore{version: spotShortRuntimeStateSchemaVersion, payload: string(payload), found: true}
	s := NewSpotShortStrategy("spot_short", cfg, &signalTestExecutor{}, venue, nil, nil)
	s.smEx = margin
	s.SetRuntimeStateStore(store)
	return s, store
}

func TestSpotShortStartupReconcilesOrderAndBaseFeeFillsBeforeRepay(t *testing.T) {
	venue := &spotShortReconcileExchange{
		order: &exchange.Order{OrderID: 71, Symbol: "BTCUSDT", Side: exchange.SideBuy, Status: exchange.OrderStatusPartiallyFilled, Quantity: 1, ExecutedQty: 0.5},
		fills: []*exchange.OrderFill{{OrderID: 71, TradeID: "trade-1", Symbol: "BTCUSDT", Side: exchange.SideBuy, Quantity: 0.5, Commission: 0.001, CommissionAsset: "BTC", BaseFeeQty: 0.001}},
	}
	margin := &mockMarginExchange{}
	s, _ := spotShortRestoreFixture(t, venue, margin)
	if err := s.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(margin.repaid) != 1 || math.Abs(margin.repaid[0]-0.499) > 1e-12 {
		t.Fatalf("repay should use net base received after fee: %v", margin.repaid)
	}
	if got := s.pendingRepay[71]; got.ExecutedQty != 0.5 || got.BaseFeeQty != 0.001 {
		t.Fatalf("reconciled order cursor was not stored: %+v", got)
	}
}

func TestSpotShortStartupFailsClosedWithoutCompleteFillEvidence(t *testing.T) {
	venue := &spotShortReconcileExchange{
		order: &exchange.Order{OrderID: 71, Symbol: "BTCUSDT", Side: exchange.SideBuy, Status: exchange.OrderStatusFilled, Quantity: 1, ExecutedQty: 1},
		fills: nil, // Some exchange adapters currently return nil,nil for unsupported fill history.
	}
	margin := &mockMarginExchange{}
	s, _ := spotShortRestoreFixture(t, venue, margin)
	if err := s.Start(context.Background()); err == nil {
		t.Fatal("startup must refuse to guess repayment amount without fill/fee history")
	}
	if len(margin.repaid) != 0 {
		t.Fatalf("must not repay based on order cumulative quantity without fee evidence: %v", margin.repaid)
	}
}
