package strategy

import (
	"context"
	"strings"
	"testing"
	"time"

	"quantmesh/exchange"
	"quantmesh/execution"
	"quantmesh/position"
)

type spotShortSellRecoveryOwnershipExchange struct {
	spotShortClientOrderLookupExchange
	gate *execution.OpeningGate
}

type spotShortBorrowRecoveryOwnershipExchange struct {
	spotShortClientOrderLookupExchange
	gate *execution.OpeningGate
}

func (e *spotShortBorrowRecoveryOwnershipExchange) GetMarginBorrowHistory(ctx context.Context, asset string, start, end int64, page, size int) ([]exchange.MarginBorrowRecord, int64, error) {
	rows, total, err := e.spotShortClientOrderLookupExchange.GetMarginBorrowHistory(ctx, asset, start, end, page, size)
	e.gate.Block("runtime_ownership_unverified")
	return rows, total, err
}

func TestSpotShortBorrowTransferRecoveryDoesNotCommitAfterOwnershipLoss(t *testing.T) {
	const cid = "owned-margin-borrow"
	started := time.Now().Add(-time.Second).UnixMilli()
	gate := &execution.OpeningGate{}
	venue := &spotShortBorrowRecoveryOwnershipExchange{
		gate: gate,
		spotShortClientOrderLookupExchange: spotShortClientOrderLookupExchange{
			clientID:    cid,
			clientOrder: &exchange.Order{OrderID: 91, ClientOrderID: cid, Symbol: "BTCUSDT", Side: exchange.SideSell, Status: exchange.OrderStatusFilled, Quantity: 0.5, ExecutedQty: 0.5},
			borrowRows:  []exchange.MarginBorrowRecord{{TransferID: 81, Asset: "BTC", Status: "CONFIRMED", Amount: 0.5, Timestamp: started + 1}}, borrowTotal: 1,
		},
	}
	s := newSpotShortForTest(&spotShortOwnershipOrderExecutor{}, venue, &mockMarginExchange{})
	s.SetOpeningGate(gate)
	store := &memoryRuntimeStateStore{}
	s.SetRuntimeStateStore(store)
	intent := spotShortPendingBorrow{Amount: 0.5, Phase: "prepared", CreatedAtUnixMilli: started}
	s.pendingBorrow[cid] = intent
	s.mu.Lock()
	err := s.persistRuntimeStateLocked()
	s.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	original := store.payload
	err = s.reconcileRuntimeState(context.Background())
	if err == nil || !strings.Contains(err.Error(), "runtime ownership is unverified") || s.pendingBorrow[cid] != intent || store.payload != original {
		t.Fatalf("lost owner committed borrow transfer recovery: %v", err)
	}
	s.ex = &venue.spotShortClientOrderLookupExchange
	gate.Unblock("runtime_ownership_unverified")
	if err := s.reconcileRuntimeState(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(s.pendingBorrow) != 0 {
		t.Fatal("valid transfer and sell recovery did not finish")
	}
}

type spotShortRepayRecoveryOwnershipExchange struct {
	*mockMarginExchange
	gate *execution.OpeningGate
}

func (e *spotShortRepayRecoveryOwnershipExchange) GetMarginTransactionHistory(ctx context.Context, asset, kind string, start, end int64, page, size int) ([]exchange.MarginBorrowRecord, int64, error) {
	rows, total, err := e.mockMarginExchange.GetMarginTransactionHistory(ctx, asset, kind, start, end, page, size)
	e.gate.Block("runtime_ownership_unverified")
	return rows, total, err
}

func TestSpotShortRepayRecoveryDoesNotCommitAfterOwnershipLoss(t *testing.T) {
	gate := &execution.OpeningGate{}
	started := time.Now().Add(-time.Second).UnixMilli()
	margin := &mockMarginExchange{repayHistory: []exchange.MarginBorrowRecord{{TransferID: 81, Asset: "BTC", Status: "CONFIRMED", Amount: 0.499, Timestamp: started + 1}}}
	s := newSpotShortForTest(&spotShortOwnershipOrderExecutor{}, &signalTestExchange{}, margin)
	s.rawEx = &spotShortRepayRecoveryOwnershipExchange{mockMarginExchange: margin, gate: gate}
	s.SetOpeningGate(gate)
	store := &memoryRuntimeStateStore{}
	s.SetRuntimeStateStore(store)
	pending := spotShortPendingRepay{OrderQuantity: 1, RepayUncertain: true, RepayAmount: 0.499, RepayStartedAtUnixMilli: started, RepayExpectedExecutedQty: 0.5, RepayExpectedBaseFeeQty: 0.001}
	s.pendingRepay[71] = pending
	s.mu.Lock()
	err := s.persistRuntimeStateLocked()
	s.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	original := store.payload
	update := &position.OrderUpdate{OrderID: 71, Symbol: "BTCUSDT", Side: "BUY", Status: "PARTIALLY_FILLED", ExecutedQty: 0.5}
	err = s.OnOrderUpdate(update)
	if err == nil || !strings.Contains(err.Error(), "runtime ownership is unverified") || s.pendingRepay[71] != pending || store.payload != original {
		t.Fatalf("lost owner committed repayment recovery: err=%v pending=%+v", err, s.pendingRepay[71])
	}
	s.rawEx = margin
	gate.Unblock("runtime_ownership_unverified")
	if err := s.OnOrderUpdate(update); err != nil {
		t.Fatal(err)
	}
	if got := s.pendingRepay[71]; got.RepayUncertain || got.ExecutedQty != 0.5 || got.BaseFeeQty != 0.001 || len(margin.repaid) != 0 {
		t.Fatal("valid repayment recovery failed or repeated repayment")
	}
}

func (e *spotShortSellRecoveryOwnershipExchange) GetOrderByClientOrderID(ctx context.Context, symbol, cid string) (*exchange.Order, error) {
	order, err := e.spotShortClientOrderLookupExchange.GetOrderByClientOrderID(ctx, symbol, cid)
	e.gate.Block("runtime_ownership_unverified")
	return order, err
}

func TestSpotShortSellRecoveryDoesNotCommitAfterOwnershipLoss(t *testing.T) {
	const cid = "owned-margin-sell"
	gate := &execution.OpeningGate{}
	venue := &spotShortSellRecoveryOwnershipExchange{
		gate: gate,
		spotShortClientOrderLookupExchange: spotShortClientOrderLookupExchange{
			clientID:    cid,
			clientOrder: &exchange.Order{OrderID: 91, ClientOrderID: cid, Symbol: "BTCUSDT", Side: exchange.SideSell, Status: exchange.OrderStatusFilled, Quantity: 0.5, ExecutedQty: 0.5},
		},
	}
	margin := &mockMarginExchange{}
	s := newSpotShortForTest(&spotShortOwnershipOrderExecutor{}, venue, margin)
	s.SetOpeningGate(gate)
	store := &memoryRuntimeStateStore{}
	s.SetRuntimeStateStore(store)
	coordinator := &walletCoordinationTestLock{}
	if err := s.SetAccountWalletCoordinationLock(coordinator, "funding_carry_wallet:"+t.Name()); err != nil {
		t.Fatal(err)
	}
	intent := spotShortPendingBorrow{Amount: 0.5, Phase: "borrowed", BorrowTransferID: 81, CreatedAtUnixMilli: 1}
	s.pendingBorrow[cid] = intent
	s.mu.Lock()
	err := s.persistRuntimeStateLocked()
	s.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	original := store.payload
	err = s.reconcileRuntimeState(context.Background())
	if err == nil || !strings.Contains(err.Error(), "runtime ownership is unverified") || s.pendingBorrow[cid] != intent || store.payload != original || coordinator.active != 0 {
		t.Fatalf("lost owner committed sell recovery: err=%v intent=%+v", err, s.pendingBorrow[cid])
	}
	s.ex = &venue.spotShortClientOrderLookupExchange
	gate.Unblock("runtime_ownership_unverified")
	if err := s.reconcileRuntimeState(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(s.pendingBorrow) != 0 || len(margin.borrowed) != 0 || len(margin.repaid) != 0 || coordinator.active != 0 {
		t.Fatal("valid recovery did not complete without wallet mutation")
	}
}
