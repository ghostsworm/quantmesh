package strategy

import (
	"context"
	"sync"
)

// CapitalReleaseInventoryReader supplements (never replaces) durable owner
// proof and venue reconciliation. Returned rows must be independent copies.
type CapitalReleaseInventoryReader interface {
	CapitalReleaseInventorySnapshot(context.Context) ([]*Position, []*Order, error)
}

func capitalReleaseInventorySnapshot(ctx context.Context, mu *sync.RWMutex, read func() ([]*Position, []*Order)) ([]*Position, []*Order, error) {
	if err := acquireCapitalProofLock(ctx, mu.TryRLock, mu.RUnlock); err != nil {
		return nil, nil, err
	}
	defer mu.RUnlock()
	positions, orders := read()
	positionCopies, err := copyCapitalReleaseRows(ctx, positions)
	if err != nil {
		return nil, nil, err
	}
	orderCopies, err := copyCapitalReleaseRows(ctx, orders)
	if err != nil {
		return nil, nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	return positionCopies, orderCopies, nil
}

// Position and Order contain only value fields, including FillProgress.
func copyCapitalReleaseRows[T any](ctx context.Context, rows []*T) ([]*T, error) {
	copies := make([]*T, len(rows))
	for i, current := range rows {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if current != nil {
			copy := *current
			copies[i] = &copy
		}
	}
	return copies, nil
}

func capitalReleaseSignalInventory(position *Position, order *Order) ([]*Position, []*Order) {
	var positions []*Position
	var orders []*Order
	if position != nil {
		positions = []*Position{position}
	}
	if order != nil {
		orders = []*Order{order}
	}
	return positions, orders
}

func (s *TrendFollowingStrategy) CapitalReleaseInventorySnapshot(ctx context.Context) ([]*Position, []*Order, error) {
	return capitalReleaseInventorySnapshot(ctx, &s.mu, func() ([]*Position, []*Order) {
		return capitalReleaseSignalInventory(s.position, s.activeOrder)
	})
}

func (s *MeanReversionStrategy) CapitalReleaseInventorySnapshot(ctx context.Context) ([]*Position, []*Order, error) {
	return capitalReleaseInventorySnapshot(ctx, &s.mu, func() ([]*Position, []*Order) {
		return capitalReleaseSignalInventory(s.position, s.activeOrder)
	})
}

func (s *MomentumStrategy) CapitalReleaseInventorySnapshot(ctx context.Context) ([]*Position, []*Order, error) {
	return capitalReleaseInventorySnapshot(ctx, &s.mu, func() ([]*Position, []*Order) {
		return capitalReleaseSignalInventory(s.position, s.activeOrder)
	})
}

func (s *SpotLongStrategy) CapitalReleaseInventorySnapshot(ctx context.Context) ([]*Position, []*Order, error) {
	return capitalReleaseInventorySnapshot(ctx, &s.mu, func() ([]*Position, []*Order) { return s.positions, s.orders })
}

func (s *SpotShortStrategy) CapitalReleaseInventorySnapshot(ctx context.Context) ([]*Position, []*Order, error) {
	return capitalReleaseInventorySnapshot(ctx, &s.mu, func() ([]*Position, []*Order) { return s.positions, s.orders })
}

func (s *FuturesLongStrategy) CapitalReleaseInventorySnapshot(ctx context.Context) ([]*Position, []*Order, error) {
	return capitalReleaseInventorySnapshot(ctx, &s.mu, func() ([]*Position, []*Order) { return s.positions, s.orders })
}

func (s *FuturesShortStrategy) CapitalReleaseInventorySnapshot(ctx context.Context) ([]*Position, []*Order, error) {
	return capitalReleaseInventorySnapshot(ctx, &s.mu, func() ([]*Position, []*Order) { return s.positions, s.orders })
}
