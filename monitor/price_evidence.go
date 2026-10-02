package monitor

import (
	"context"
	"fmt"
	"time"
)

// GetQuoteEvidence returns one received quote and its local receipt timestamp.
// It includes unchanged and invalid quotes so consumers cannot mistake a stale
// last-good cached price for current valid evidence. Reading never refreshes it.
func (pm *PriceMonitor) GetQuoteEvidence() (float64, time.Time) {
	if quote := pm.lastQuote.Load(); quote != nil {
		return quote.NewPrice, quote.Timestamp
	}
	return 0, time.Time{}
}

// The same atomic received quote, without refreshing its evidence timestamp.
func (pm *PriceMonitor) GetQuoteEvidenceContext(ctx context.Context) (float64, time.Time, error) {
	if ctx == nil || pm == nil {
		return 0, time.Time{}, fmt.Errorf("quote evidence requires monitor and context")
	}
	if err := ctx.Err(); err != nil {
		return 0, time.Time{}, err
	}
	price, at := pm.GetQuoteEvidence()
	if err := ctx.Err(); err != nil {
		return 0, time.Time{}, err
	}
	return price, at, nil
}
