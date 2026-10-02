package strategy

import (
	"context"
	"fmt"
)

// Retain the accepted ID before verification. It is recovery evidence, not a
// verified debt event; the existing durable in-flight intent must remain set.
func (s *FundingCarryStrategy) recordMarginBorrowAcknowledgement(ctx context.Context, transferID int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.verifyDebtCommitLocked(ctx); err != nil {
		return err
	}
	if transferID <= 0 || !s.intentInFlight {
		return fmt.Errorf("margin borrow acknowledgement requires positive identity and durable intent")
	}
	s.marginBorrowTransferID = transferID
	if err := s.persistRuntimeStateLocked(); err != nil {
		// Do not discard an actual ACK from memory when its save is uncertain.
		s.unownedExposure, s.runtimeStateErr = true, err
		return fmt.Errorf("persist margin borrow acknowledgement: %w", err)
	}
	return nil
}
