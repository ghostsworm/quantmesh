package strategy

import (
	"context"
	"fmt"
)

// Caller holds s.mu so the current gate is checked at the financial commit,
// not only before an exchange query. This is not a durable generation fence.
func (s *FundingCarryStrategy) verifyDebtCommitLocked(ctx context.Context) error {
	if ctx == nil {
		return fmt.Errorf("margin debt commit requires context")
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("margin debt commit canceled: %w", err)
	}
	return verifyStrategyWalletRuntimeOwner(s.openingGate)
}
