package strategy

import (
	"context"
	"errors"

	"quantmesh/execution"
)

// Wallet mutations used to fund a new position must observe the same opening
// barrier as orders. Repayment and protective closing do not use this check.
func (s *FundingCarryStrategy) verifyWalletOpeningAdmission(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.RLock()
	gate := s.openingGate
	s.mu.RUnlock()
	if gate != nil && gate.Blocked() {
		return execution.ErrOpeningPaused
	}
	return nil
}

// Caller has just saved a fresh intent and has not yet sent its financial RPC.
// A successful no-op completion clears only that intent. Persistence/ownership
// failure retains the existing fail-closed semantics; no unknown RPC is replayed.
func (s *FundingCarryStrategy) verifyUnsubmittedWalletOpening(ctx context.Context) error {
	if err := s.verifyWalletOpeningAdmission(ctx); err != nil {
		return errors.Join(err, s.finishRuntimeIntent(ctx, true))
	}
	return nil
}
