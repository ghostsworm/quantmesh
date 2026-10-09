package strategy

import (
	"context"
	"errors"
	"fmt"

	"quantmesh/storage"
)

// An authoritative flat snapshot is required before calling this checkpoint.
// Keep the debt and borrow identity: futures closure is not a complete close.
func (s *FundingCarryStrategy) checkpointReverseFuturesClosed(ctx context.Context, expected float64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.verifyDebtCommitLocked(ctx); err != nil {
		return err
	}
	if s.direction != DirectionReverse || !s.intentInFlight || !fundingCarryFinancialAmountsMatch(s.futQty, expected) {
		return fmt.Errorf("reverse futures close checkpoint no longer matches owned intent")
	}
	previous := s.futQty
	s.futQty = 0
	if err := s.persistRuntimeStateLocked(); err != nil {
		if errors.Is(err, storage.ErrFundingCarryRuntimeStateCommitConfirmedCanceled) {
			// The exact flat-futures quantity is durable; keep memory aligned
			// while returning cancellation so the caller cannot continue closing.
			s.runtimeStateErr = nil
			return fmt.Errorf("persist reverse futures close checkpoint after caller cancellation: %w", err)
		}
		s.futQty = previous
		s.unownedExposure, s.runtimeStateErr = true, err
		return fmt.Errorf("persist reverse futures close checkpoint: %w", err)
	}
	return nil
}
