package main

import (
	"context"
	"errors"
	"fmt"

	"quantmesh/execution"
	"quantmesh/strategy"
)

const fundingCarryReconciliationBlock = "funding_carry_reconciliation_required"

func startFundingCarryManagedStrategy(ctx context.Context, manager *strategy.StrategyManager, fc *strategy.FundingCarryStrategy, gate *execution.OpeningGate) (bool, error) {
	if ctx == nil || manager == nil || fc == nil || gate == nil || manager.GetStrategy("funding_carry") != fc {
		return false, fmt.Errorf("funding_carry managed startup requires exact strategy and gate binding")
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	registered, err := manager.GetAllStrategiesContext(ctx)
	if err != nil {
		return false, fmt.Errorf("read funding_carry managed strategies: %w", err)
	}
	if len(registered) != 1 || registered["funding_carry"] != fc {
		return false, fmt.Errorf("funding_carry managed startup requires its exclusive strategy manager")
	}
	err = manager.StartAllContext(ctx)
	if err == nil {
		return false, nil
	}
	if !fundingCarryReconciliationOnlyError(err) {
		return false, err
	}
	if verifyErr := fc.VerifyRemainingReconciliation(ctx, gate); verifyErr != nil {
		return false, errors.Join(err, verifyErr)
	}
	gate.Block(fundingCarryReconciliationBlock)
	return true, nil
}

// A diagnostic joined with another failure is not managed-recovery admission.
// Permit only the precise recovery result under ordinary single-cause wrapping.
func fundingCarryReconciliationOnlyError(err error) bool {
	for err != nil {
		if _, ok := err.(*strategy.FundingCarryReconciliationRequiredError); ok {
			return true
		}
		wrapped, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = wrapped.Unwrap()
	}
	return false
}
