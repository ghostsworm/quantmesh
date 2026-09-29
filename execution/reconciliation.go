package execution

import "time"

// PositionReconciliationLockTTL bounds the lease shared by snapshots and
// physical order submissions. Holders renew it while their critical section
// is active.
const PositionReconciliationLockTTL = 30 * time.Second
const PositionCoordinationLockLostBlock = "position_coordination_lock_lost"

// PositionReconciliationUnverifiedBlock is cleared only after a complete,
// authoritative reconciliation succeeds for the same executor.
const PositionReconciliationUnverifiedBlock = "position_reconciliation_unverified"

// PositionReconciliationLockKey coordinates account-position snapshots with
// every standard order submission for the same venue and symbol.
func PositionReconciliationLockKey(exchangeName, symbol string) string {
	return "reconcile:" + exchangeName + ":" + symbol
}
