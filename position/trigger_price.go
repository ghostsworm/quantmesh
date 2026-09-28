package position

// triggerPricePending gates new exposure only. Existing holdings must still
// receive stop-loss, take-profit and ordinary closing-order maintenance.
func (spm *SuperPositionManager) triggerPricePending(currentPrice float64) bool {
	trigger := spm.config.Trading.TriggerPrice
	if trigger <= 0 {
		return false
	}
	if spm.isShort() {
		return currentPrice < trigger
	}
	return currentPrice > trigger
}
