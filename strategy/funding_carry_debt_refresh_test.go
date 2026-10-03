package strategy

import (
	"context"
	"errors"
	"testing"

	"quantmesh/exchange"
	"quantmesh/execution"
)

type fundingCarryDebtRefreshVenue struct {
	*fundingCarryRepayIntentExchange
	afterFills func()
}

func (m *mockFCExchange) GetMarginRepaymentFunds(ctx context.Context, asset string) (float64, float64, float64, error) {
	principal, interest, err := m.GetMarginLiability(ctx, asset)
	// This fixture models a completed buy crediting free base assets.
	return principal, interest, m.getOrderExecQty, err
}

func (m *mockFCExchange) GetMarginLiability(ctx context.Context, asset string) (float64, float64, error) {
	positions, err := m.GetPositions(ctx, "BTCUSDT")
	if err != nil {
		return 0, 0, err
	}
	if positions == nil {
		return 0, 0, errors.New("missing fixture liability snapshot")
	}
	var principal, interest float64
	for _, p := range positions {
		if p == nil || !p.MarginDebtKnown {
			return 0, 0, errors.New("missing fixture principal/interest")
		}
		principal += p.MarginBorrowed
		interest += p.MarginInterest
	}
	return principal, interest, nil
}

func (v *fundingCarryDebtRefreshVenue) GetOrderFills(ctx context.Context, symbol string, id int64) ([]*exchange.OrderFill, error) {
	fills, err := v.mockFCExchange.GetOrderFills(ctx, symbol, id)
	if v.afterFills != nil {
		v.afterFills()
	}
	return fills, err
}

func TestFundingCarryCloseRefreshesDebtAfterBuyback(t *testing.T) {
	for _, mode := range []string{"interest_growth", "shortfall", "principal_changed", "query_error", "missing_breakdown", "cancelled", "owner_lost"} {
		t.Run(mode, func(t *testing.T) {
			s, margin, _ := newFundingCarryRepayIntentFixture()
			margin.queryErr, margin.getOrderExecQty = nil, 0.4008
			s.spot.(*mockFCExchange).quantityDecimals = 4
			venue := &fundingCarryDebtRefreshVenue{fundingCarryRepayIntentExchange: margin}
			s.marginEx = venue
			gate := &execution.OpeningGate{}
			s.SetOpeningGate(gate)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			venue.afterFills = func() {
				p := margin.positions[0]
				p.Size, p.MarginInterest = -0.4002, 0.0002
				switch mode {
				case "shortfall":
					p.Size, p.MarginInterest = -0.402, 0.002
				case "principal_changed":
					p.Size, p.MarginBorrowed = -0.5002, 0.5
				case "query_error":
					margin.positionsErr = errors.New("injected refreshed debt query failure")
				case "missing_breakdown":
					p.MarginDebtKnown = false
				case "cancelled":
					cancel()
				case "owner_lost":
					gate.Block(strategyWalletRuntimeOwnershipBlock)
				}
			}
			err := s.closeReverse(ctx, "debt_changed_during_cover")
			if mode == "interest_growth" {
				if err == nil || !s.intentInFlight || !s.unownedExposure || margin.repayCalls != 1 || margin.repayAmount != 0.4002 || s.marginCoverOrders[0].Consumed != 0.4002 || s.marginCoverOrders[0].DebtToCover != 0.4 {
					t.Fatalf("stale debt amount used after cover: amount=%v error=%v", margin.repayAmount, err)
				}
			} else if err == nil || margin.repayCalls != 0 || s.marginDebt != 0.4 || !s.unownedExposure {
				t.Fatal("invalid refreshed debt reached repayment or cleared ownership")
			}
		})
	}
}
