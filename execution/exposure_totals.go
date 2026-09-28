package execution

import (
	"fmt"
	"math"
	"math/big"
	"time"
)

type exposureTotals struct {
	positions, pending, notional *big.Rat
	layers                       map[string]bool
}

func (b *ExposureBook) totalsLocked(extra *ExposureRequest) exposureTotals {
	t := exposureTotals{positions: new(big.Rat), pending: new(big.Rat), notional: new(big.Rat), layers: make(map[string]bool)}
	mark, _ := exposureNumber(b.mark)
	for key, lot := range b.lots {
		if lot.quantity.Sign() > 0 {
			t.positions.Add(t.positions, lot.quantity)
			t.notional.Add(t.notional, new(big.Rat).Mul(lot.quantity, mark))
			t.layers[key] = true
		}
	}
	addPending := func(req ExposureRequest, filled *big.Rat, terminal bool) {
		if !req.Opening || terminal {
			return
		}
		quantity, _ := exposureNumber(req.Quantity)
		remaining := new(big.Rat).Sub(quantity, filled)
		if remaining.Sign() <= 0 {
			return
		}
		price, _ := exposureNumber(math.Max(req.Price, b.mark))
		t.pending.Add(t.pending, remaining)
		t.notional.Add(t.notional, new(big.Rat).Mul(remaining, price))
		t.layers[req.Lot] = true
	}
	for _, intent := range b.intents {
		addPending(intent.request, intent.filled, intent.terminal && !intent.unknown)
	}
	if extra != nil {
		addPending(*extra, new(big.Rat), false)
	}
	return t
}

func (b *ExposureBook) checkLimitsLocked(extra *ExposureRequest) error {
	t := b.totalsLocked(extra)
	if b.limits.Quantity > 0 {
		limit, _ := exposureNumber(b.limits.Quantity)
		if new(big.Rat).Add(t.positions, t.pending).Cmp(limit) > 0 {
			return fmt.Errorf("gross quantity including pending: %w", ErrExposureLimit)
		}
	}
	if b.limits.Notional > 0 {
		limit, _ := exposureNumber(b.limits.Notional)
		if t.notional.Cmp(limit) > 0 {
			return fmt.Errorf("gross marked/pending notional: %w", ErrExposureLimit)
		}
	}
	if b.limits.Layers > 0 && len(t.layers) > b.limits.Layers {
		return fmt.Errorf("active positions and pending layers: %w", ErrExposureLimit)
	}
	return nil
}

func (b *ExposureBook) Snapshot(now time.Time) ExposureSnapshot {
	b.mu.Lock()
	defer b.mu.Unlock()
	t := b.totalsLocked(nil)
	s := ExposureSnapshot{Ready: b.ready, Reason: b.reason, Layers: len(t.layers), Limits: b.limits, Mark: b.mark, MarkAt: b.markAt}
	s.PositionQuantity, _ = t.positions.Float64()
	s.PendingQuantity, _ = t.pending.Float64()
	s.ProjectedQuantity, _ = new(big.Rat).Add(t.positions, t.pending).Float64()
	s.ProjectedNotional, _ = t.notional.Float64()
	if err := b.readyErrorLocked(now); err != nil {
		s.Ready = false
		if s.Reason == "" {
			s.Reason = err.Error()
		}
	}
	s.ReasonCode = "ready"
	if !s.Ready {
		switch {
		case b.reason != "":
			s.ReasonCode = "reconciliation_required"
		case !b.ready:
			s.ReasonCode = "not_initialized"
		default:
			s.ReasonCode = "quote_unavailable"
		}
	}
	// Quantity/notional exhaustion rejects every positive opening. A full lot
	// count only rejects NEW lots; an existing lot may still have quantity room.
	s.OpeningAvailable = s.Ready
	for _, bound := range []struct {
		used  *big.Rat
		limit float64
	}{
		{new(big.Rat).Add(t.positions, t.pending), b.limits.Quantity},
		{t.notional, b.limits.Notional},
	} {
		if bound.limit > 0 {
			limit, _ := exposureNumber(bound.limit)
			if bound.used.Cmp(limit) >= 0 {
				s.OpeningAvailable = false
			}
		}
	}
	s.NewLotAvailable = s.OpeningAvailable && (b.limits.Layers == 0 || s.Layers < b.limits.Layers)
	return s
}
