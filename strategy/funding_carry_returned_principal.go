package strategy

import (
	"context"
	"fmt"
	"math/big"
)

// Commit returned principal and its evidence together, never using total repayment.
func (s *FundingCarryStrategy) returnBorrowedPrincipal(ctx context.Context, transferID int64, asset string, amount, expectedRemaining float64) error {
	confirmed, err := s.confirmMarginDebtTransaction(ctx, "repay", transferID, asset, amount)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.verifyDebtCommitLocked(ctx); err != nil {
		return err
	}
	if !validRuntimeAmount(s.marginDebt) || !validRuntimeAmount(expectedRemaining) {
		return fmt.Errorf("margin principal state is invalid")
	}
	if replay, err := s.debtEventReplayLocked(confirmed); replay || err != nil {
		if err != nil {
			return err
		}
		if fundingCarryFinancialAmountsMatch(s.marginDebt, expectedRemaining) {
			return nil
		}
		return fmt.Errorf("replayed margin repayment requires principal state reconciliation")
	}
	returned := new(big.Rat).Sub(fundingCarryDecimalPrincipal(s.marginDebt), fundingCarryDecimalPrincipal(confirmed.Principal))
	if returned.Sign() < 0 {
		if !fundingCarryFinancialAmountsMatch(s.marginDebt, confirmed.Principal) {
			return fmt.Errorf("returned margin principal exceeds owned debt")
		}
		returned.SetInt64(0)
	}
	remaining, _ := returned.Float64()
	previousDebt := s.marginDebt
	s.marginDebtEvents = append(s.marginDebtEvents, confirmed)
	s.marginDebt = remaining
	mismatch := !fundingCarryFinancialAmountsMatch(remaining, expectedRemaining)
	if mismatch {
		s.unownedExposure = true
	}
	if err := s.persistRuntimeStateLocked(); err != nil {
		s.marginDebtEvents = s.marginDebtEvents[:len(s.marginDebtEvents)-1]
		s.marginDebt = previousDebt
		s.unownedExposure, s.runtimeStateErr = true, err
		return fmt.Errorf("persist returned margin principal: %w", err)
	}
	if mismatch {
		return fmt.Errorf("margin repayment left unmatched principal %.12g; outcome requires reconciliation", remaining)
	}
	return nil
}
