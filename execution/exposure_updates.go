package execution

import (
	"fmt"
	"math"
	"math/big"
	"strings"
)

func exposureTerminal(status string) bool {
	switch status {
	case "FILLED", "CANCELED", "CANCELLED", "EXPIRED", "REJECTED":
		return true
	}
	return false
}

// Observe consumes cumulative filled quantity exactly once. Only a valid venue
// terminal report releases the unfilled remainder; cancellation requests do not.
func (b *ExposureBook) Observe(id string, update ExposureUpdate) (resultErr error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	i := b.intents[id]
	if i == nil {
		return fmt.Errorf("exposure intent missing")
	}
	defer func() {
		if resultErr != nil {
			i.unknown = true
		}
	}()
	filled, err := exposureNumber(update.CumulativeQty)
	if err != nil {
		return b.failLocked("invalid cumulative exposure fill")
	}
	if _, err := exposureNumber(update.OrderQty); err != nil {
		return b.failLocked("invalid venue order quantity")
	}
	if _, err := exposureNumber(update.Price); err != nil {
		return b.failLocked("invalid venue price")
	}
	status := strings.ToUpper(update.Status)
	switch status {
	case "NEW", "PLACED", "CONFIRMED", "PARTIALLY_FILLED", "CANCEL_REQUESTED", "FILLED", "CANCELED", "CANCELLED", "EXPIRED", "REJECTED":
	default:
		return b.failLocked("unrecognized exposure order status")
	}
	terminal := exposureTerminal(status)
	expectedQuantity := i.request.Quantity
	if i.venueQuantity > 0 {
		expectedQuantity = i.venueQuantity
	}
	if update.OrderQty > 0 {
		if i.venueQuantity > 0 && update.OrderQty != i.venueQuantity {
			return b.failLocked("venue order quantity changed after acknowledgement")
		}
		expectedQuantity = update.OrderQty
	}
	if status == "FILLED" && (filled.Sign() == 0 || update.CumulativeQty != expectedQuantity) {
		return b.failLocked("invalid filled exposure quantity")
	}
	if filled.Cmp(i.filled) < 0 {
		if terminal {
			return b.failLocked("terminal exposure quantity regressed")
		}
		return nil // stale NEW/partial acknowledgement cannot resurrect reservation
	}
	if i.terminal && filled.Cmp(i.filled) > 0 {
		return b.failLocked("fill grew after verified termination")
	}
	if i.terminal && !terminal {
		return nil
	}
	if update.OrderQty > 0 {
		i.venueQuantity = update.OrderQty
	}
	oversized := update.OrderQty > i.request.Quantity || update.CumulativeQty > i.request.Quantity
	if oversized {
		i.request.Quantity = math.Max(update.OrderQty, update.CumulativeQty)
		i.unknown = true
		_ = b.failLocked("venue increased order quantity beyond reservation")
	}
	if i.request.Opening {
		i.request.Price = math.Max(i.request.Price, update.Price)
	}
	delta := new(big.Rat).Sub(filled, i.filled)
	if delta.Sign() > 0 {
		if i.request.Opening {
			lot := b.lots[i.request.Lot]
			if lot == nil {
				return b.failLocked("opening lot missing")
			}
			lot.quantity.Add(lot.quantity, delta)
		} else if err := b.consumeCloseLocked(i, delta); err != nil {
			return err
		}
	}
	i.filled = filled
	i.terminal = terminal
	if oversized {
		return fmt.Errorf("venue increased order quantity: %w", ErrExposureUnverified)
	}
	return nil
}

func (b *ExposureBook) consumeCloseLocked(intent *exposureIntent, quantity *big.Rat) error {
	req := intent.request
	available := new(big.Rat)
	for key, reserved := range intent.closeLots {
		lot := b.lots[key]
		if lot == nil || (!req.BotWideClose && lot.group != req.Group) || lot.leg != req.Leg || lot.quantity.Cmp(reserved) < 0 {
			return b.failLocked("reserved close inventory changed")
		}
		available.Add(available, reserved)
	}
	if available.Cmp(quantity) < 0 {
		return b.failLocked("close fill exceeds known owned inventory")
	}
	remaining := new(big.Rat).Set(quantity)
	for _, key := range b.lotOrder {
		reserved := intent.closeLots[key]
		lot := b.lots[key]
		if reserved == nil || reserved.Sign() == 0 {
			continue
		}
		part := new(big.Rat).Set(reserved)
		if part.Cmp(remaining) > 0 {
			part.Set(remaining)
		}
		lot.quantity.Sub(lot.quantity, part)
		reserved.Sub(reserved, part)
		remaining.Sub(remaining, part)
		if remaining.Sign() == 0 {
			break
		}
	}
	return nil
}
