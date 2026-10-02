package strategy

import (
	"context"
	"quantmesh/execution"
)

// SetOpeningGate binds independent wallet mutations to the same runtime owner
// state used by the physical executor. Configure it before registration/start.
func (s *SpotShortStrategy) SetOpeningGate(gate *execution.OpeningGate) {
	s.mu.Lock()
	s.openingGate = gate
	s.mu.Unlock()
}

func (s *SpotShortStrategy) verifyRuntimeWalletOwner() error {
	s.mu.RLock()
	gate := s.openingGate
	s.mu.RUnlock()
	return verifyStrategyWalletRuntimeOwner(gate)
}

// Caller holds s.mu. Recheck after external proof reads and before changing
// recovery state; opening-only pauses must not prohibit protective recovery.
func (s *SpotShortStrategy) verifyRecoveryCommitLocked(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return verifyStrategyWalletRuntimeOwner(s.openingGate)
}
