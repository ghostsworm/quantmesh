package strategy

import "context"

// GetAllStrategiesContext copies the strategy registry without holding its
// mutex while callers query individual strategies or durable state.
// The copied map is not an inventory/flatness proof.
func (sm *StrategyManager) GetAllStrategiesContext(ctx context.Context) (map[string]Strategy, error) {
	if err := acquireCapitalProofLock(ctx, sm.mu.TryRLock, sm.mu.RUnlock); err != nil {
		return nil, err
	}
	defer sm.mu.RUnlock()
	result := make(map[string]Strategy, len(sm.strategies))
	for name, current := range sm.strategies {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		result[name] = current
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return result, nil
}
