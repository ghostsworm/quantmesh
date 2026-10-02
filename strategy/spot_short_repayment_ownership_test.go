package strategy

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"quantmesh/exchange"
	"quantmesh/execution"
)

type spotShortFillOwnershipLossExchange struct {
	spotShortReconcileExchange
	gate *execution.OpeningGate
}

type spotShortRepaymentOwnershipLossStore struct {
	*memoryRuntimeStateStore
	gate            *execution.OpeningGate
	loseOnSubmitted bool
}

func (s *spotShortRepaymentOwnershipLossStore) SaveRuntimeState(name string, version int, payload string) error {
	if err := s.memoryRuntimeStateStore.SaveRuntimeState(name, version, payload); err != nil {
		return err
	}
	var state spotShortRuntimeState
	if err := json.Unmarshal([]byte(payload), &state); err != nil {
		return err
	}
	if state.PendingRepay[71].RepayUncertain && state.PendingRepay[71].RepayPrepared != s.loseOnSubmitted {
		s.gate.Block("runtime_ownership_unverified")
	}
	return nil
}

func TestSpotShortRecoveryRetainsIntentWhenOwnerLostDuringPersistence(t *testing.T) {
	gate := &execution.OpeningGate{}
	venue := &spotShortReconcileExchange{
		order: &exchange.Order{OrderID: 71, Symbol: "BTCUSDT", Side: exchange.SideBuy, Status: exchange.OrderStatusPartiallyFilled, Quantity: 1, ExecutedQty: 0.5},
		fills: []*exchange.OrderFill{{OrderID: 71, TradeID: "trade-1", Symbol: "BTCUSDT", Side: exchange.SideBuy, Price: 100, Quantity: 0.5, Commission: 0.001, CommissionAsset: "BTC", BaseFeeQty: 0.001}},
	}
	margin := &mockMarginExchange{}
	s, store := spotShortRestoreFixture(t, venue, margin)
	s.SetOpeningGate(gate)
	s.SetRuntimeStateStore(&spotShortRepaymentOwnershipLossStore{memoryRuntimeStateStore: store, gate: gate})
	coordinator := &walletCoordinationTestLock{}
	if err := s.SetAccountWalletCoordinationLock(coordinator, "funding_carry_wallet:"+t.Name()); err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	err := s.restoreRuntimeStateLocked()
	s.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	err = s.reconcileRuntimeState(context.Background())
	if err == nil || !strings.Contains(err.Error(), "runtime ownership is unverified") || len(margin.repaid) != 0 || coordinator.active != 0 {
		t.Fatalf("persistence-time ownership loss permitted repayment: err=%v repayments=%v", err, margin.repaid)
	}
	var saved spotShortRuntimeState
	if err := json.Unmarshal([]byte(store.payload), &saved); err != nil {
		t.Fatal(err)
	}
	pending := s.pendingRepay[71]
	if !pending.RepayUncertain || pending.ExecutedQty != 0 || pending.RepayAmount != 0.499 || pending.RepayTransferID != 0 || saved.PendingRepay[71] != pending {
		t.Fatalf("lost owner cleared or advanced persisted repayment intent: %+v", pending)
	}
	restarted, _ := spotShortRestoreFixture(t, venue, margin)
	restarted.SetRuntimeStateStore(store)
	restarted.SetOpeningGate(&execution.OpeningGate{})
	restarted.mu.Lock()
	err = restarted.restoreRuntimeStateLocked()
	restarted.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if err := restarted.reconcileRuntimeState(context.Background()); err != nil {
		t.Fatalf("restart could not resume definitely unsubmitted repayment: %v", err)
	}
	if err := restarted.reconcileRuntimeState(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(margin.repaid) != 1 || restarted.pendingRepay[71].RepayUncertain {
		t.Fatal("restart repeated repayment or retained resolved intent")
	}
}

func (e *spotShortFillOwnershipLossExchange) GetOrderFills(ctx context.Context, symbol string, id int64) (interface{}, error) {
	result, err := e.spotShortReconcileExchange.GetOrderFills(ctx, symbol, id)
	e.gate.Block("runtime_ownership_unverified")
	return result, err
}

func TestSpotShortRecoveryRejectsOwnershipLossDuringFillQuery(t *testing.T) {
	gate := &execution.OpeningGate{}
	venue := &spotShortFillOwnershipLossExchange{
		gate: gate,
		spotShortReconcileExchange: spotShortReconcileExchange{
			order: &exchange.Order{OrderID: 71, Symbol: "BTCUSDT", Side: exchange.SideBuy, Status: exchange.OrderStatusPartiallyFilled, Quantity: 1, ExecutedQty: 0.5},
			fills: []*exchange.OrderFill{{OrderID: 71, TradeID: "trade-1", Symbol: "BTCUSDT", Side: exchange.SideBuy, Price: 100, Quantity: 0.5, Commission: 0.001, CommissionAsset: "BTC", BaseFeeQty: 0.001}},
		},
	}
	margin := &mockMarginExchange{}
	s, _ := spotShortRestoreFixture(t, venue, margin)
	s.SetOpeningGate(gate)
	coordinator := &walletCoordinationTestLock{}
	if err := s.SetAccountWalletCoordinationLock(coordinator, "funding_carry_wallet:"+t.Name()); err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	err := s.restoreRuntimeStateLocked()
	original := s.pendingRepay[71]
	s.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	err = s.reconcileRuntimeState(context.Background())
	if err == nil || !strings.Contains(err.Error(), "runtime ownership is unverified") || len(margin.repaid) != 0 || s.pendingRepay[71] != original || coordinator.active != 0 {
		t.Fatalf("fill-query ownership loss permitted repayment or changed cursor: err=%v repayments=%v pending=%+v", err, margin.repaid, s.pendingRepay[71])
	}
	// A rightful owner can retry the original evidence without double repayment.
	s.ex = &venue.spotShortReconcileExchange
	gate.Unblock("runtime_ownership_unverified")
	if err := s.reconcileRuntimeState(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(margin.repaid) != 1 || s.pendingRepay[71].ExecutedQty != 0.5 {
		t.Fatal("ownership recovery did not resume pending repayment")
	}
}
