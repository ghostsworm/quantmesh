package main

import (
	"context"
	"fmt"
	"time"

	"quantmesh/execution"
	"quantmesh/order"
	"quantmesh/storage"
)

const runtimeWalletOwnershipUnverifiedBlock = "runtime_ownership_unverified"

func verifyRuntimeWalletOwnership(gate *execution.OpeningGate) error {
	if gate.HasBlock(runtimeWalletOwnershipUnverifiedBlock) {
		return fmt.Errorf("wallet revalidation refused because runtime ownership is unverified")
	}
	return nil
}

// Install before publishing the executor or starting any order-capable worker.
// Missing evidence blocks only openings; the executor preserves protective exits.
func installRuntimeWalletOpeningAdmission(executor *order.ExchangeOrderExecutor, checker storage.AccountWalletCapitalAdmissionChecker, walletKey string) {
	executor.SetOpeningAdmissionGuard(func(ctx context.Context) error {
		if checker == nil || walletKey == "" {
			return fmt.Errorf("persistent wallet opening admission evidence is unavailable")
		}
		return checker.CheckAccountWalletCapitalAdmission(ctx, []string{walletKey}, 2*accountWalletCapitalRefreshInterval)
	})
}

func verifyRecoveredWalletOpeningCancellation(ctx context.Context, gate *execution.OpeningGate, cancelOpenings func(context.Context) error) error {
	if cancelOpenings == nil {
		return fmt.Errorf("wallet recovery requires verification of unconfirmed opening cancellations")
	}
	cancelCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err := cancelOpenings(cancelCtx); err != nil {
		return fmt.Errorf("verify owned opening cancellations after wallet balance recovery: %w", err)
	}
	if err := cancelCtx.Err(); err != nil {
		return fmt.Errorf("wallet cancellation verification deadline: %w", err)
	}
	if gate.HasBlock(execution.UnverifiedCancellationBlock) {
		return fmt.Errorf("wallet recovery still has unverified opening cancellations")
	}
	return nil
}
