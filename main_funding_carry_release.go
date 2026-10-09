package main

import (
	"errors"
	"fmt"
	"sync"
)

type runtimeCapitalReleaseRetryError struct{ cause error }

func (e *runtimeCapitalReleaseRetryError) Error() string {
	return fmt.Sprintf("stopped funding_carry requires independent capital verification/release retry: %v", e.cause)
}
func (e *runtimeCapitalReleaseRetryError) Unwrap() error { return e.cause }

// Called only after the cached financial stop succeeded. Once capital was
// verified and released, never query or mutate scopes already handed to peers.
// Remaining leases retain renewal on failure; startup rollback keeps Release.
func newFundingCarryVerifiedRelease(rt *SymbolRuntime, leases []*runtimeOwnershipLease, releaseCapital func() error) func() error {
	var mu sync.Mutex
	var capitalReleased, completed bool
	var capitalFailure *string
	guard := func() error {
		if rt == nil || len(leases) == 0 || releaseCapital == nil {
			return fmt.Errorf("funding_carry verified release requires runtime, leases and capital verifier")
		}
		if rt.shutdownCloseUnverified.Load() != capitalFailure || fundingCarryRuntimeOwnershipLeaseLost(leases) {
			return fmt.Errorf("funding_carry ownership release requires verified financial state and retained ownership")
		}
		return nil
	}
	return func() error {
		mu.Lock()
		defer mu.Unlock()
		if completed {
			return nil
		}
		if err := guard(); err != nil {
			return err
		}
		if !capitalReleased {
			if err := releaseCapital(); err != nil {
				// Only this release phase may own a retry marker. Never overwrite
				// an unrelated financial failure or retain a candidate after loss.
				if guardErr := guard(); guardErr != nil {
					return errors.Join(err, guardErr)
				}
				if capitalFailure == nil {
					reason := err.Error()
					if !rt.shutdownCloseUnverified.CompareAndSwap(nil, &reason) {
						return errors.Join(err, fmt.Errorf("capital failure was superseded"))
					}
					capitalFailure = &reason
				}
				if rt.OpeningGate != nil {
					rt.OpeningGate.Block("capital_reservation_unverified")
				}
				if guardErr := guard(); guardErr != nil {
					return errors.Join(err, guardErr)
				}
				return &runtimeCapitalReleaseRetryError{cause: err}
			}
			if err := guard(); err != nil {
				return err
			}
			if capitalFailure != nil {
				if !rt.shutdownCloseUnverified.CompareAndSwap(capitalFailure, nil) {
					return fmt.Errorf("capital proof no longer owns the failure marker")
				}
				capitalFailure = nil
			}
			capitalReleased = true
		}
		if err := guard(); err != nil {
			return err
		}
		var releaseErr error
		for i := len(leases) - 1; i >= 0; i-- {
			if err := guard(); err != nil {
				return errors.Join(releaseErr, err)
			}
			released, err := releaseRuntimeOwnershipLeaseAfterVerifiedStop(leases[i], nil, "")
			if err == nil && !released {
				err = fmt.Errorf("funding_carry ownership remains unreleased")
			}
			releaseErr = errors.Join(releaseErr, err)
		}
		if err := guard(); err != nil {
			return errors.Join(releaseErr, err)
		}
		if releaseErr != nil {
			return &runtimeOwnershipReleaseRetryError{cause: releaseErr}
		}
		completed = true
		return nil
	}
}
