package strategy

import (
	"context"
	"math"
	"strings"
	"testing"

	"quantmesh/exchange"
	"quantmesh/execution"
)

func TestSpotShortRecoveryRejectsLostRuntimeOwnershipAndPreservesRepayment(t *testing.T) {
	venue := &spotShortReconcileExchange{
		order: &exchange.Order{OrderID: 71, Symbol: "BTCUSDT", Side: exchange.SideBuy, Status: exchange.OrderStatusPartiallyFilled, Quantity: 1, ExecutedQty: 0.5},
		fills: []*exchange.OrderFill{{OrderID: 71, TradeID: "trade-1", Symbol: "BTCUSDT", Side: exchange.SideBuy, Price: 100, Quantity: 0.5, Commission: 0.001, CommissionAsset: "BTC", BaseFeeQty: 0.001}},
	}
	margin := &mockMarginExchange{}
	s, _ := spotShortRestoreFixture(t, venue, margin)
	gate := &execution.OpeningGate{}
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
	gate.Block("runtime_ownership_unverified")
	if err := s.reconcileRuntimeState(context.Background()); err == nil {
		t.Fatal("lost runtime owner entered repayment recovery")
	}
	if len(margin.repaid) != 0 || len(coordinator.acquired) != 0 || s.pendingRepay[71] != original {
		t.Fatal("rejected recovery mutated wallet or repayment cursor")
	}
	gate.Unblock("runtime_ownership_unverified")
	gate.Block("manual")
	if err := s.reconcileRuntimeState(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(margin.repaid) != 1 || math.Abs(margin.repaid[0]-0.499) > 1e-12 || !gate.HasBlock("manual") {
		t.Fatalf("protective net-base repayment did not recover under manual pause: %v", margin.repaid)
	}
	if pending := s.pendingRepay[71]; pending.RepayUncertain || pending.ExecutedQty != 0.5 || pending.BaseFeeQty != 0.001 {
		t.Fatalf("recovery lost verified repayment cursor: %+v", pending)
	}
	if err := s.reconcileRuntimeState(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(margin.repaid) != 1 || coordinator.active != 0 {
		t.Fatal("repeated recovery repaid twice or leaked wallet lease")
	}
}

func TestSpotShortRecoveryRechecksRuntimeOwnerAfterWalletLease(t *testing.T) {
	margin := &mockMarginExchange{}
	s, _ := spotShortRestoreFixture(t, &signalTestExchange{}, margin)
	gate := &execution.OpeningGate{}
	s.SetOpeningGate(gate)
	coordinator := &ownershipLossWalletLock{gate: gate}
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
	if err == nil || !strings.Contains(err.Error(), "runtime ownership is unverified") || len(margin.repaid) != 0 || s.pendingRepay[71] != original || coordinator.active != 0 || len(coordinator.acquired) != 1 {
		t.Fatalf("lost owner mutated recovery or leaked wallet lease: err=%v repayments=%v active=%d", err, margin.repaid, coordinator.active)
	}
}
