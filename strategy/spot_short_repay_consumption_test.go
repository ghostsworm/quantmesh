package strategy

import (
	"context"
	"strings"
	"testing"
	"time"

	"quantmesh/exchange"
	"quantmesh/position"
)

func TestSpotShortConsumedRepaymentCannotBeReusedAfterRestart(t *testing.T) {
	for _, exact := range []bool{false, true} {
		t.Run(map[bool]string{false: "history", true: "exact_id"}[exact], func(t *testing.T) {
			started := time.Now().Add(-time.Second).UnixMilli()
			margin := &mockMarginExchange{}
			history := spotShortRepayHistoryOnlyExchange{IExchange: margin, rows: []exchange.MarginBorrowRecord{{TransferID: 81, Asset: "BTC", Amount: 0.499, Status: "CONFIRMED", Timestamp: started + 1}}}
			s := newSpotShortForTest(&signalTestExecutor{}, &signalTestExchange{}, margin)
			s.rawEx = &history
			if exact {
				s.rawEx = &spotShortRepayExactHistoryExchange{history}
			}
			store := &memoryRuntimeStateStore{}
			s.SetRuntimeStateStore(store)
			pending := spotShortPendingRepay{OrderQuantity: 0.5, RepayUncertain: true, RepayAmount: 0.499, RepayStartedAtUnixMilli: started, RepayExpectedExecutedQty: 0.5, RepayExpectedBaseFeeQty: 0.001}
			if exact {
				pending.RepayTransferID = 81
			}
			s.pendingRepay[71] = pending
			if err := s.OnOrderUpdate(&position.OrderUpdate{OrderID: 71, Side: "BUY", Status: "FILLED", ExecutedQty: 0.5}); err != nil {
				t.Fatal(err)
			}
			if len(s.pendingRepay) != 0 {
				t.Fatal("terminal intent was not cleared")
			}
			restarted := newSpotShortForTest(&signalTestExecutor{}, &signalTestExchange{}, margin)
			restarted.rawEx = s.rawEx
			restarted.SetRuntimeStateStore(store)
			restarted.mu.Lock()
			err := restarted.restoreRuntimeStateLocked()
			restarted.mu.Unlock()
			if err != nil {
				t.Fatal(err)
			}
			restarted.pendingRepay[72] = pending
			restarted.mu.Lock()
			err = restarted.persistRuntimeStateLocked()
			restarted.mu.Unlock()
			if err != nil {
				t.Fatal(err)
			}
			original := store.payload
			if err := restarted.OnOrderUpdate(&position.OrderUpdate{OrderID: 72, Side: "BUY", Status: "FILLED", ExecutedQty: 0.5}); err == nil {
				t.Fatal("consumed transfer was reused after terminal intent deletion and restart")
			}
			if restarted.pendingRepay[72] != pending || store.payload != original || len(margin.repaid) != 0 {
				t.Fatal("reused transfer changed durable state or repeated repayment")
			}
		})
	}
}

type spotShortDuplicateRepayAck struct{ *mockMarginExchange }

func (e *spotShortDuplicateRepayAck) Repay(ctx context.Context, asset string, amount float64) (int64, error) {
	_, err := e.mockMarginExchange.Repay(ctx, asset, amount)
	return 1, err
}

func TestSpotShortConsumedRepaymentAckPreservesUncertainOutcome(t *testing.T) {
	margin := &mockMarginExchange{}
	venue := &spotShortReconcileExchange{}
	s := newSpotShortForTest(&signalTestExecutor{}, venue, margin)
	s.smEx = &spotShortDuplicateRepayAck{margin}
	store := &memoryRuntimeStateStore{}
	s.SetRuntimeStateStore(store)
	for _, id := range []int64{71, 72} {
		venue.fills = []*exchange.OrderFill{{OrderID: id, TradeID: "fill", Symbol: "BTCUSDT", Side: exchange.SideBuy, Price: 100, Quantity: 0.5, Commission: 0.001, CommissionAsset: "BTC", BaseFeeQty: 0.001}}
		s.pendingRepay[id] = spotShortPendingRepay{OrderQuantity: 0.5}
		err := s.OnOrderUpdate(&position.OrderUpdate{OrderID: id, Side: "BUY", Status: "FILLED", ExecutedQty: 0.5})
		if id == 71 && err != nil {
			t.Fatal(err)
		}
		if id == 72 && (err == nil || !strings.Contains(err.Error(), "already consumed")) {
			t.Fatalf("duplicate ACK accepted: %v", err)
		}
	}
	if got := s.pendingRepay[72]; !got.RepayUncertain || got.RepayTransferID != 1 || got.ExecutedQty != 0 || len(margin.repaid) != 2 {
		t.Fatalf("duplicate ACK discarded unresolved outcome: %+v", got)
	}
	restored := newSpotShortForTest(&signalTestExecutor{}, venue, margin)
	restored.SetRuntimeStateStore(store)
	restored.mu.Lock()
	err := restored.restoreRuntimeStateLocked()
	restored.mu.Unlock()
	if err != nil || restored.consumedRepayTransfers[1] != 71 || restored.pendingRepay[72] != s.pendingRepay[72] {
		t.Fatalf("duplicate ACK evidence not durable: %v", err)
	}
}

func TestSpotShortRepaymentConsumptionSaveFailureCanRetry(t *testing.T) {
	started := time.Now().Add(-time.Second).UnixMilli()
	margin := &mockMarginExchange{}
	history := &spotShortRepayHistoryOnlyExchange{IExchange: margin, rows: []exchange.MarginBorrowRecord{{TransferID: 81, Asset: "BTC", Amount: 0.499, Status: "CONFIRMED", Timestamp: started + 1}}}
	s := newSpotShortForTest(&signalTestExecutor{}, &signalTestExchange{}, margin)
	s.rawEx = history
	store := &failNthRuntimeStateSave{failAt: 2}
	s.SetRuntimeStateStore(store)
	pending := spotShortPendingRepay{OrderQuantity: 0.5, RepayUncertain: true, RepayAmount: 0.499, RepayStartedAtUnixMilli: started, RepayExpectedExecutedQty: 0.5, RepayExpectedBaseFeeQty: 0.001}
	s.pendingRepay[71] = pending
	s.mu.Lock()
	err := s.persistRuntimeStateLocked()
	s.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	original := store.payload
	update := &position.OrderUpdate{OrderID: 71, Side: "BUY", Status: "FILLED", ExecutedQty: 0.5}
	if err := s.OnOrderUpdate(update); err == nil {
		t.Fatal("injected save failure was ignored")
	}
	if len(s.consumedRepayTransfers) != 0 || s.pendingRepay[71] != pending || store.payload != original {
		t.Fatal("failed save consumed transfer or changed cursors")
	}
	if err := s.OnOrderUpdate(update); err != nil {
		t.Fatal(err)
	}
	if len(s.pendingRepay) != 0 || s.consumedRepayTransfers[81] != 71 || len(margin.repaid) != 0 {
		t.Fatal("retry failed or repeated repayment")
	}
}
