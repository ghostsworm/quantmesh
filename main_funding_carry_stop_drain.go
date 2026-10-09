package main

import (
	"context"
	"errors"
	"fmt"

	"quantmesh/order"
	"quantmesh/strategy"
)

type runtimeStopDrainRetryError struct{ cause error }

var errFundingCarryDrainOwnershipLost = errors.New("funding_carry runtime ownership lease lost during submission drain; financial cancellation withheld")

func fundingCarryStopDrainOwnershipGuard(leases []*runtimeOwnershipLease) error {
	if fundingCarryRuntimeOwnershipLeaseLost(leases) {
		return errFundingCarryDrainOwnershipLost
	}
	return nil
}

func (e *runtimeStopDrainRetryError) Error() string {
	return fmt.Sprintf("runtime stop requires submission drain retry: %v", e.cause)
}
func (e *runtimeStopDrainRetryError) Unwrap() error { return e.cause }

func isRetryableRuntimeStopDrain(err error) bool {
	var pending *runtimeStopDrainRetryError
	return errors.As(err, &pending)
}

// Only this non-financial boundary may emit a drain-retry error. In-flight
// operations may already have financial outcomes; draining does not certify
// those outcomes, clear UNKNOWN or authorize closing/releasing capital.
func drainFundingCarryStop(ctx context.Context, carry *strategy.FundingCarryStrategy, executors []*order.ExchangeOrderExecutor, ownershipGuard func() error) error {
	checkOwnership := func() error {
		if ownershipGuard != nil {
			return ownershipGuard()
		}
		return nil
	}
	if err := checkOwnership(); err != nil {
		return err
	}
	waitFailure := func(err error) error {
		// Ownership loss supersedes a benign wait timeout. Never classify
		// this as a retryable drain or proceed to financial cancellation.
		if ownershipErr := checkOwnership(); ownershipErr != nil {
			return errors.Join(ownershipErr, err)
		}
		return &runtimeStopDrainRetryError{cause: err}
	}
	for _, executor := range executors {
		executor.BeginShutdown()
	}
	if err := carry.QuiesceContext(ctx); err != nil {
		return waitFailure(err)
	}
	for _, executor := range executors {
		if err := executor.DrainShutdown(ctx); err != nil {
			return waitFailure(err)
		}
	}
	return checkOwnership()
}
