package position

import (
	"context"
	"quantmesh/config"
	"quantmesh/execution"
)

const (
	equityDataBlock         = "equity_data_unverified"
	stopLossEquityDataBlock = "stop_loss_equity_unverified"
	gridRiskNotionalBlock   = "grid_risk_notional_unverified"
)

// refreshStopLossEquityOpeningGate protects configurations whose hard stop is
// denominated in account equity. Missing/stale equity blocks new exposure and
// cancels owner-scoped residual opening orders; recovery releases only this hold.
func (spm *SuperPositionManager) refreshStopLossEquityOpeningGate() {
	risk := spm.gridRiskControl()
	required := risk.Enabled && risk.StopLossRatio > 0 && risk.GetStopLossBasis() == config.StopLossBasisEquity
	available := required && spm.accountEquityForStopLoss() > 0
	blocked := spm.openingGate.HasBlock(stopLossEquityDataBlock)
	if !available {
		if required && !blocked {
			spm.openingGate.Block(stopLossEquityDataBlock)
			if _, ok := spm.executor.(interface{ CancelOwnedOpeningOrders(context.Context) error }); ok {
				// Cancellation may wait for another sweep to finish. Hold its
				// independent source before scheduling so recovery cannot reopen
				// admission while this cancellation is queued or in flight.
				spm.openingGate.Block(execution.UnverifiedCancellationBlock)
			}
			spm.markAdjustDirty()
			go spm.CancelResidualOpeningOrders()
		} else if !required && blocked {
			spm.openingGate.Unblock(stopLossEquityDataBlock)
			spm.markAdjustDirty()
		}
		return
	}
	if blocked {
		spm.openingGate.Unblock(stopLossEquityDataBlock)
		spm.markAdjustDirty()
	}
}

func (spm *SuperPositionManager) refreshGridRiskNotionalGate(currentPrice float64) {
	risk := spm.gridRiskControl()
	requiresNotional := risk.Enabled && (risk.StopLossRatio > 0 ||
		(risk.TakeProfitTriggerRatio > 0 && risk.TrailingTakeProfitRatio > 0) ||
		(risk.CloseConditionEnabled && (risk.CloseConditionProfitTarget > 0 || risk.CloseConditionLossLimit > 0)))
	blocked := spm.openingGate.HasBlock(gridRiskNotionalBlock)
	if !requiresNotional {
		if blocked {
			spm.openingGate.Unblock(gridRiskNotionalBlock)
			spm.markAdjustDirty()
		}
		return
	}
	value := spm.calculateTotalPositionValue(currentPrice)
	if finiteGridValue(value) && value >= 0 {
		if blocked {
			spm.openingGate.Unblock(gridRiskNotionalBlock)
			spm.markAdjustDirty()
		}
		return
	}
	if !blocked {
		spm.openingGate.Block(gridRiskNotionalBlock)
		if _, ok := spm.executor.(interface{ CancelOwnedOpeningOrders(context.Context) error }); ok {
			spm.openingGate.Block(execution.UnverifiedCancellationBlock)
		}
		spm.markAdjustDirty()
		go spm.CancelResidualOpeningOrders()
	}
}

// SetEquityRiskPaused returns whether the owned block changed. It does not
// pause price delivery or protective closes, and cannot clear another owner.
func (spm *SuperPositionManager) SetEquityRiskPaused(paused bool) bool {
	previous := spm.openingGate.HasBlock(equityDataBlock)
	if paused {
		spm.openingGate.Block(equityDataBlock)
	} else {
		spm.openingGate.Unblock(equityDataBlock)
	}
	if previous != paused {
		spm.markAdjustDirty()
	}
	return previous != paused
}
