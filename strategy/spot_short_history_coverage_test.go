package strategy

import (
	"context"
	"testing"
	"time"

	"quantmesh/exchange"
	"quantmesh/position"
)

type spotShortIncompleteRepayHistory struct {
	*mockMarginExchange
	total int64
}

func (e *spotShortIncompleteRepayHistory) GetMarginTransactionHistory(context.Context, string, string, int64, int64, int, int) ([]exchange.MarginBorrowRecord, int64, error) {
	return e.repayHistory, e.total, nil
}

func TestSpotShortBorrowRecoveryRejectsIncompleteHistory(t *testing.T) {
	started := time.Now().Add(-time.Second).UnixMilli()
	venue := &spotShortClientOrderLookupExchange{borrowRows: []exchange.MarginBorrowRecord{{TransferID: 81, Asset: "BTC", Amount: 0.5, Status: "CONFIRMED", Timestamp: started + 1}}, borrowTotal: 2}
	s := newSpotShortForTest(&signalTestExecutor{}, venue, &mockMarginExchange{})
	store := &memoryRuntimeStateStore{}
	s.SetRuntimeStateStore(store)
	intent := spotShortPendingBorrow{Amount: 0.5, Phase: "prepared", CreatedAtUnixMilli: started}
	s.pendingBorrow["cid"] = intent
	s.mu.Lock()
	err := s.persistRuntimeStateLocked()
	s.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	original := store.payload
	if _, err := s.reconcilePendingBorrowTransfer(context.Background(), "cid", intent); err == nil {
		t.Fatal("truncated borrow history was accepted as unique transfer proof")
	}
	if s.pendingBorrow["cid"] != intent || store.payload != original {
		t.Fatal("incomplete history changed borrow intent")
	}
}

func TestSpotShortRepayRecoveryRejectsIncompleteHistory(t *testing.T) {
	started := time.Now().Add(-time.Second).UnixMilli()
	margin := &mockMarginExchange{repayHistory: []exchange.MarginBorrowRecord{{TransferID: 81, Asset: "BTC", Amount: 0.499, Status: "CONFIRMED", Timestamp: started + 1}}}
	s := newSpotShortForTest(&signalTestExecutor{}, &signalTestExchange{}, margin)
	s.rawEx = &spotShortIncompleteRepayHistory{mockMarginExchange: margin, total: 2}
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
	if err := s.OnOrderUpdate(&position.OrderUpdate{OrderID: 71, Side: "BUY", Status: "PARTIALLY_FILLED", ExecutedQty: 0.5}); err == nil {
		t.Fatal("truncated repay history was accepted as unique transaction proof")
	}
	if s.pendingRepay[71] != pending || store.payload != original || len(margin.repaid) != 0 {
		t.Fatal("incomplete history resolved or repeated repayment")
	}
}
