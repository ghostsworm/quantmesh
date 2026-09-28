package position

const equityDataBlock = "equity_data_unverified"

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
