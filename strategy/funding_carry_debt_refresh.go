package strategy

import (
	"context"
	"fmt"

	"quantmesh/exchange"
)

// Called under the operation and account wallet coordination, after cover fills.
func (s *FundingCarryStrategy) currentMarginRepaymentAmount(ctx context.Context, asset string, coverID int64) (float64, error) {
	reader, ok := s.marginEx.(exchange.MarginLiabilityReader)
	if !ok {
		return 0, fmt.Errorf("margin venue cannot read liabilities independently of inventory")
	}
	principal, interest, err := reader.GetMarginLiability(ctx, asset)
	if err != nil {
		return 0, fmt.Errorf("refresh margin principal and interest: %w", err)
	}
	amount := principal + interest
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.verifyDebtCommitLocked(ctx); err != nil {
		return 0, err
	}
	if !validRuntimeAmount(principal) || !validRuntimeAmount(interest) || !validRuntimeAmount(amount) || amount <= 0 || !fundingCarryFinancialAmountsMatch(principal, s.marginDebt) {
		return 0, fmt.Errorf("refreshed margin debt does not match owned principal")
	}
	if err := validateFundingCarryCoverSource(s.marginCoverOrders, &fundingCarryRepayIntent{Asset: asset, AccountScope: s.marginAccountScope, Amount: amount, CoverOrderID: coverID}); err != nil {
		return 0, err
	}
	return amount, nil
}
