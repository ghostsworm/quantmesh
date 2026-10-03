package strategy

import (
	"context"
	"fmt"

	"quantmesh/execution"
)

// Only produced after complete historical accounting import and successful
// wallet cleanup. It is not proof of current inventory or permission to trade.
type FundingCarryReconciliationRequiredError struct{ message string }

func (e *FundingCarryReconciliationRequiredError) Error() string { return e.message }

func (s *FundingCarryStrategy) IsRunning() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.started && s.ctx != nil && s.ctx.Err() == nil
}

func (s *FundingCarryStrategy) VerifyRemainingReconciliation(ctx context.Context, gate *execution.OpeningGate) error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if err := s.verifyDebtCommitLocked(ctx); err != nil {
		return err
	}
	if gate == nil || s.openingGate != gate || s.started || !s.strategySpotKnown || !s.unownedExposure || !s.intentInFlight ||
		s.runtimeStateErr != nil || s.marginRepayIntent != nil || s.marginCoverIntent != nil || s.marginAccountScope == "" {
		return fmt.Errorf("funding_carry is not a verified stopped remaining-asset reconciliation")
	}
	if err := validateFundingCarryDebtPrincipalBalance(s.runtimeStateSnapshotLocked()); err != nil {
		return err
	}
	remaining, err := fundingCarryCoverRemaining(s.coverRemainingStateLocked(), s.spot.GetBaseAsset())
	if err != nil {
		return err
	}
	if remaining.Sign() <= 0 {
		return fmt.Errorf("funding_carry has no verified remaining-asset reconciliation")
	}
	return nil
}
