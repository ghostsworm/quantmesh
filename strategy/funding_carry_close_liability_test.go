package strategy

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"quantmesh/exchange"
	"quantmesh/execution"
)

type fundingCarryCloseLiabilityVenue struct {
	*fundingCarryRepayIntentExchange
	principal, interest float64
	queryErr            error
	afterRead           func()
	reads               int
	residual            float64
	unknownOrders       bool
}

func (v *fundingCarryCloseLiabilityVenue) GetAccountOpenOrders(ctx context.Context) ([]*exchange.Order, error) {
	if v.unknownOrders {
		return nil, nil
	}
	return v.fundingCarryRepayIntentExchange.GetAccountOpenOrders(ctx)
}

func TestFundingCarryCloseLegacyLiabilityProofRemainsStrict(t *testing.T) {
	for _, mode := range []string{"valid", "flat", "nil_snapshot", "wrong_symbol", "unknown_breakdown", "inconsistent_total", "negative_interest", "invalid_size", "overflow", "flat_micro_principal", "flat_micro_interest", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			s, margin, _ := newFundingCarryRepayIntentFixture()
			s.marginEx = &fundingCarryLegacyDebtVenue{margin.mockFCExchange}
			flat := mode == "flat" || mode == "flat_micro_principal" || mode == "flat_micro_interest"
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			p := margin.positions[0]
			switch mode {
			case "flat":
				p.Size, p.MarginBorrowed = 0, 0
			case "flat_micro_principal":
				p.Size, p.MarginBorrowed = 0, 0.00001
			case "flat_micro_interest":
				p.Size, p.MarginBorrowed, p.MarginInterest = 0, 0, 0.00001
			case "nil_snapshot":
				margin.returnNilPositions = true
			case "wrong_symbol":
				p.Symbol = "ETHUSDT"
			case "unknown_breakdown":
				p.MarginDebtKnown = false
			case "inconsistent_total":
				p.Size = -0.40001
			case "negative_interest":
				p.MarginInterest = -0.00001
			case "invalid_size":
				p.Size = math.NaN()
			case "overflow":
				p.Size, p.MarginBorrowed = -math.MaxFloat64, math.MaxFloat64
				margin.positions = []*exchange.Position{p, p}
			case "cancelled":
				cancel()
			}
			principal, interest, err := s.readMarginLiabilityForClose(ctx, flat)
			valid := mode == "valid" || mode == "flat"
			if (err == nil) != valid {
				t.Fatalf("legacy liability proof changed: %s err=%v", mode, err)
			}
			if mode == "valid" && (principal != 0.4 || interest != 0) || mode == "flat" && (principal != 0 || interest != 0) {
				t.Fatal("legacy principal/interest incorrectly reconstructed")
			}
			if margin.repayCalls != 0 || len(margin.placedOrders) != 0 || s.marginDebt != 0.4 {
				t.Fatal("liability proof made a financial mutation")
			}
		})
	}
}

func (v *fundingCarryCloseLiabilityVenue) GetMarginLiability(_ context.Context, asset string) (float64, float64, error) {
	v.reads++
	if asset != "BTC" {
		return 0, 0, errors.New("wrong close liability asset")
	}
	if v.afterRead != nil {
		v.afterRead()
	}
	if v.repayCalls > 0 {
		return v.residual, v.interest, v.queryErr
	}
	return v.principal, v.interest, v.queryErr
}

func (v *fundingCarryCloseLiabilityVenue) GetMarginRepaymentFunds(_ context.Context, asset string) (float64, float64, float64, error) {
	if asset != "BTC" {
		return 0, 0, 0, errors.New("wrong repayment asset")
	}
	return v.principal, v.interest, v.getOrderExecQty, v.queryErr
}

