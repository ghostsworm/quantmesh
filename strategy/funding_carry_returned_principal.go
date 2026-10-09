package strategy

import (
	"context"
	"errors"
	"fmt"
	"math/big"

	"quantmesh/storage"
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
	coverOrders, err := s.coverOrdersAfterRepaymentLocked(confirmed)
	if err != nil {
		return err
	}
	if replay, err := s.debtEventReplayLocked(confirmed); replay || err != nil {
		if err != nil {
			return err
		}
		if fundingCarryFinancialAmountsMatch(s.marginDebt, expectedRemaining) {
			if s.marginRepayIntent != nil && s.marginRepayIntent.CoverOrderID > 0 {
				previous := s.marginCoverOrders
				s.marginCoverOrders = coverOrders
				if err := s.persistRecoveryCheckpointLocked(ctx); err != nil {
					if errors.Is(err, storage.ErrFundingCarryRuntimeStateCommitConfirmedCanceled) {
						s.runtimeStateErr = nil
						return fmt.Errorf("persist replayed margin repayment cover state after caller cancellation: %w", err)
					}
					s.marginCoverOrders = previous
					s.unownedExposure, s.runtimeStateErr = true, err
					return err
				}
			}
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
	previousCoverOrders := s.marginCoverOrders
	s.marginCoverOrders = coverOrders
	s.marginDebtEvents = append(s.marginDebtEvents, confirmed)
	s.marginDebt = remaining
	mismatch := !fundingCarryFinancialAmountsMatch(remaining, expectedRemaining)
	if mismatch {
		s.unownedExposure = true
	}
	if err := s.persistRecoveryCheckpointLocked(ctx); err != nil {
		if errors.Is(err, storage.ErrFundingCarryRuntimeStateCommitConfirmedCanceled) {
			// The fenced store confirms this exact checkpoint. Preserve matching
			// in-memory principal and event state, but propagate cancellation so
			// the caller cannot continue the current financial operation.
			return fmt.Errorf("persist returned margin principal after caller cancellation: %w", err)
		}
		s.marginDebtEvents = s.marginDebtEvents[:len(s.marginDebtEvents)-1]
		s.marginDebt = previousDebt
		s.marginCoverOrders = previousCoverOrders
		s.unownedExposure, s.runtimeStateErr = true, err
		return fmt.Errorf("persist returned margin principal: %w", err)
	}
	if mismatch {
		return fmt.Errorf("margin repayment left unmatched principal %.12g; outcome requires reconciliation", remaining)
	}
	return nil
}
