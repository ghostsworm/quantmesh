package position

import "math"

// FillProgress tracks exchange cumulative fills, not the last execution's price.
// Callers serialize updates and keep this state until the order is terminal.
type FillProgress struct {
	Quantity float64
	Notional float64
}

// Advance returns only newly executed quantity and its incremental average price.
// Replayed, stale, and malformed updates never move the cursor backwards.
func (f *FillProgress) Advance(quantity, averagePrice, fallbackPrice float64) (float64, float64) {
	if !positiveFinite(quantity) || quantity <= f.Quantity {
		return 0, 0
	}
	delta := quantity - f.Quantity
	notional := f.Notional + delta*fallbackPrice
	if positiveFinite(averagePrice) {
		notional = quantity * averagePrice
	}
	deltaPrice := (notional - f.Notional) / delta
	if !positiveFinite(notional) || !positiveFinite(deltaPrice) {
		return 0, 0
	}
	f.Quantity, f.Notional = quantity, notional
	return delta, deltaPrice
}

func positiveFinite(v float64) bool {
	return v > 0 && !math.IsNaN(v) && !math.IsInf(v, 0)
}
