package strategy

import (
	"fmt"
	"math"

	"quantmesh/exchange"
)

// Caller holds s.mu. Query fallback must preserve an already known identity.
func (s *SpotShortStrategy) verifyRepayTransferAttributionLocked(orderID int64, pending spotShortPendingRepay, record exchange.MarginBorrowRecord) error {
	if pending.RepayTransferID > 0 && pending.RepayTransferID != record.TransferID {
		return fmt.Errorf("repayment transaction %d does not match saved transaction %d", record.TransferID, pending.RepayTransferID)
	}
	for id, other := range s.pendingRepay {
		if id == orderID {
			continue
		}
		if other.RepayTransferID == record.TransferID {
			return fmt.Errorf("repayment transaction %d is already assigned to another order", record.TransferID)
		}
		if other.RepayUncertain && !other.RepayPrepared && other.RepayTransferID == 0 &&
			record.Timestamp >= other.RepayStartedAtUnixMilli &&
			math.Abs(record.Amount-other.RepayAmount) <= math.Max(1e-10, other.RepayAmount*1e-8) {
			return fmt.Errorf("repayment transaction %d also matches another unresolved repayment", record.TransferID)
		}
	}
	return nil
}
