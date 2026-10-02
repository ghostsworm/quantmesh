package strategy

import (
	"context"
	"fmt"
	"reflect"
)

// Built-in Combo child types all implement context-aware inventory snapshots.
// Missing capability is an error, never permission to omit a child or to wait
// on its synchronous getter after the request deadline.
func (s *ComboStrategy) CapitalReleaseInventorySnapshot(ctx context.Context) ([]*Position, []*Order, error) {
	if err := acquireCapitalProofLock(ctx, s.mu.TryRLock, s.mu.RUnlock); err != nil {
		return nil, nil, err
	}
	defer s.mu.RUnlock()
	if s.initErr != nil || len(s.strategies) == 0 {
		return nil, nil, fmt.Errorf("Combo inventory initialization is unavailable")
	}
	positions := make([]*Position, 0)
	orders := make([]*Order, 0)
	for _, child := range s.strategies {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		if child == nil || (reflect.ValueOf(child).Kind() == reflect.Ptr && reflect.ValueOf(child).IsNil()) {
			return nil, nil, fmt.Errorf("Combo inventory child is unavailable")
		}
		if _, nested := child.(*ComboStrategy); nested {
			return nil, nil, fmt.Errorf("nested Combo is not a supported configured inventory child")
		}
		reader, ok := child.(CapitalReleaseInventoryReader)
		if !ok {
			return nil, nil, fmt.Errorf("Combo child inventory requires cancellable snapshots")
		}
		childPositions, childOrders, err := reader.CapitalReleaseInventorySnapshot(ctx)
		if err != nil {
			return nil, nil, fmt.Errorf("read Combo child inventory: %w", err)
		}
		positions = append(positions, childPositions...)
		orders = append(orders, childOrders...)
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	return positions, orders, nil
}
