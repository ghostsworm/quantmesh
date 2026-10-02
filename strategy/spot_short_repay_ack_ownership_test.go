package strategy

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"quantmesh/exchange"
	"quantmesh/execution"
	"quantmesh/position"
)

type spotShortRepayAckOwnershipExchange struct {
	*mockMarginExchange
	gate *execution.OpeningGate
}

func (e *spotShortRepayAckOwnershipExchange) Repay(ctx context.Context, asset string, amount float64) (int64, error) {
	id, err := e.mockMarginExchange.Repay(ctx, asset, amount)
	if err == nil {
		e.mockFCExchange.mu.Lock()
		e.mockFCExchange.repayAmount = amount
		e.mockFCExchange.mu.Unlock()
		e.repayHistory = []exchange.MarginBorrowRecord{{TransferID: id, Asset: asset, Amount: amount, Status: "CONFIRMED", Timestamp: time.Now().UnixMilli()}}
	}
	e.gate.Block("runtime_ownership_unverified")
	return id, err
}

func (e *spotShortRepayAckOwnershipExchange) GetMarginTransactionByID(_ context.Context, asset, kind string, id int64) (exchange.MarginBorrowRecord, error) {
	for _, record := range e.repayHistory {
		if record.TransferID == id && record.Asset == asset && kind == "REPAY" {
			return record, nil // Keep the execution timestamp; querying must not mint a later event.
		}
	}
	return exchange.MarginBorrowRecord{}, nil
}

func TestSpotShortRepayAcknowledgmentOwnershipLossKeepsRecoveryEvidence(t *testing.T) {
	gate := &execution.OpeningGate{}
	margin := &mockMarginExchange{}
	venue := &spotShortReconcileExchange{fills: []*exchange.OrderFill{{OrderID: 71, TradeID: "fill-71", Symbol: "BTCUSDT", Side: exchange.SideBuy, Price: 100, Quantity: 0.5, Commission: 0.001, CommissionAsset: "BTC", BaseFeeQty: 0.001}}}
	s := newSpotShortForTest(&signalTestExecutor{}, venue, margin)
	s.SetOpeningGate(gate)
	ack := &spotShortRepayAckOwnershipExchange{mockMarginExchange: margin, gate: gate}
	s.smEx = ack
	s.rawEx = ack
	store := &memoryRuntimeStateStore{}
	s.SetRuntimeStateStore(store)
	coordinator := &walletCoordinationTestLock{}
	if err := s.SetAccountWalletCoordinationLock(coordinator, "funding_carry_wallet:"+t.Name()); err != nil {
		t.Fatal(err)
	}
	s.pendingRepay[71] = spotShortPendingRepay{OrderQuantity: 1}
	update := &position.OrderUpdate{OrderID: 71, Symbol: "BTCUSDT", Side: "BUY", Status: "PARTIALLY_FILLED", ExecutedQty: 0.5}
	err := s.OnOrderUpdate(update)
	if err == nil || !strings.Contains(err.Error(), "runtime ownership is unverified") {
		t.Fatalf("lost owner accepted repayment acknowledgment: %v", err)
	}
	pending := s.pendingRepay[71]
	var saved spotShortRuntimeState
	if err := json.Unmarshal([]byte(store.payload), &saved); err != nil {
		t.Fatal(err)
	}
	if !pending.RepayUncertain || pending.RepayPrepared || pending.ExecutedQty != 0 || pending.BaseFeeQty != 0 || pending.RepayTransferID != 1 || saved.PendingRepay[71] != pending || len(margin.repaid) != 1 || coordinator.active != 0 {
		t.Fatalf("repayment evidence was lost or prematurely resolved: %+v repayments=%v", pending, margin.repaid)
	}
	gate.Unblock("runtime_ownership_unverified")
	if err := s.OnOrderUpdate(update); err != nil {
		t.Fatal(err)
	}
	if err := s.OnOrderUpdate(update); err != nil {
		t.Fatal(err)
	}
	if got := s.pendingRepay[71]; got.RepayUncertain || got.ExecutedQty != 0.5 || got.BaseFeeQty != 0.001 || len(margin.repaid) != 1 {
		t.Fatal("confirmed recovery repeated repayment or lost cursors")
	}
}
