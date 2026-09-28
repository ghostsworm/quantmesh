package position

const priceFeedStaleBlock = "price_feed_stale"

// SetMarketRiskPaused is an independent source: market recovery must not undo
// a manual/circuit-breaker pause, and a manual resume must not undo bad markets.
// Price delivery must continue so every strategy can manage existing holdings.
func (spm *SuperPositionManager) SetMarketRiskPaused(paused bool) {
	if paused {
		spm.openingGate.Block("market_risk")
	} else {
		spm.openingGate.Unblock("market_risk")
	}
	spm.markAdjustDirty()
}

// SetPriceFeedStale protects new exposure when the authoritative market feed
// stops. It is independent from market-risk and manual pause sources.
func (spm *SuperPositionManager) SetPriceFeedStale(stale bool) {
	if stale {
		spm.openingGate.Block(priceFeedStaleBlock)
	} else {
		spm.openingGate.Unblock(priceFeedStaleBlock)
	}
	spm.markAdjustDirty()
}
