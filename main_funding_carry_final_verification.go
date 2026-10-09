package main

import (
	"context"
	"errors"
	"fmt"

	"quantmesh/strategy"
)

type runtimeStopVerificationRetryError struct{ cause error }

func (e *runtimeStopVerificationRetryError) Error() string {
	return fmt.Sprintf("stopped funding_carry runtime requires final read-only verification: %v", e.cause)
}
func (e *runtimeStopVerificationRetryError) Unwrap() error { return e.cause }

func isRetryableRuntimeStopVerification(err error) bool {
	var pending *runtimeStopVerificationRetryError
	var capital *runtimeCapitalReleaseRetryError
	return errors.As(err, &pending) || errors.As(err, &capital)
}

// All methods run under the owning runtime's stopMu. Only the pointer created
// here may be cleared: a later failure with identical text is still different
// evidence and must be preserved. The runtime remains sealed throughout.
type fundingCarryFinalStopVerification struct {
	rt             *SymbolRuntime
	reason         *string
	ownershipGuard func() error
	verify         func(context.Context) error
}

func (v *fundingCarryFinalStopVerification) record(err error) bool {
	if !strategy.IsFundingCarryFinalVerificationPending(err) {
		return false
	}
	return v.recordReason(err.Error())
}

func (v *fundingCarryFinalStopVerification) recordReason(reason string) bool {
	if v.rt == nil || v.reason != nil || v.ownershipGuard == nil || v.verify == nil || reason == "" {
		return false
	}
	if !v.rt.shutdownCloseUnverified.CompareAndSwap(nil, &reason) {
		return false
	}
	v.reason = &reason
	return true
}

func (v *fundingCarryFinalStopVerification) guard() error {
	if v.rt == nil || v.reason == nil || v.rt.shutdownCloseUnverified.Load() != v.reason || v.ownershipGuard == nil || v.verify == nil {
		return fmt.Errorf("final stop verification no longer owns the cached failure")
	}
	return v.ownershipGuard()
}

func (v *fundingCarryFinalStopVerification) pending(cause error) error {
	if err := v.guard(); err != nil {
		return errors.Join(cause, err)
	}
	return &runtimeStopVerificationRetryError{cause: cause}
}

func (v *fundingCarryFinalStopVerification) reconcile(ctx context.Context) error {
	if err := v.guard(); err != nil {
		return err
	}
	if ctx == nil {
		return fmt.Errorf("final stop verification requires context")
	}
	proofErr := ctx.Err()
	if proofErr == nil {
		proofErr = v.verify(ctx)
	}
	if err := v.guard(); err != nil {
		return errors.Join(proofErr, err) // Other failures supersede retryability.
	}
	if proofErr == nil {
		proofErr = ctx.Err()
	}
	if proofErr != nil {
		return &runtimeStopVerificationRetryError{cause: proofErr}
	}
	if !v.rt.shutdownCloseUnverified.CompareAndSwap(v.reason, nil) {
		return fmt.Errorf("final stop verification failure changed before commit")
	}
	v.reason = nil
	return nil
}
