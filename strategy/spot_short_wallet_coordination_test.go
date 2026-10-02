package strategy

import (
	"context"
	"errors"
	"math"
	"testing"

	"quantmesh/exchange"
	"quantmesh/position"
)

func TestSpotShortRecoveryRepaysWithAccountWalletLease(t *testing.T) {
	for _, startup := range []bool{true, false} {
		name := "runtime"
		if startup {
			name = "startup"
		}
		t.Run(name, func(t *testing.T) {
			venue := &spotShortReconcileExchange{
				order: &exchange.Order{OrderID: 71, Symbol: "BTCUSDT", Side: exchange.SideBuy, Status: exchange.OrderStatusPartiallyFilled, Quantity: 1, ExecutedQty: 0.5},
				fills: []*exchange.OrderFill{{OrderID: 71, TradeID: "trade-1", Symbol: "BTCUSDT", Side: exchange.SideBuy, Price: 100, Quantity: 0.5, Commission: 0.001, CommissionAsset: "BTC", BaseFeeQty: 0.001}},
			}
			margin := &mockMarginExchange{}
			s, _ := spotShortRestoreFixture(t, venue, margin)
			coordinator := &walletCoordinationTestLock{}
			key := "funding_carry_wallet:" + t.Name()
			if err := s.SetAccountWalletCoordinationLock(coordinator, key); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), spotShortRuntimeReconcileTimeout)
			defer cancel()
			if startup {
				if err := s.Start(ctx); err != nil {
					t.Fatal(err)
				}
				defer s.Stop()
			} else {
				s.mu.Lock()
				err := s.restoreRuntimeStateLocked()
				s.mu.Unlock()
				if err != nil {
					t.Fatal(err)
				}
				if err := s.reconcileRuntimeState(ctx); err != nil {
					t.Fatal(err)
				}
			}
			if len(margin.repaid) != 1 || math.Abs(margin.repaid[0]-0.499) > 1e-12 {
				t.Fatalf("expected one repayment net of base fees: %v", margin.repaid)
			}
			if len(coordinator.acquired) != 1 || coordinator.acquired[0] != key {
				t.Fatalf("recovery must hold exactly one account lease: %v", coordinator.acquired)
			}
			if pending := s.pendingRepay[71]; pending.RepayUncertain || pending.ExecutedQty != 0.5 || pending.BaseFeeQty != 0.001 {
				t.Fatalf("repayment did not advance the verified cursor: %+v", pending)
			}
			if err := s.reconcileRuntimeState(ctx); err != nil {
				t.Fatal(err)
			}
			if len(margin.repaid) != 1 {
				t.Fatalf("duplicate recovery repeated repayment: %v", margin.repaid)
			}
		})
	}
}

func TestSpotShortWalletLockFailureDoesNotCreateUncertainRepayment(t *testing.T) {
	margin := &mockMarginExchange{}
	s := newSpotShortForTest(&signalTestExecutor{}, &signalTestExchange{}, margin)
	original := spotShortPendingRepay{OrderQuantity: 1}
	s.pendingRepay[71] = original
	if err := s.SetAccountWalletCoordinationLock(&walletCoordinationTestLock{lockErr: context.DeadlineExceeded}, "funding_carry_wallet:"+t.Name()); err != nil {
		t.Fatal(err)
	}
	err := s.OnOrderUpdate(&position.OrderUpdate{OrderID: 71, Side: "BUY", Status: "PARTIALLY_FILLED", ExecutedQty: 0.5})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected acquisition failure: %v", err)
	}
	if s.pendingRepay[71] != original || len(margin.repaid) != 0 {
		t.Fatalf("failed lock acquisition changed repayment state: %+v repayments=%v", s.pendingRepay[71], margin.repaid)
	}
}

func TestSpotShortStartupRefusesRecoveryWithoutWalletLease(t *testing.T) {
	s, _ := spotShortRestoreFixture(t, &signalTestExchange{}, &mockMarginExchange{})
	if err := s.SetAccountWalletCoordinationLock(&walletCoordinationTestLock{lockErr: context.Canceled}, "funding_carry_wallet:"+t.Name()); err != nil {
		t.Fatal(err)
	}
	if err := s.Start(context.Background()); !errors.Is(err, context.Canceled) {
		t.Fatalf("startup bypassed the account lease: %v", err)
	}
}
