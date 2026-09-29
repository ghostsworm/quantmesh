package position

import (
	"fmt"
	"math"
	"quantmesh/config"
	"quantmesh/execution"
)

// Before a hot update the constructor's private config is the initial source.
// No hot updater writes that config: other strategy components may retain it.
func (spm *SuperPositionManager) riskControlSnapshot() config.RiskControls {
	if snapshot := spm.riskControls.Load(); snapshot != nil {
		return *snapshot
	}
	return config.RiskControls{Open: spm.config.Trading.OpenPositionControl, Grid: spm.config.Trading.GridRiskControl}
}

func (spm *SuperPositionManager) openingControl() config.OpenPositionControl {
	return spm.riskControlSnapshot().Open
}
func (spm *SuperPositionManager) gridRiskControl() config.GridRiskControl {
	return spm.riskControlSnapshot().Grid
}

func (spm *SuperPositionManager) GetRiskControls() config.RiskControls {
	return spm.riskControlSnapshot().Clone()
}

// The grid mutex prevents an AdjustOrders tick from mixing two revisions.
// Other readers use the atomically published immutable snapshot.
func (spm *SuperPositionManager) SetRiskControls(controls config.RiskControls) {
	spm.mu.Lock()
	defer spm.mu.Unlock()
	spm.publishRiskControlsLocked(controls)
}

// SetVerifiedCapitalLimit installs the immutable startup-verified Bot budget.
// All subsequent control publication paths, including OpeningController's
// direct updates, are clamped against this ceiling.
func (spm *SuperPositionManager) SetVerifiedCapitalLimit(limit float64) error {
	if !validRuntimeLimit(limit) || limit <= 0 {
		return fmt.Errorf("verified capital limit must be finite and positive")
	}
	spm.mu.Lock()
	defer spm.mu.Unlock()
	if spm.verifiedCapitalLimit > 0 && spm.verifiedCapitalLimit != limit {
		return fmt.Errorf("verified capital limit is immutable once installed")
	}
	spm.verifiedCapitalLimit = limit
	spm.publishRiskControlsLocked(spm.riskControlSnapshot())
	return nil
}

func validRuntimeLimit(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}

func (spm *SuperPositionManager) publishRiskControlsLocked(controls config.RiskControls) {
	copy := controls.Clone()
	if limit := spm.verifiedCapitalLimit; limit > 0 {
		copy.Open.MaxPositionValue = clampNotionalLimit(copy.Open.MaxPositionValue, limit)
		if copy.Open.BotRiskControl != nil {
			copy.Open.BotRiskControl.MaxPositionValue = clampNotionalLimit(copy.Open.BotRiskControl.MaxPositionValue, limit)
		}
	}
	// Pause/resume is owned by OpeningGate, not a stale configuration flag.
	copy.Open.PauseOpening = false
	if copy.Open.BotRiskControl != nil {
		copy.Open.BotRiskControl.PauseOpening = false
		copy.Open.BotRiskControl.PauseOpeningReason = ""
	}
	if executor, ok := spm.executor.(interface {
		SetExposureLimits(execution.ExposureLimits) error
	}); ok {
		qty, value, layers := copy.Open.PositionLimits()
		if err := executor.SetExposureLimits(execution.ExposureLimits{Quantity: qty, Notional: value, Layers: layers}); err != nil {
			spm.openingGate.Block("invalid_exposure_limits")
		} else {
			spm.openingGate.Unblock("invalid_exposure_limits")
		}
	}
	spm.riskControls.Store(&copy)
	spm.markAdjustDirty()
}

func clampNotionalLimit(configured, verified float64) float64 {
	if math.IsNaN(configured) || math.IsInf(configured, 0) || configured <= 0 || configured > verified {
		return verified
	}
	return configured
}

func (spm *SuperPositionManager) positionLimitReached(price float64) bool {
	quantity, value, layers := spm.openingControl().PositionLimits()
	totalQty, totalValue, totalLayers, valued := spm.GetPositionExposure(price)
	return (quantity > 0 && totalQty >= quantity) ||
		(value > 0 && (totalValue >= value || (!valued && totalQty > 0))) ||
		(layers > 0 && totalLayers >= layers)
}

// GetPositionExposure measures gross filled inventory, never netting opposite
// legs or deriving quantity from entry cost. A missing mark does not hide qty.
func (spm *SuperPositionManager) GetPositionExposure(price float64) (quantity, notional float64, layers int, valued bool) {
	invalidInventory := false
	spm.slots.Range(func(_, raw interface{}) bool {
		slot := raw.(*InventorySlot)
		slot.mu.RLock()
		qty := slot.PositionQty
		// Slots store positive economic quantity for both legs. Treat malformed
		// values and aggregate overflow as unknown inventory, never as flat.
		if math.IsNaN(qty) || math.IsInf(qty, 0) || qty < 0 {
			invalidInventory = true
		} else if qty > 0 {
			quantity += qty
			if math.IsInf(quantity, 0) || math.IsNaN(quantity) {
				invalidInventory = true
			}
			layers++
		}
		slot.mu.RUnlock()
		return true
	})
	if invalidInventory {
		// A finite sentinel keeps API serialization valid while ensuring both
		// quantity and value limits fail closed at their existing call sites.
		return math.MaxFloat64, 0, layers, false
	}
	valued = price > 0 && !math.IsNaN(price) && !math.IsInf(price, 0)
	if valued {
		notional = quantity * price
		if math.IsNaN(notional) || math.IsInf(notional, 0) {
			return quantity, 0, layers, false
		}
	}
	return
}

// GetPositionLegQuantities returns gross filled inventory for each economic
// side. Opposite legs are deliberately not netted, which is required when a
// shutdown coordinator reconciles several Bot ledgers against hedge-mode venue
// positions.

func (spm *SuperPositionManager) GetPositionLegQuantities() (long, short float64, err error) {
	invalidInventory := false
	spm.slots.Range(func(_, raw interface{}) bool {
		slot := raw.(*InventorySlot)
		slot.mu.RLock()
		qty := slot.PositionQty
		if math.IsNaN(qty) || math.IsInf(qty, 0) || qty < 0 {
			invalidInventory = true
		} else if qty > 0 {
			if spm.isBoth() && slot.PositionLeg != PositionLegLong && slot.PositionLeg != PositionLegShort {
				invalidInventory = true
			} else if spm.liquidationIsShortLeg(slot.PositionLeg) {
				short += slot.PositionQty
			} else {
				long += slot.PositionQty
			}
			if math.IsInf(long, 0) || math.IsInf(short, 0) {
				invalidInventory = true
			}
		}
		slot.mu.RUnlock()
		return true
	})
	if invalidInventory {
		return 0, 0, fmt.Errorf("position leg inventory is invalid or overflowing")
	}
	return long, short, nil
}

func (spm *SuperPositionManager) SetOpenPositionControl(control config.OpenPositionControl) {
	spm.mu.Lock()
	defer spm.mu.Unlock()
	next := spm.riskControlSnapshot()
	next.Open = control
	spm.publishRiskControlsLocked(next)
}

func (spm *SuperPositionManager) SetGridRiskControl(control config.GridRiskControl) {
	spm.mu.Lock()
	defer spm.mu.Unlock()
	next := spm.riskControlSnapshot()
	next.Grid = control
	spm.publishRiskControlsLocked(next)
}
