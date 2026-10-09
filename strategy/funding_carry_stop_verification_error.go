package strategy

import "fmt"

// This private type is a retry candidate, not proof of flatness. Only the
// stopped strategy may create it; callers must run the complete read-only
// verifier before clearing a cached stop or releasing any reservation.
type fundingCarryFinalVerificationPendingError struct{ cause error }

func (e *fundingCarryFinalVerificationPendingError) Error() string {
	return fmt.Sprintf("funding_carry stopped close requires final read-only verification: %v", e.cause)
}

func (e *fundingCarryFinalVerificationPendingError) Unwrap() error { return e.cause }

// IsFundingCarryFinalVerificationPending rejects mixed joined failures.
// errors.As alone would also match a prepare/ownership failure joined with a
// final-verification candidate, allowing unrelated failures to be discarded.
func IsFundingCarryFinalVerificationPending(err error) bool {
	if err == nil {
		return false
	}
	if pending, ok := err.(*fundingCarryFinalVerificationPendingError); ok {
		return pending != nil && pending.cause != nil
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		found := false
		for _, child := range joined.Unwrap() {
			if child == nil {
				continue
			}
			if !IsFundingCarryFinalVerificationPending(child) {
				return false
			}
			found = true
		}
		return found
	}
	if wrapped, ok := err.(interface{ Unwrap() error }); ok {
		return IsFundingCarryFinalVerificationPending(wrapped.Unwrap())
	}
	return false
}

// Caller holds the operation token and stopMu, after the financial attempt
// returned. A marker without the completed local ledger is not retryable.
func (s *FundingCarryStrategy) classifyStoppedMarginCloseError(err error) error {
	if err == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !s.marginCloseVerificationPending || s.executionRecoveryRequired ||
		!s.strategySpotKnown || !s.intentInFlight || s.marginAccountScope == "" ||
		s.direction != DirectionReverse || s.marginDebt != 0 || s.futQty != 0 || s.strategySpotQty != 0 ||
		s.marginRepayIntent != nil || s.marginCoverIntent != nil || s.marginBorrowTransferID <= 0 ||
		s.marginBorrowedAt.IsZero() || len(s.marginDebtEvents) < 2 || len(s.marginCoverOrders) == 0 {
		return err
	}
	state := s.runtimeStateSnapshotLocked()
	lastBorrowMatches := false
	for _, event := range state.MarginDebtEvents {
		if event.Action == "borrow" {
			lastBorrowMatches = event.TransferID == s.marginBorrowTransferID && event.OccurredAt.Equal(s.marginBorrowedAt)
		}
	}
	if !lastBorrowMatches {
		return err
	}
	if validateFundingCarryDebtPrincipalBalance(state) != nil ||
		validateFundingCarryDebtAsset(state, s.spot.GetBaseAsset()) != nil ||
		requireNoFundingCarryCoverRemaining(state, s.spot.GetBaseAsset()) != nil {
		return err
	}
	return &fundingCarryFinalVerificationPendingError{cause: err}
}
