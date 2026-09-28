package position

import (
	"context"
	"quantmesh/execution"
)

const volatilityRiskBlock = "bot_volatility_risk"

// SetVolatilityRiskPause changes only the volatility-owned admission source.
// Manual resume and another controller must never erase this live risk.
// The result says whether a new hold was acquired (requiring owned cancellation).
func (spm *SuperPositionManager) SetVolatilityRiskPause(reason string) bool {
	spm.openingPauseMu.Lock()
	defer spm.openingPauseMu.Unlock()
	wasBlocked := spm.openingGate.HasBlock(volatilityRiskBlock)
	spm.volatilityPauseReason.Store(reason)
	if reason == "" {
		spm.openingGate.Unblock(volatilityRiskBlock)
	} else {
		spm.openingGate.Block(volatilityRiskBlock)
		if !wasBlocked {
			if _, ok := spm.executor.(interface{ CancelOwnedOpeningOrders(context.Context) error }); ok {
				// Acquire before scheduling: recovery must not reopen admission
				// while the cancellation worker is still waiting to start.
				spm.openingGate.Block(execution.UnverifiedCancellationBlock)
			}
		}
	}
	spm.markAdjustDirty()
	return reason != "" && !wasBlocked
}

func (spm *SuperPositionManager) IsVolatilityRiskPaused() bool {
	return spm.openingGate.HasBlock(volatilityRiskBlock)
}

func (spm *SuperPositionManager) CancelVolatilityOpeningOrders(ctx context.Context) {
	if executor, ok := spm.executor.(interface{ CancelOwnedOpeningOrders(context.Context) error }); ok {
		if err := executor.CancelOwnedOpeningOrders(ctx); err != nil {
			spm.openingGate.Block(execution.UnverifiedCancellationBlock)
		}
		return
	}
	// Only standalone/replay executors lack the owner registry.
	spm.CancelAllOpenOrders()
}
