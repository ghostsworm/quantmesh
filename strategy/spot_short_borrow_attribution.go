package strategy

import (
	"fmt"
	"math"

	"quantmesh/exchange"
)

// Caller holds s.mu. A unique history row may still match another pending intent.
func (s *SpotShortStrategy) verifyBorrowTransferAttributionLocked(clientOrderID string, record exchange.MarginBorrowRecord) error {
	for cid, other := range s.pendingBorrow {
		if cid == clientOrderID {
			continue
		}
		if other.BorrowTransferID == record.TransferID {
			return fmt.Errorf("borrow transfer %d is already assigned to another pending intent", record.TransferID)
		}
		if other.Phase == "prepared" && record.Timestamp >= other.CreatedAtUnixMilli &&
			math.Abs(record.Amount-other.Amount) <= math.Max(1e-10, other.Amount*1e-8) {
			return fmt.Errorf("borrow transfer %d also matches another unresolved borrow intent", record.TransferID)
		}
	}
	return nil
}
