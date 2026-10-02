package strategy

import (
	"context"
	"fmt"
)

func (s *DCAEnhancedStrategy) CapitalReleaseInventorySnapshot(ctx context.Context) ([]*Position, []*Order, error) {
	if err := acquireCapitalProofLock(ctx, s.mu.TryRLock, s.mu.RUnlock); err != nil {
		return nil, nil, err
	}
	defer s.mu.RUnlock()
	if s.strategyCfg == nil {
		return nil, nil, fmt.Errorf("DCA inventory configuration is unavailable")
	}
	if !finiteNumber(s.totalQty) || s.totalQty < 0 {
		return nil, nil, fmt.Errorf("DCA inventory quantity is invalid")
	}
	for _, layer := range s.layers {
		if layer == nil {
			return nil, nil, fmt.Errorf("DCA inventory layer is invalid")
		}
	}
	positions, orders := s.inventoryPositionsLocked(), s.inventoryOrdersLocked()
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	return positions, orders, nil
}

func (s *MartingaleStrategy) CapitalReleaseInventorySnapshot(ctx context.Context) ([]*Position, []*Order, error) {
	if err := acquireCapitalProofLock(ctx, s.mu.TryRLock, s.mu.RUnlock); err != nil {
		return nil, nil, err
	}
	defer s.mu.RUnlock()
	if s.strategyCfg == nil {
		return nil, nil, fmt.Errorf("Martingale inventory configuration is unavailable")
	}
	if !finiteNumber(s.totalQty) || s.totalQty < 0 {
		return nil, nil, fmt.Errorf("Martingale inventory quantity is invalid")
	}
	for _, entry := range s.entries {
		if entry == nil {
			return nil, nil, fmt.Errorf("Martingale inventory entry is invalid")
		}
	}
	positions, orders := s.inventoryPositionsLocked(), s.inventoryOrdersLocked()
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	return positions, orders, nil
}
