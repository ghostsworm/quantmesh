package monitor

import "time"

// GetQuoteEvidence returns one received quote and its local receipt timestamp.
// It includes unchanged and invalid quotes so consumers cannot mistake a stale
// last-good cached price for current valid evidence. Reading never refreshes it.
func (pm *PriceMonitor) GetQuoteEvidence() (float64, time.Time) {
	if quote := pm.lastQuote.Load(); quote != nil {
		return quote.NewPrice, quote.Timestamp
	}
	return 0, time.Time{}
}
