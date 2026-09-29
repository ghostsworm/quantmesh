package execution

import "fmt"

// ReconcileGroupPositions replaces only one owner's reconciled inventory group.
// It refuses to overwrite a group while any order in that group is unresolved.
func (b *ExposureBook) ReconcileGroupPositions(group string, positions []ExposurePosition) error {
	if group == "" {
		return fmt.Errorf("exposure reconciliation group is required")
	}
	updated := make(map[string]*exposureLot, len(positions))
	order := make([]string, 0, len(positions))
	for _, position := range positions {
		quantity, err := exposureNumber(position.Quantity)
		if err != nil || quantity.Sign() <= 0 || position.Key == "" || position.Group != group || !exposureLeg(position.Leg) {
			return fmt.Errorf("invalid reconciled exposure position")
		}
		if _, exists := updated[position.Key]; exists {
			return fmt.Errorf("duplicate reconciled exposure lot")
		}
		updated[position.Key] = &exposureLot{group: group, leg: position.Leg, quantity: quantity}
		order = append(order, position.Key)
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.initialized || !b.ready || b.reason != "" {
		return fmt.Errorf("exposure book is not eligible for inventory reconciliation: %w", ErrExposureUnverified)
	}
	for _, intent := range b.intents {
		if intent.request.Group == group && (!intent.terminal || intent.unknown) {
			return b.failLocked("cannot reconcile inventory with unresolved group orders")
		}
	}

	nextLots := make(map[string]*exposureLot, len(b.lots)+len(updated))
	nextOrder := make([]string, 0, len(b.lotOrder)+len(order))
	for _, key := range b.lotOrder {
		lot := b.lots[key]
		if lot.group == group {
			continue
		}
		nextLots[key] = lot
		nextOrder = append(nextOrder, key)
	}
	for _, key := range order {
		if _, exists := nextLots[key]; exists {
			return fmt.Errorf("reconciled exposure lot conflicts with another group")
		}
		nextLots[key] = updated[key]
		nextOrder = append(nextOrder, key)
	}
	b.lots, b.lotOrder = nextLots, nextOrder
	return nil
}
