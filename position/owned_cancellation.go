package position

import (
	"context"
	"time"

	"quantmesh/logger"
)

// CancelResidualOpeningOrders includes non-grid orders through the shared
// executor. Never replace missing ownership evidence with an account-wide sweep.
func (spm *SuperPositionManager) CancelResidualOpeningOrders() {
	spm.sleep(pauseResidualCancelDelay)
	if !spm.IsOpeningPaused() {
		return
	}
	if executor, ok := spm.executor.(interface{ CancelOwnedOpeningOrders(context.Context) error }); ok {
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		if err := executor.CancelOwnedOpeningOrders(ctx); err != nil {
			logger.Error("[%s] 開倉撤單尚未核實，保持暫停: %v", spm.logPrefix(), err)
		}
		return
	}
	// Replay/standalone executors may have no venue ownership registry.
	spm.CancelAllOpenOrders()
}
