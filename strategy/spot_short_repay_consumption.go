package strategy

import "fmt"

// Caller holds s.mu; consumption and fill cursors are persisted together.
// Keep evidence after terminal intent deletion. Do not prune without a ledger proof.
func (s *SpotShortStrategy) consumeRepaymentTransferLocked(transferID, orderID int64) error {
	if transferID <= 0 || orderID <= 0 {
		return fmt.Errorf("repayment consumption requires positive transaction and order identities")
	}
	if owner, consumed := s.consumedRepayTransfers[transferID]; consumed {
		return fmt.Errorf("repayment transaction %d was already consumed by order %d", transferID, owner)
	}
	if s.consumedRepayTransfers == nil {
		s.consumedRepayTransfers = make(map[int64]int64)
	}
	s.consumedRepayTransfers[transferID] = orderID
	return nil
}
