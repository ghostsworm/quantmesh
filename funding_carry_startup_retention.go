package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"quantmesh/storage"
)

const fundingCarryFailedStartupAuditTimeout = 5 * time.Second

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

// Even failures before capital admission can leave claims from an earlier
// runtime. Read them without the canceled startup context; never mutate them.
func auditFundingCarryFailedStartup(err error, botID string, checker storage.AccountWalletCapitalReservationBotChecker) error {
	if err == nil {
		return nil
	}
	var retained *fundingCarryStartupRetentionError
	if errors.As(err, &retained) {
		return err
	}
	var auditErr error
	if checker == nil {
		auditErr = errors.New("persistent wallet reservation checker unavailable after startup failure")
	} else {
		ctx, cancel := context.WithTimeout(context.Background(), fundingCarryFailedStartupAuditTimeout)
		defer cancel()
		held, checkErr := checker.HasAccountWalletCapitalReservation(ctx, botID)
		if checkErr != nil {
			auditErr = fmt.Errorf("read persistent wallet reservations after startup failure: %w", checkErr)
		} else if held {
			auditErr = errors.New("persistent wallet reservations remain after startup failure")
		}
	}
	if auditErr == nil {
		return err
	}
	return errors.Join(err, &fundingCarryStartupRetentionError{Cause: auditErr})
}