func TestFundingCarryCloseUsesIndependentLiabilityBeforeAndAfterRepayment(t *testing.T) {
	for _, mode := range []string{"valid", "external_principal", "micro_principal", "negative", "nan", "overflow", "query_error", "cancelled", "owner_lost", "residual_debt", "residual_interest", "post_query_error", "post_cancelled", "post_owner_lost"} {
		t.Run(mode, func(t *testing.T) {
			s, margin, store := newFundingCarryRepayIntentFixture()
			s.strategySpotKnown = true
			s.marginBorrowedAt = time.UnixMilli(1000).UTC()
			s.marginDebtEvents = []fundingCarryMarginDebtEvent{{Action: "borrow", TransferID: 42, Asset: "BTC", Amount: 0.4, Principal: 0.4, OccurredAt: s.marginBorrowedAt, AccountScope: "scope-a"}}
			margin.queryErr = nil
			margin.positionsErr = errors.New("free base inventory prevents short-position attribution")
			venue := &fundingCarryCloseLiabilityVenue{fundingCarryRepayIntentExchange: margin, principal: 0.4}
			s.marginEx = venue
			gate := &execution.OpeningGate{}
			s.SetOpeningGate(gate)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			switch mode {
			case "external_principal":
				venue.principal = 0.5
			case "micro_principal":
				venue.principal = 0.40001
			case "negative":
				venue.interest = -1
			case "nan":
				venue.principal = math.NaN()
			case "overflow":
				venue.principal, venue.interest = math.MaxFloat64, math.MaxFloat64
			case "query_error":
				venue.queryErr = errors.New("independent liability unavailable")
			case "cancelled":
				venue.afterRead = cancel
			case "owner_lost":
				venue.afterRead = func() { gate.Block(strategyWalletRuntimeOwnershipBlock) }
			case "residual_debt":
				venue.residual = 0.00001
			case "residual_interest", "post_query_error", "post_cancelled", "post_owner_lost":
				venue.afterRead = func() {
					if margin.repayCalls == 0 {
						return
					}
					if mode == "post_query_error" {
						venue.queryErr = errors.New("post-repayment debt query failed")
					} else if mode == "post_cancelled" {
						cancel()
					} else if mode == "post_owner_lost" {
						gate.Block(strategyWalletRuntimeOwnershipBlock)
					} else {
						venue.interest = 0.00001
					}
				}
			}
			err := s.closeReverse(ctx, "independent_liability")
			if venue.reads == 0 {
				t.Fatal("protective close bypassed independent liability capability")
			}
			if mode == "valid" {
				if err != nil || venue.reads != 2 || margin.repayCalls != 1 || len(margin.placedOrders) != 1 || s.marginDebt != 0 || s.direction != DirectionNone || s.intentInFlight || s.unownedExposure {
					t.Fatalf("confirmed owned close did not complete: reads=%d repay=%d err=%v", venue.reads, margin.repayCalls, err)
				}
				if s.strategySpotQty != 0 || len(s.marginCoverOrders) != 1 || s.marginCoverOrders[0].Consumed != 0.4 || s.marginCoverOrders[0].Net != 0.4 || store.payload == "" {
					t.Fatal("close adopted external inventory or lost own fill/repayment evidence")
				}
				state, decodeErr := decodeFundingCarryRuntimeState(store.version, store.payload, s.fut.GetName(), s.spot.GetName(), s.symbol)
				if decodeErr != nil || state.Direction != DirectionNone || state.MarginDebt != 0 || len(state.MarginDebtEvents) != 2 {
					t.Fatalf("confirmed close lacks restart-valid debt and cover ledger: %v", decodeErr)
				}
				return
			}
			if err == nil || (!s.unownedExposure && mode != "owner_lost") {
				t.Fatal("invalid liability proof cleared uncertainty")
			}
			if mode == "residual_debt" || mode == "residual_interest" || mode == "post_query_error" || mode == "post_cancelled" || mode == "post_owner_lost" {
				if venue.reads != 2 || margin.repayCalls != 1 || s.direction != DirectionReverse || s.marginDebt != 0 || len(s.marginDebtEvents) != 2 {
					t.Fatal("remaining debt proof lost accepted repayment or falsely completed close")
				}
			} else if margin.repayCalls != 0 || len(margin.placedOrders) != 0 || s.marginDebt != 0.4 {
				t.Fatal("invalid pre-close debt proof reached a financial mutation")
			}
			if mode == "owner_lost" && (!gate.Blocked() || store.payload != "") {
				t.Fatal("ownership failure wrote durable state or lost its opening hold")
			}
		})
	}
}
