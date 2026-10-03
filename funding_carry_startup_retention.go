package main

import "fmt"

// A startup error is not proof that its account-wallet claims were released.
// Callers must retain this diagnostic separately from the original failure;
// it is not admission to a running or managed-reconciliation strategy.
type fundingCarryStartupRetentionError struct {
	Cause error
}

func (e *fundingCarryStartupRetentionError) Error() string {
	return fmt.Sprintf("funding_carry startup capital reservation release is unverified; reconciliation required: %v", e.Cause)
}

func (e *fundingCarryStartupRetentionError) Unwrap() error { return e.Cause }
