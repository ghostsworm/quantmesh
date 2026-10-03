package strategy

import (
	"context"
	"errors"
	"math"
	"testing"

	"quantmesh/execution"
)

type fundingCarryFundsVenue struct {
	*fundingCarryRepayIntentExchange
	available float64
	fundsErr  error
	afterRead func()
}

func (v *fundingCarryFundsVenue) GetMarginRepaymentFunds(ctx context.Context, asset string) (float64, float64, float64, error) {
	principal, interest, err := v.mockFCExchange.GetMarginLiability(ctx, asset)
	if v.afterRead != nil {
		v.afterRead()
	}
	return principal, interest, v.available, errors.Join(err, v.fundsErr)
}

func TestFundingCarryRepaymentRequiresCurrentFreeMarginFunds(t *testing.T) {
	for _, mode := range []string{"available", "spent", "frozen", "nan", "negative", "query_error", "cancelled", "owner_lost", "save_failure"} {
		t.Run(mode, func(t *testing.T) {
			s, margin, store := newFundingCarryRepayIntentFixture()
			margin.queryErr = nil
			venue := &fundingCarryFundsVenue{fundingCarryRepayIntentExchange: margin, available: 0.4}
			s.marginEx = venue
			gate := &execution.OpeningGate{}
			s.SetOpeningGate(gate)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch mode {
			case "spent":
				venue.available = 0.39
			case "frozen":
				venue.available = 0.1
			case "nan":
				venue.available = math.NaN()
			case "negative":
				venue.available = -1
			case "query_error":
				venue.fundsErr = errors.New("injected repayment funds query failure")
			case "cancelled":
				venue.afterRead = cancel
			case "owner_lost":
				venue.afterRead = func() { gate.Block(strategyWalletRuntimeOwnershipBlock) }
			case "save_failure":
				venue.afterRead = func() { store.err = errors.New("injected repayment request save failure") }
			}
			err := s.closeReverse(ctx, "current_margin_free_funds")
			if mode == "available" {
				if err != nil || margin.repayCalls != 1 || s.marginCoverOrders[0].Consumed != 0.4 {
					t.Fatalf("available owned cover cannot repay: %v", err)
				}
			} else if err == nil || margin.repayCalls != 0 || s.marginDebt != 0.4 || !s.unownedExposure {
				t.Fatal("invalid/unavailable free funds reached repayment")
			}
			if len(s.marginCoverOrders) != 1 || !s.marginCoverOrders[0].Verified {
				t.Fatal("historical fill proof lost while checking physical funds")
			}
		})
	}
}
