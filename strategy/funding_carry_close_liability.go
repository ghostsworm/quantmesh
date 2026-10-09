package strategy

import (
	"context"
	"fmt"
	"math"

	"quantmesh/exchange"
)

// Liability is not an inventory/ownership proof. Closing still requires the
// owned principal ledger, exact cover fills, repayment source and zero remaining
// cover assets. An authoritative liability error must never fall back to a
// short-position view that can hide free/locked inventory or accrued interest.
func (s *FundingCarryStrategy) readMarginLiabilityForClose(ctx context.Context, requireFlat bool) (float64, float64, error) {
	if err := ctx.Err(); err != nil {
		return 0, 0, err
	}
	var principal, interest float64
	if reader, ok := s.marginEx.(exchange.MarginLiabilityReader); ok {
		var err error
		principal, interest, err = reader.GetMarginLiability(ctx, s.spot.GetBaseAsset())
		if err != nil {
			return 0, 0, fmt.Errorf("read independent margin liability: %w", err)
		}
	} else {
		positions, err := readScopedPositionSnapshot(ctx, s.marginEx, s.symbol)
		if err != nil {
			return 0, 0, fmt.Errorf("read legacy margin liability: %w", err)
		}
		for _, p := range positions {
			if p == nil || math.IsNaN(p.Size) || math.IsInf(p.Size, 0) {
				return 0, 0, fmt.Errorf("invalid legacy margin position snapshot")
			}
			if requireFlat && (p.Size < 0 || !p.MarginDebtKnown || !finiteNonNegative(p.MarginBorrowed) || !finiteNonNegative(p.MarginInterest) || p.MarginBorrowed > 0 || p.MarginInterest > 0) {
				return 0, 0, fmt.Errorf("margin repayment left unverified legacy debt")
			}
			if p.Size < 0 {
				if !p.MarginDebtKnown || !finiteNonNegative(p.MarginBorrowed) || !finiteNonNegative(p.MarginInterest) ||
					!fundingCarryFinancialAmountsMatch(math.Abs(p.Size), p.MarginBorrowed+p.MarginInterest) {
					return 0, 0, fmt.Errorf("legacy margin liability lacks consistent principal/interest breakdown")
				}
				principal += p.MarginBorrowed
				interest += p.MarginInterest
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return 0, 0, err
	}
	if !finiteNonNegative(principal) || !finiteNonNegative(interest) || !finiteNonNegative(principal+interest) {
		return 0, 0, fmt.Errorf("margin liability principal/interest is invalid")
	}
	if requireFlat && (principal > 0 || interest > 0) {
		return 0, 0, fmt.Errorf("margin repayment left positive principal or interest")
	}
	return principal, interest, nil
}
