package main

import (
	"errors"
	"fmt"
	"sync"
)

type runtimeOwnershipReleaseRetryError struct{ cause error }

func (e *runtimeOwnershipReleaseRetryError) Error() string {
	return fmt.Sprintf("verified financial stop requires ownership release retry: %v", e.cause)
}
func (e *runtimeOwnershipReleaseRetryError) Unwrap() error { return e.cause }

func isRetryableRuntimeOwnershipRelease(err error) bool {
	var retry *runtimeOwnershipReleaseRetryError
	return errors.As(err, &retry)
}

func newStandardRuntimeStop(rt *SymbolRuntime, lease *runtimeOwnershipLease, stop func() error) func() error {
	return newRuntimeStopWithRetryableRelease(stop, func() error {
		released, err := releaseRuntimeOwnershipLeaseAfterVerifiedStop(lease, nil, rt.shutdownCloseUnverifiedReason())
		if err != nil {
			if lease != nil && !lease.Lost() && rt.shutdownCloseUnverifiedReason() == "" {
				return &runtimeOwnershipReleaseRetryError{cause: err}
			}
			return err
		}
		if !released {
			return fmt.Errorf("runtime ownership remains unreleased")
		}
		return nil
	})
}

// Financial UNKNOWN is not replayable. Once the financial phase succeeded,
// however, an ownership-checked lease release can retry without repeating
// cancellation, close orders, ledger mutations or runtime cleanup.
func newRuntimeStopWithRetryableRelease(stop, release func() error) func() error {
	var mu sync.Mutex
	var stopped, completed bool
	var stopErr error
	return func() error {
		mu.Lock()
		defer mu.Unlock()
		if completed {
			return nil
		}
		if !stopped {
			stopErr = stop()
			stopped = true
		}
		if stopErr != nil {
			return stopErr
		}
		if err := release(); err != nil {
			return err
		}
		completed = true
		return nil
	}
}
