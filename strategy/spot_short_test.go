package strategy

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

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
	repayHistory []exchange.MarginBorrowRecord
	historyErr   error
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
	if err := restarted.Start(context.Background()); err == nil || !strings.Contains(err.Error(), "margin exchange cannot query exact client order IDs") {
		t.Fatalf("restart must block while borrow transfer/order is unverified, got %v", err)
	}
}

func (m *mockMarginExchange) Repay(ctx context.Context, asset string, amount float64) (int64, error) {
	m.marginMu.Lock()
	defer m.marginMu.Unlock()
	m.repaid = append(m.repaid, amount)
	return 1, m.repayErr
}

func (m *mockMarginExchange) GetMarginTransactionHistory(_ context.Context, asset, transactionType string, startTime, endTime int64, page, pageSize int) ([]exchange.MarginBorrowRecord, int64, error) {
	m.marginMu.Lock()
	defer m.marginMu.Unlock()
	if !strings.EqualFold(transactionType, "REPAY") || asset != "BTC" || page != 1 || pageSize != 100 || startTime <= 0 || endTime < startTime {
		return nil, 0, errors.New("unexpected margin transaction history query")
	}
	return append([]exchange.MarginBorrowRecord(nil), m.repayHistory...), int64(len(m.repayHistory)), m.historyErr
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

type spotShortOrderCallbackExecutor struct {
	signalTestExecutor
	onPlace func(*position.OrderRequest, *position.Order)
}

func (e *spotShortOrderCallbackExecutor) PlaceOrder(req *position.OrderRequest) (*position.Order, error) {
	order, err := e.signalTestExecutor.PlaceOrder(req)
	if err == nil && e.onPlace != nil {
		e.onPlace(req, order)
	}
	return order, err
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

func TestSpotShortPersistsBuyIntentBeforeSubmissionAndHandlesFillBeforeAck(t *testing.T) {
	store := &memoryRuntimeStateStore{}
	margin := &mockMarginExchange{}
	var strategy *SpotShortStrategy
	venue := &spotShortReconcileExchange{}
	executor := &spotShortOrderCallbackExecutor{}
	executor.onPlace = func(req *position.OrderRequest, order *position.Order) {
		var state spotShortRuntimeState
		if err := json.Unmarshal([]byte(store.payload), &state); err != nil {
			t.Fatalf("decode durable buy intent before submission: %v", err)
		}
		if req.ClientOrderID == "" || state.PendingBuy[req.ClientOrderID].Quantity != req.Quantity {
			t.Fatalf("buy intent was not persisted before exchange submit: req=%+v state=%+v", req, state.PendingBuy)
		}
		venue.fills = []*exchange.OrderFill{{OrderID: order.OrderID, TradeID: "early-buy-fill", Symbol: req.Symbol,
			Side: exchange.SideBuy, Price: req.Price, Quantity: req.Quantity, CommissionQuoteKnown: true}}
		if err := strategy.OnOrderUpdate(&position.OrderUpdate{
			OrderID: order.OrderID, ClientOrderID: req.ClientOrderID, Symbol: req.Symbol,
			Side: "BUY", Status: "FILLED", ExecutedQty: req.Quantity,
		}); err != nil {
			t.Fatalf("process fill arriving before submit acknowledgement: %v", err)
		}
	}
	strategy = newSpotShortForTest(executor, venue, margin)
	strategy.SetRuntimeStateStore(store)
	if err := strategy.decreaseShort(context.Background(), 0.25); err != nil {
		t.Fatalf("decrease short: %v", err)
	}
	if len(margin.repaid) != 1 || math.Abs(margin.repaid[0]-0.25) > 1e-12 {
		t.Fatalf("early fill should repay exactly once: %v", margin.repaid)
	}
	var finalState spotShortRuntimeState
	if err := json.Unmarshal([]byte(store.payload), &finalState); err != nil {
		t.Fatal(err)
	}
	if len(finalState.PendingBuy) != 0 || len(finalState.PendingRepay) != 0 {
		t.Fatalf("terminal early fill should leave no unresolved order marker: buy=%v repay=%v", finalState.PendingBuy, finalState.PendingRepay)
	}
}

func TestSpotShortFilledWithoutExecutionRetainsRepayIntent(t *testing.T) {
	store := &memoryRuntimeStateStore{}
	margin := &mockMarginExchange{}
	strategy := newSpotShortForTest(&signalTestExecutor{}, &signalTestExchange{}, margin)
	strategy.SetRuntimeStateStore(store)
	clientOrderID := "spot-short-zero-fill-cid"
	strategy.mu.Lock()
	strategy.pendingBuy[clientOrderID] = spotShortPendingBuy{Quantity: 0.25, CreatedAtUnixMilli: time.Now().UnixMilli()}
	if err := strategy.persistRuntimeStateLocked(); err != nil {
		strategy.mu.Unlock()
		t.Fatal(err)
	}
	strategy.mu.Unlock()

	if err := strategy.OnOrderUpdate(&position.OrderUpdate{OrderID: 108, ClientOrderID: clientOrderID,
		Symbol: "BTCUSDT", Side: "BUY", Status: "FILLED", ExecutedQty: 0}); err == nil {
		t.Fatal("FILLED update without execution must be rejected")
	}
	if len(strategy.pendingBuy) != 0 || strategy.pendingRepay[108].ClientOrderID != clientOrderID ||
		len(margin.repaid) != 0 {
		t.Fatalf("zero-fill terminal update must retain repayment reconciliation without repaying: buy=%v repay=%v repaid=%v",
			strategy.pendingBuy, strategy.pendingRepay, margin.repaid)
	}
	var persisted spotShortRuntimeState
	if !store.found || json.Unmarshal([]byte(store.payload), &persisted) != nil || persisted.PendingRepay[108].ClientOrderID != clientOrderID {
		t.Fatalf("zero-fill terminal update lost durable repayment intent: found=%v state=%+v", store.found, persisted)
	}
}

func TestSpotShortUncertainBuySubmissionRetainsDurableClientOrderIntent(t *testing.T) {
	store := &memoryRuntimeStateStore{}
	errSubmit := errors.New("submission response lost")
	strategy := newSpotShortForTest(&failingOrderExecutor{err: errSubmit}, &signalTestExchange{}, &mockMarginExchange{})
	strategy.SetRuntimeStateStore(store)
	var reported error
	strategy.SetUnresolvedDebtHandler(func(err error) { reported = err })
	err := strategy.decreaseShort(context.Background(), 0.25)
	if !errors.Is(err, errSubmit) || reported == nil {
		t.Fatalf("ambiguous buy submit must be reported as unresolved: err=%v reported=%v", err, reported)
	}
	var state spotShortRuntimeState
	if !store.found || json.Unmarshal([]byte(store.payload), &state) != nil || len(state.PendingBuy) != 1 {
		t.Fatalf("ambiguous buy submit lost durable client order intent: found=%v state=%+v", store.found, state.PendingBuy)
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
	if err := restarted.Start(context.Background()); err == nil || !strings.Contains(err.Error(), "margin exchange cannot query exact client order IDs") {
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
	if len(s.pendingBorrow) != 1 {
		t.Fatalf("accepted sell must retain borrow intent until the exchange confirms a terminal order: %+v", s.pendingBorrow)
	}
}

func TestSpotShortFilledBuyReturnsRepayFailureAndRetainsPendingDebt(t *testing.T) {
	repayErr := errors.New("repay endpoint unavailable")
	margin := &mockMarginExchange{repayErr: repayErr}
	venue := &spotShortReconcileExchange{fills: []*exchange.OrderFill{{OrderID: 42, TradeID: "buy-42", Symbol: "BTCUSDT",
		Side: exchange.SideBuy, Price: 100, Quantity: 0.5, CommissionQuoteKnown: true}}}
	s := newSpotShortForTest(&signalTestExecutor{}, venue, margin)
	store := &memoryRuntimeStateStore{}
	s.SetRuntimeStateStore(store)
	s.pendingRepay[42] = spotShortPendingRepay{OrderQuantity: 0.5}

	err := s.OnOrderUpdate(&position.OrderUpdate{OrderID: 42, Symbol: "BTCUSDT", Side: "BUY", Status: "FILLED", ExecutedQty: 0.5})
	if !errors.Is(err, repayErr) {
		t.Fatalf("expected repay failure to propagate to Bot risk gate, got %v", err)
	}
	if got := s.pendingRepay[42]; !got.RepayUncertain || got.ExecutedQty != 0 || got.RepayAmount != 0.5 ||
		got.RepayStartedAtUnixMilli <= 0 || got.RepayExpectedExecutedQty != 0.5 {
		t.Fatalf("pending repay amount=%v, want debt retained for reconciliation", got)
	}
	var persisted spotShortRuntimeState
	if !store.found || json.Unmarshal([]byte(store.payload), &persisted) != nil || !persisted.PendingRepay[42].RepayUncertain ||
		persisted.PendingRepay[42].RepayAmount != 0.5 || persisted.PendingRepay[42].RepayExpectedExecutedQty != 0.5 {
		t.Fatalf("uncertain repayment evidence was not durable: found=%v state=%+v", store.found, persisted.PendingRepay[42])
	}
}

func TestSpotShortRecoversUncertainRepaymentFromUniqueConfirmedHistory(t *testing.T) {
	startedAt := time.Now().UTC().Add(-time.Second).UnixMilli()
	margin := &mockMarginExchange{repayHistory: []exchange.MarginBorrowRecord{{TransferID: 812, Asset: "BTC", Amount: 0.499,
		Status: "CONFIRMED", Timestamp: startedAt + 1}}}
	s := newSpotShortForTest(&signalTestExecutor{}, &signalTestExchange{}, margin)
	s.SetRuntimeStateStore(&memoryRuntimeStateStore{})
	s.pendingRepay[44] = spotShortPendingRepay{OrderQuantity: 0.5, RepayUncertain: true, RepayAmount: 0.499,
		RepayStartedAtUnixMilli: startedAt, RepayExpectedExecutedQty: 0.5, RepayExpectedBaseFeeQty: 0.001}
	update := &position.OrderUpdate{OrderID: 44, Symbol: "BTCUSDT", Side: "BUY", Status: "PARTIALLY_FILLED", ExecutedQty: 0.5,
		CommissionKnown: true, CommissionAsset: "BTC", BaseFeeQty: 0.001}
	if err := s.OnOrderUpdate(update); err != nil {
		t.Fatalf("reconcile confirmed repayment: %v", err)
	}
	got := s.pendingRepay[44]
	if got.RepayUncertain || got.ExecutedQty != 0.5 || got.BaseFeeQty != 0.001 || got.RepayTransferID != 0 || len(margin.repaid) != 0 {
		t.Fatalf("unexpected recovered repayment state or duplicate repay: pending=%+v repaid=%v", got, margin.repaid)
	}
}

func TestSpotShortKeepsUncertainRepaymentWhenHistoryIsAmbiguousOrFailed(t *testing.T) {
	startedAt := time.Now().UTC().Add(-time.Second).UnixMilli()
	confirmed := exchange.MarginBorrowRecord{TransferID: 813, Asset: "BTC", Amount: 0.499, Status: "CONFIRMED", Timestamp: startedAt + 1}
	tests := []struct {
		name    string
		records []exchange.MarginBorrowRecord
	}{
		{name: "duplicate candidates", records: []exchange.MarginBorrowRecord{confirmed,
			{TransferID: 814, Asset: "BTC", Amount: 0.499, Status: "CONFIRMED", Timestamp: startedAt + 2}}},
		{name: "failed transaction", records: []exchange.MarginBorrowRecord{{TransferID: 815, Asset: "BTC", Amount: 0.499,
			Status: "FAILED", Timestamp: startedAt + 1}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			margin := &mockMarginExchange{repayHistory: test.records}
			s := newSpotShortForTest(&signalTestExecutor{}, &signalTestExchange{}, margin)
			s.SetRuntimeStateStore(&memoryRuntimeStateStore{})
			pending := spotShortPendingRepay{OrderQuantity: 0.5, RepayUncertain: true, RepayAmount: 0.499,
				RepayStartedAtUnixMilli: startedAt, RepayExpectedExecutedQty: 0.5, RepayExpectedBaseFeeQty: 0.001}
			s.pendingRepay[44] = pending
			update := &position.OrderUpdate{OrderID: 44, Symbol: "BTCUSDT", Side: "BUY", Status: "PARTIALLY_FILLED", ExecutedQty: 0.5,
				CommissionKnown: true, CommissionAsset: "BTC", BaseFeeQty: 0.001}
			if err := s.OnOrderUpdate(update); err == nil {
				t.Fatal("ambiguous or failed repayment evidence must remain unresolved")
			}
			if got := s.pendingRepay[44]; got != pending || len(margin.repaid) != 0 {
				t.Fatalf("uncertain repayment intent changed or was repeated: pending=%+v repaid=%v", got, margin.repaid)
			}
		})
	}
}

func TestSpotShortFilledBuyWithoutRepayExecutorFailsClosed(t *testing.T) {
	venue := &spotShortReconcileExchange{fills: []*exchange.OrderFill{{OrderID: 43, TradeID: "buy-43", Symbol: "BTCUSDT",
		Side: exchange.SideBuy, Price: 100, Quantity: 0.25, CommissionQuoteKnown: true}}}
	s := newSpotShortForTest(&signalTestExecutor{}, venue, nil)
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
	venue := &spotShortReconcileExchange{fills: []*exchange.OrderFill{{OrderID: 51, TradeID: "partial-51", Symbol: "BTCUSDT",
		Side: exchange.SideBuy, Price: 100, Quantity: 0.4, CommissionQuoteKnown: true}}}
	s := newSpotShortForTest(&signalTestExecutor{}, venue, margin)
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

func TestSpotShortLiveFillRepaysOnlyVerifiedNetBaseAndChecksFeeCursor(t *testing.T) {
	margin := &mockMarginExchange{}
	venue := &spotShortReconcileExchange{fills: []*exchange.OrderFill{{
		OrderID: 71, TradeID: "fill-1", Symbol: "BTCUSDT", Side: exchange.SideBuy,
		Price: 100, Quantity: 0.4, Commission: 0.01, CommissionAsset: "BTC", BaseFeeQty: 0.01,
	}}}
	s := newSpotShortForTest(&signalTestExecutor{}, venue, margin)
	store := &memoryRuntimeStateStore{}
	s.SetRuntimeStateStore(store)
	s.pendingRepay[71] = spotShortPendingRepay{OrderQuantity: 1}

	first := &position.OrderUpdate{OrderID: 71, Symbol: "BTCUSDT", Side: "BUY", Status: "PARTIALLY_FILLED", ExecutedQty: 0.4, AvgPrice: 100}
	if err := s.OnOrderUpdate(first); err != nil {
		t.Fatal(err)
	}
	if len(margin.repaid) != 1 || math.Abs(margin.repaid[0]-0.39) > 1e-12 {
		t.Fatalf("first repayment should use verified net base: %v", margin.repaid)
	}

	venue.fills = append(venue.fills, &exchange.OrderFill{OrderID: 71, TradeID: "fill-2", Symbol: "BTCUSDT",
		Side: exchange.SideBuy, Price: 101, Quantity: 0.3, Commission: 0.015, CommissionAsset: "BTC", BaseFeeQty: 0.015})
	second := &position.OrderUpdate{OrderID: 71, Symbol: "BTCUSDT", Side: "BUY", Status: "PARTIALLY_FILLED", ExecutedQty: 0.7, AvgPrice: (0.4*100 + 0.3*101) / 0.7}
	if err := s.OnOrderUpdate(second); err != nil {
		t.Fatal(err)
	}
	if len(margin.repaid) != 2 || math.Abs(margin.repaid[1]-0.285) > 1e-12 ||
		s.pendingRepay[71].ExecutedQty != 0.7 || math.Abs(s.pendingRepay[71].BaseFeeQty-0.025) > 1e-12 {
		t.Fatalf("second repayment/cursor failed cumulative fee reconciliation: repaid=%v pending=%+v", margin.repaid, s.pendingRepay[71])
	}
}

func TestSpotShortLiveFillWithoutFeeHistoryKeepsDebtAndDoesNotRepay(t *testing.T) {
	margin := &mockMarginExchange{}
	venue := &spotShortReconcileExchange{}
	s := newSpotShortForTest(&signalTestExecutor{}, venue, margin)
	s.SetRuntimeStateStore(&memoryRuntimeStateStore{})
	s.pendingRepay[72] = spotShortPendingRepay{OrderQuantity: 1}
	var reported error
	s.SetUnresolvedDebtHandler(func(err error) { reported = err })

	err := s.OnOrderUpdate(&position.OrderUpdate{OrderID: 72, Symbol: "BTCUSDT", Side: "BUY",
		Status: "PARTIALLY_FILLED", ExecutedQty: 0.5, AvgPrice: 100})
	if err == nil || reported == nil || len(margin.repaid) != 0 || s.pendingRepay[72].ExecutedQty != 0 {
		t.Fatalf("missing live fill history must preserve debt without repayment: err=%v reported=%v repaid=%v pending=%+v",
			err, reported, margin.repaid, s.pendingRepay[72])
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

type spotShortClientOrderLookupExchange struct {
	spotShortReconcileExchange
	clientOrder *exchange.Order
	clientID    string
	lookupCalls atomic.Int32
	borrowRows  []exchange.MarginBorrowRecord
	borrowTotal int64
	borrowErr   error
	borrowCalls int
}

func (e *spotShortClientOrderLookupExchange) GetOrderByClientOrderID(_ context.Context, symbol, clientOrderID string) (*exchange.Order, error) {
	e.lookupCalls.Add(1)
	if symbol != "BTCUSDT" || clientOrderID != e.clientID {
		return nil, errors.New("unexpected client order lookup scope")
	}
	return e.clientOrder, nil
}

func TestSpotShortRuntimeReconciliationRetriesPendingBorrowOrder(t *testing.T) {
	const clientOrderID = "runtime-margin-sell-cid"
	venue := &spotShortClientOrderLookupExchange{
		clientID: clientOrderID,
		clientOrder: &exchange.Order{OrderID: 92, ClientOrderID: clientOrderID, Symbol: "BTCUSDT", Side: exchange.SideSell,
			Quantity: 0.25, ExecutedQty: 0.25, Status: exchange.OrderStatusFilled},
	}
	cfg := &config.Config{}
	cfg.Trading.BotID = "spot-short-runtime-reconcile"
	cfg.Trading.Symbol = "BTCUSDT"
	state := spotShortRuntimeState{BotID: spotShortBotID(cfg), Strategy: "spot_short", Symbol: "BTCUSDT", BaseAsset: "BTC",
		PendingRepay: map[int64]spotShortPendingRepay{}, PendingBorrow: map[string]spotShortPendingBorrow{}, PendingBuy: map[string]spotShortPendingBuy{}}
	payload, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	store := &memoryRuntimeStateStore{version: spotShortRuntimeStateSchemaVersion, payload: string(payload), found: true}
	strategy := NewSpotShortStrategy("spot_short", cfg, &signalTestExecutor{}, venue, nil, nil)
	strategy.SetRuntimeStateStore(store)
	strategy.SetEventBus(event.NewEventBus(10))
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	if err := strategy.Start(ctx); err != nil {
		t.Fatalf("start SpotShort strategy: %v", err)
	}
	t.Cleanup(func() { _ = strategy.Stop() })

	strategy.mu.Lock()
	strategy.pendingBorrow[clientOrderID] = spotShortPendingBorrow{Amount: 0.25, Phase: "borrowed", BorrowTransferID: 91,
		CreatedAtUnixMilli: time.Now().Add(-time.Minute).UnixMilli()}
	if err := strategy.persistRuntimeStateLocked(); err != nil {
		strategy.mu.Unlock()
		t.Fatalf("persist runtime test intent: %v", err)
	}
	strategy.mu.Unlock()

	deadline := time.Now().Add(6 * time.Second)
	for time.Now().Before(deadline) {
		strategy.mu.RLock()
		_, pending := strategy.pendingBorrow[clientOrderID]
		strategy.mu.RUnlock()
		if !pending && venue.lookupCalls.Load() > 0 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("runtime reconciler did not verify and clear pending margin sell intent; lookups=%d", venue.lookupCalls.Load())
}

func TestSpotShortReconciliationRetainsBorrowIntentAfterPartialTerminalSell(t *testing.T) {
	const clientOrderID = "partial-terminal-margin-sell-cid"
	venue := &spotShortClientOrderLookupExchange{
		clientID: clientOrderID,
		clientOrder: &exchange.Order{OrderID: 93, ClientOrderID: clientOrderID, Symbol: "BTCUSDT", Side: exchange.SideSell,
			Quantity: 0.5, ExecutedQty: 0.2, Status: exchange.OrderStatusCanceled},
	}
	strategy := newSpotShortForTest(&signalTestExecutor{}, venue, &mockMarginExchange{})
	intent := spotShortPendingBorrow{Amount: 0.5, Phase: "borrowed", BorrowTransferID: 94, CreatedAtUnixMilli: time.Now().UnixMilli()}
	strategy.pendingBorrow[clientOrderID] = intent

	err := strategy.reconcilePendingBorrowIntents(context.Background())
	if err == nil || !strings.Contains(err.Error(), "unexecuted borrowed asset remains unresolved") {
		t.Fatalf("partial terminal sell must remain unresolved, got %v", err)
	}
	if strategy.pendingBorrow[clientOrderID] != intent {
		t.Fatalf("partial terminal sell must preserve borrow intent: %+v", strategy.pendingBorrow)
	}
}

func (e *spotShortClientOrderLookupExchange) GetMarginBorrowHistory(_ context.Context, asset string, startTime, endTime int64, page, pageSize int) ([]exchange.MarginBorrowRecord, int64, error) {
	e.borrowCalls++
	if asset != "BTC" || startTime <= 0 || endTime < startTime || page != 1 || pageSize != 100 {
		return nil, 0, errors.New("unexpected margin borrow history query")
	}
	return e.borrowRows, e.borrowTotal, e.borrowErr
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
		fills: []*exchange.OrderFill{{OrderID: 71, TradeID: "trade-1", Symbol: "BTCUSDT", Side: exchange.SideBuy, Price: 100, Quantity: 0.5, Commission: 0.001, CommissionAsset: "BTC", BaseFeeQty: 0.001}},
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

func TestSpotShortStartupReconcilesPersistedBuyIntentByExactClientOrderID(t *testing.T) {
	cfg := &config.Config{}
	cfg.Trading.BotID = "spot-short-cid-reconcile"
	cfg.Trading.Symbol = "BTCUSDT"
	clientOrderID := "buy-recovery-cid"
	order := &exchange.Order{OrderID: 88, ClientOrderID: clientOrderID, Symbol: "BTCUSDT", Side: exchange.SideBuy,
		Status: exchange.OrderStatusNew, Quantity: 0.4}
	state := spotShortRuntimeState{
		BotID: cfg.Trading.BotID, Strategy: "spot_short", Symbol: "BTCUSDT", BaseAsset: "BTC",
		PendingRepay: map[int64]spotShortPendingRepay{}, PendingBuy: map[string]spotShortPendingBuy{
			clientOrderID: {Quantity: 0.4, CreatedAtUnixMilli: time.Now().UnixMilli()},
		},
	}
	payload, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	venue := &spotShortClientOrderLookupExchange{
		spotShortReconcileExchange: spotShortReconcileExchange{order: order},
		clientOrder:                order, clientID: clientOrderID,
	}
	store := &memoryRuntimeStateStore{version: spotShortRuntimeStateSchemaVersion, payload: string(payload), found: true}
	strategy := NewSpotShortStrategy("spot_short", cfg, &signalTestExecutor{}, venue, nil, nil)
	strategy.SetRuntimeStateStore(store)
	if err := strategy.Start(context.Background()); err != nil {
		t.Fatalf("reconcile exact persisted buy intent: %v", err)
	}
	if len(strategy.pendingBuy) != 0 || strategy.pendingRepay[88].OrderQuantity != 0.4 {
		t.Fatalf("buy intent was not safely rebound to exchange order: pendingBuy=%v pendingRepay=%v", strategy.pendingBuy, strategy.pendingRepay)
	}
}

func TestSpotShortStartupRejectsMismatchedClientOrderRecovery(t *testing.T) {
	cfg := &config.Config{}
	cfg.Trading.BotID = "spot-short-cid-mismatch"
	cfg.Trading.Symbol = "BTCUSDT"
	clientOrderID := "buy-recovery-cid"
	state := spotShortRuntimeState{
		BotID: cfg.Trading.BotID, Strategy: "spot_short", Symbol: "BTCUSDT", BaseAsset: "BTC",
		PendingRepay: map[int64]spotShortPendingRepay{}, PendingBuy: map[string]spotShortPendingBuy{
			clientOrderID: {Quantity: 0.4, CreatedAtUnixMilli: time.Now().UnixMilli()},
		},
	}
	payload, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	venue := &spotShortClientOrderLookupExchange{
		spotShortReconcileExchange: spotShortReconcileExchange{},
		clientOrder:                &exchange.Order{OrderID: 88, ClientOrderID: clientOrderID, Symbol: "ETHUSDT", Side: exchange.SideBuy, Quantity: 0.4},
		clientID:                   clientOrderID,
	}
	store := &memoryRuntimeStateStore{version: spotShortRuntimeStateSchemaVersion, payload: string(payload), found: true}
	strategy := NewSpotShortStrategy("spot_short", cfg, &signalTestExecutor{}, venue, nil, nil)
	strategy.SetRuntimeStateStore(store)
	if err := strategy.Start(context.Background()); err == nil || !strings.Contains(err.Error(), "identity/quantity mismatch") {
		t.Fatalf("startup must reject order returned with another symbol: %v", err)
	}
}

func TestSpotShortStartupRecoversPreparedBorrowFromUniqueConfirmedHistory(t *testing.T) {
	cfg := &config.Config{}
	cfg.Trading.BotID = "spot-short-borrow-history-reconcile"
	cfg.Trading.Symbol = "BTCUSDT"
	clientOrderID := "borrow-history-cid"
	createdAt := time.Now().UTC().Add(-time.Second).UnixMilli()
	state := spotShortRuntimeState{
		BotID: cfg.Trading.BotID, Strategy: "spot_short", Symbol: "BTCUSDT", BaseAsset: "BTC",
		PendingRepay: map[int64]spotShortPendingRepay{}, PendingBorrow: map[string]spotShortPendingBorrow{
			clientOrderID: {Amount: 0.4, Phase: "prepared", CreatedAtUnixMilli: createdAt},
		},
	}
	payload, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	store := &memoryRuntimeStateStore{version: spotShortRuntimeStateSchemaVersion, payload: string(payload), found: true}
	venue := &spotShortClientOrderLookupExchange{
		clientOrder: &exchange.Order{OrderID: 91, ClientOrderID: clientOrderID, Symbol: "BTCUSDT", Side: exchange.SideSell, Quantity: 0.4, ExecutedQty: 0.4, Status: exchange.OrderStatusFilled},
		clientID:    clientOrderID,
		borrowRows:  []exchange.MarginBorrowRecord{{TransferID: 7001, Asset: "BTC", Amount: 0.4, Status: "CONFIRMED", Timestamp: createdAt}},
		borrowTotal: 1,
	}
	strategy := NewSpotShortStrategy("spot_short", cfg, &signalTestExecutor{}, venue, nil, nil)
	strategy.SetRuntimeStateStore(store)
	if err := strategy.Start(context.Background()); err != nil {
		t.Fatalf("recover uniquely confirmed borrow and exact sell: %v", err)
	}
	if venue.borrowCalls != 1 || len(strategy.pendingBorrow) != 0 {
		t.Fatalf("prepared borrow was not reconciled once: calls=%d pending=%v", venue.borrowCalls, strategy.pendingBorrow)
	}
}

func TestSpotShortStartupKeepsAmbiguousBorrowHistoryBlocked(t *testing.T) {
	cfg := &config.Config{}
	cfg.Trading.BotID = "spot-short-borrow-history-ambiguous"
	cfg.Trading.Symbol = "BTCUSDT"
	clientOrderID := "borrow-history-cid"
	createdAt := time.Now().UTC().Add(-time.Second).UnixMilli()
	state := spotShortRuntimeState{
		BotID: cfg.Trading.BotID, Strategy: "spot_short", Symbol: "BTCUSDT", BaseAsset: "BTC",
		PendingRepay: map[int64]spotShortPendingRepay{}, PendingBorrow: map[string]spotShortPendingBorrow{
			clientOrderID: {Amount: 0.4, Phase: "prepared", CreatedAtUnixMilli: createdAt},
		},
	}
	payload, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	store := &memoryRuntimeStateStore{version: spotShortRuntimeStateSchemaVersion, payload: string(payload), found: true}
	venue := &spotShortClientOrderLookupExchange{
		clientID: clientOrderID,
		borrowRows: []exchange.MarginBorrowRecord{
			{TransferID: 7001, Asset: "BTC", Amount: 0.4, Status: "CONFIRMED", Timestamp: createdAt},
			{TransferID: 7002, Asset: "BTC", Amount: 0.4, Status: "CONFIRMED", Timestamp: createdAt + 1},
		},
		borrowTotal: 2,
	}
	strategy := NewSpotShortStrategy("spot_short", cfg, &signalTestExecutor{}, venue, nil, nil)
	strategy.SetRuntimeStateStore(store)
	if err := strategy.Start(context.Background()); err == nil || !strings.Contains(err.Error(), "2 matching history records") {
		t.Fatalf("ambiguous borrow history must keep startup blocked: %v (history calls=%d rows=%+v)", err, venue.borrowCalls, venue.borrowRows)
	}
	if len(strategy.pendingBorrow) != 1 {
		t.Fatalf("ambiguous history must preserve the durable borrow intent: %v", strategy.pendingBorrow)
	}
}
