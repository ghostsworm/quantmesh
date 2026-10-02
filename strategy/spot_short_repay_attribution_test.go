package strategy

import (
	"context"
	"testing"
	"time"

	"quantmesh/exchange"
	"quantmesh/position"
)

type spotShortRepayHistoryOnlyExchange struct {
	exchange.IExchange
	rows []exchange.MarginBorrowRecord
}

func (e *spotShortRepayHistoryOnlyExchange) GetMarginTransactionHistory(context.Context, string, string, int64, int64, int, int) ([]exchange.MarginBorrowRecord, int64, error) {
	return e.rows, int64(len(e.rows)), nil
}

type spotShortRepayExactHistoryExchange struct {
	spotShortRepayHistoryOnlyExchange
}

func (e *spotShortRepayExactHistoryExchange) GetMarginTransactionByID(context.Context, string, string, int64) (exchange.MarginBorrowRecord, error) {
	return e.rows[0], nil
}

func TestSpotShortRepayRecoveryRejectsConflictingTransferAttribution(t *testing.T) {
	for _, tc := range []struct {
		name              string
		targetID, otherID int64
		exact             bool
		other             bool
	}{
		{name: "history_competing_unknown", other: true},
		{name: "history_already_assigned", otherID: 81, other: true},
		{name: "history_ignores_saved_id", targetID: 82},
		{name: "exact_duplicate_assignment", targetID: 81, otherID: 81, exact: true, other: true},
		{name: "exact_competing_unknown", targetID: 81, exact: true, other: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			started := time.Now().Add(-time.Second).UnixMilli()
			margin := &mockMarginExchange{}
			history := spotShortRepayHistoryOnlyExchange{IExchange: margin, rows: []exchange.MarginBorrowRecord{{TransferID: 81, Asset: "BTC", Amount: 0.499, Status: "CONFIRMED", Timestamp: started + 1}}}
			s := newSpotShortForTest(&signalTestExecutor{}, &signalTestExchange{}, margin)
			s.rawEx = &history
			if tc.exact {
				s.rawEx = &spotShortRepayExactHistoryExchange{history}
			}
			store := &memoryRuntimeStateStore{}
			s.SetRuntimeStateStore(store)
			pending := spotShortPendingRepay{OrderQuantity: 1, RepayUncertain: true, RepayTransferID: tc.targetID, RepayAmount: 0.499, RepayStartedAtUnixMilli: started, RepayExpectedExecutedQty: 0.5, RepayExpectedBaseFeeQty: 0.001}
			s.pendingRepay[71] = pending
			other := pending
			other.RepayTransferID = tc.otherID
			if tc.other {
				s.pendingRepay[72] = other
			}
			s.mu.Lock()
			err := s.persistRuntimeStateLocked()
			s.mu.Unlock()
			if err != nil {
				t.Fatal(err)
			}
			original := store.payload
			restarted := newSpotShortForTest(&signalTestExecutor{}, &signalTestExchange{}, margin)
			restarted.rawEx = s.rawEx
			restarted.SetRuntimeStateStore(store)
			restarted.mu.Lock()
			err = restarted.restoreRuntimeStateLocked()
			restarted.mu.Unlock()
			if err != nil {
				t.Fatal(err)
			}
			s = restarted
			if err := s.OnOrderUpdate(&position.OrderUpdate{OrderID: 71, Side: "BUY", Status: "PARTIALLY_FILLED", ExecutedQty: 0.5}); err == nil {
				t.Fatal("conflicting repayment transfer advanced the fill cursor")
			}
			if s.pendingRepay[71] != pending || (tc.other && s.pendingRepay[72] != other) || store.payload != original || len(margin.repaid) != 0 {
				t.Fatal("conflicting repayment changed durable evidence or repeated repayment")
			}
		})
	}
}

func TestSpotShortRepayRecoveryPreservesUnambiguousProgress(t *testing.T) {
	for _, name := range []string{"known_unsubmitted", "different_amount", "later_submission", "other_known_transfer", "settled_order", "saved_id_history_fallback"} {
		t.Run(name, func(t *testing.T) {
			started := time.Now().Add(-time.Second).UnixMilli()
			margin := &mockMarginExchange{}
			history := &spotShortRepayHistoryOnlyExchange{IExchange: margin, rows: []exchange.MarginBorrowRecord{{TransferID: 81, Asset: "BTC", Amount: 0.499, Status: "CONFIRMED", Timestamp: started + 1}}}
			s := newSpotShortForTest(&signalTestExecutor{}, &signalTestExchange{}, margin)
			s.rawEx = history
			store := &memoryRuntimeStateStore{}
			s.SetRuntimeStateStore(store)
			pending := spotShortPendingRepay{OrderQuantity: 1, RepayUncertain: true, RepayAmount: 0.499, RepayStartedAtUnixMilli: started, RepayExpectedExecutedQty: 0.5, RepayExpectedBaseFeeQty: 0.001}
			other := pending
			switch name {
			case "known_unsubmitted":
				other.RepayPrepared = true
			case "different_amount":
				other.RepayAmount = 0.249
				other.RepayExpectedExecutedQty = 0.25
			case "later_submission":
				other.RepayStartedAtUnixMilli = started + 2
			case "other_known_transfer":
				other.RepayTransferID = 82
			case "settled_order":
				other = spotShortPendingRepay{OrderQuantity: 1}
			case "saved_id_history_fallback":
				pending.RepayTransferID = 81
				other = spotShortPendingRepay{OrderQuantity: 1}
			}
			s.pendingRepay[71], s.pendingRepay[72] = pending, other
			update := &position.OrderUpdate{OrderID: 71, Side: "BUY", Status: "PARTIALLY_FILLED", ExecutedQty: 0.5}
			if err := s.OnOrderUpdate(update); err != nil {
				t.Fatal(err)
			}
			if err := s.OnOrderUpdate(update); err != nil {
				t.Fatal(err)
			}
			if got := s.pendingRepay[71]; got.RepayUncertain || got.ExecutedQty != 0.5 || got.BaseFeeQty != 0.001 || s.pendingRepay[72] != other || len(margin.repaid) != 0 {
				t.Fatalf("unambiguous recovery changed unrelated state or repeated repayment: %+v", got)
			}
			restored := newSpotShortForTest(&signalTestExecutor{}, &signalTestExchange{}, margin)
			restored.SetRuntimeStateStore(store)
			restored.mu.Lock()
			err := restored.restoreRuntimeStateLocked()
			restored.mu.Unlock()
			if err != nil || restored.pendingRepay[71] != s.pendingRepay[71] || restored.pendingRepay[72] != other {
				t.Fatalf("recovery was not durable: %v", err)
			}
		})
	}
}
