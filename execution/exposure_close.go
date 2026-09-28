package execution

import (
	"fmt"
	"math/big"
)

// Close allocation is atomic with admission. Pending opens cannot fund a close,
// and pending/unknown closes cannot lend the same inventory to another caller.
// Price freshness and opening limits do not prevent reducing verified inventory.
func (b *ExposureBook) reserveCloseLocked(req ExposureRequest, quantity *big.Rat) (map[string]*big.Rat, error) {
	if !b.ready {
		return nil, fmt.Errorf("close inventory not initialized: %w", ErrExposureUnverified)
	}
	for _, intent := range b.intents {
		if !intent.request.Opening && intent.unknown && (req.BotWideClose || intent.request.BotWideClose || intent.request.Group == req.Group) && intent.request.Leg == req.Leg {
			return nil, fmt.Errorf("unknown close must be reconciled before another close: %w", ErrExposureUnverified)
		}
	}
	remaining := new(big.Rat).Set(quantity)
	allocation := make(map[string]*big.Rat)
	for _, key := range b.lotOrder {
		lot := b.lots[key]
		if (req.Lot != "" && req.Lot != key) || (!req.BotWideClose && lot.group != req.Group) || lot.leg != req.Leg {
			continue
		}
		free := new(big.Rat).Set(lot.quantity)
		for _, intent := range b.intents {
			if intent.request.Opening || (intent.terminal && !intent.unknown) {
				continue
			}
			if held := intent.closeLots[key]; held != nil {
				free.Sub(free, held)
			}
		}
		if free.Sign() < 0 {
			return nil, b.failLocked("close reservations exceed owned inventory")
		}
		if free.Sign() == 0 {
			continue
		}
		if free.Cmp(remaining) > 0 {
			free.Set(remaining)
		}
		allocation[key] = free
		remaining.Sub(remaining, free)
		if remaining.Sign() == 0 {
			return allocation, nil
		}
	}
	return nil, fmt.Errorf("insufficient unreserved owned close inventory: %w", ErrExposureLimit)
}
