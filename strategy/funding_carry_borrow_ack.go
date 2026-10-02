package strategy

import (
	"context"
	"errors"
	"fmt"
)

// Retain the accepted ID before verification. It is recovery evidence, not a
// verified debt event; the existing durable in-flight intent must remain set.
func (s *FundingCarryStrategy) recordMarginBorrowAcknowledgement(ctx context.Context, transferID int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if transferID <= 0 || !s.intentInFlight {
		return fmt.Errorf("margin borrow acknowledgement requires positive identity and durable intent")
	}
	s.marginBorrowTransferID = transferID
	// The exchange ACK remains evidence even when the operation has expired.
	// Never let a known stale owner write it over the new owner's snapshot.
	interrupted := s.verifyDebtCommitLocked(ctx)
	if interrupted != nil {
		s.unownedExposure = true
	}
	if ownerErr := verifyStrategyWalletRuntimeOwner(s.openingGate); ownerErr != nil {
		return errors.Join(interrupted, ownerErr)
	}
	// Saving recovery metadata is not a financial commit or a renewed lease.
	if err := s.persistRuntimeStateLocked(); err != nil {
		// Do not discard an actual ACK from memory when its save is uncertain.
		s.unownedExposure, s.runtimeStateErr = true, err
		return errors.Join(interrupted, fmt.Errorf("persist margin borrow acknowledgement: %w", err))
	}
	return interrupted
}
