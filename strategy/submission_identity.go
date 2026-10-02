package strategy

import (
	"fmt"

	"quantmesh/execution"
	"quantmesh/position"
	"quantmesh/utils"
)

// Pin the CID before capital and route registration, including strategies that
// historically supplied no CID. Duplicate calls cannot replace an existing
// reservation (or race to release another call's reservation).
func (mse *MultiStrategyExecutor) beginSubmission(req *position.OrderRequest) (func(), error) {
	if req.BotWideClose {
		return nil, fmt.Errorf("strategy submission cannot request bot-wide close allocation")
	}
	if req.ClientOrderID == "" {
		req.ClientOrderID = utils.NewCompactOrderID()
	}
	key := req.ClientOrderID
	if _, loaded := mse.submissions.LoadOrStore(key, struct{}{}); loaded {
		return nil, fmt.Errorf("clientOrderID %s: %w", key, execution.ErrIntentPending)
	}
	releaseIdentity := func() { mse.submissions.Delete(key) }
	mse.mu.RLock()
	_, tracked := mse.ordersByClient[key]
	mse.mu.RUnlock()
	if tracked {
		releaseIdentity()
		return nil, fmt.Errorf("clientOrderID %s already tracked: %w", key, execution.ErrIntentPending)
	}
	finishAdmission, err := mse.capitalSubmissionGate.Begin()
	if err != nil {
		releaseIdentity()
		return nil, fmt.Errorf("capital reconciliation prevents strategy submission: %w", err)
	}
	return func() {
		releaseIdentity()
		finishAdmission()
	}, nil
}
