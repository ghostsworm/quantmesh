package strategy

import (
	"context"
	"fmt"
)

// Called under the account wallet lease and tradeMu. Only the durable
// unsubmitted phase proves no Borrow could have been called by this protocol.
func (s *SpotShortStrategy) clearUnsubmittedBorrow(ctx context.Context, cid string, expected spotShortPendingBorrow) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := verifyStrategyWalletRuntimeOwner(s.openingGate); err != nil {
		return err
	}
	current, found := s.pendingBorrow[cid]
	if !found || current != expected || current.Phase != "unsubmitted" || current.BorrowTransferID != 0 {
		return fmt.Errorf("unsubmitted borrow intent changed during recovery")
	}
	delete(s.pendingBorrow, cid)
	if err := s.persistRuntimeStateLocked(); err != nil {
		s.pendingBorrow[cid] = current
		return fmt.Errorf("persist unsubmitted borrow recovery: %w", err)
	}
	return nil
}
