package strategy

import (
	"context"
	"fmt"
	"math"
)

// Commit returned principal and its evidence together, never using total repayment.
func (s *FundingCarryStrategy) returnBorrowedPrincipal(ctx context.Context, transferID int64, asset string, amount, expectedRemaining float64) error {
	confirmed, err := s.confirmMarginDebtTransaction(ctx, "repay", transferID, asset, amount)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tolerance := math.Max(1e-10, s.marginDebt*1e-8)
	if !validRuntimeAmount(s.marginDebt) || !validRuntimeAmount(expectedRemaining) {
		return fmt.Errorf("margin principal state is invalid")
	}
	if replay, err := s.debtEventReplayLocked(confirmed); replay || err != nil {
		if err != nil {
			return err
		}
		if math.Abs(s.marginDebt-expectedRemaining) <= tolerance {
			return nil
		}
		return fmt.Errorf("replayed margin repayment requires principal state reconciliation")
	}
	remaining := s.marginDebt - confirmed.Principal
	if !validRuntimeAmount(remaining) && remaining < -tolerance {
		return fmt.Errorf("returned margin principal exceeds owned debt")
	}
	remaining = math.Max(0, remaining)
	previousDebt := s.marginDebt
	s.marginDebtEvents = append(s.marginDebtEvents, confirmed)
	s.marginDebt = remaining
	mismatch := math.Abs(remaining-expectedRemaining) > tolerance
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
