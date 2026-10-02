package strategy

import (
	"context"
	"fmt"
	"sort"
)

// HasCapitalReleaseTarget reports whether this allocator owns the requested row.
// It is only a routing hint, not proof of flatness or a monetary snapshot.
// ReleaseVerified rechecks identity/revision and amounts under its own locks.
func (ca *CapitalAllocator) HasCapitalReleaseTarget(ctx context.Context, name string) (bool, error) {
	if err := acquireCapitalProofLock(ctx, ca.mu.TryRLock, ca.mu.RUnlock); err != nil {
		return false, err
	}
	defer ca.mu.RUnlock()
	if name == "" {
		return len(ca.strategies) > 0, ctx.Err()
	}
	_, exists := ca.strategies[name]
	return exists, ctx.Err()
}

// ReleaseVerified clears stale capital only after the caller proves venue and
// durable owner state flat while holding submission/coordination barriers.
// Verification runs without allocator locks: strategy snapshots may themselves
// require capital accounting. Every intervening mutation invalidates the proof,
// including reserve/release churn that leaves the same final numeric balance.
// An empty strategy name selects all strategies in this allocator atomically.
func (ca *CapitalAllocator) ReleaseVerified(ctx context.Context, strategyName string, verifyFlat func(context.Context) error) (map[string]float64, error) {
	if ctx == nil || verifyFlat == nil {
		return nil, fmt.Errorf("capital release requires context and authoritative flatness verification")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := acquireCapitalProofLock(ctx, ca.mu.TryRLock, ca.mu.RUnlock); err != nil {
		return nil, err
	}
	revision := ca.revision
	_, exists := ca.strategies[strategyName]
	ca.mu.RUnlock()
	if strategyName != "" && !exists {
		return nil, fmt.Errorf("capital release strategy %s is not registered", strategyName)
	}
	if err := verifyFlat(ctx); err != nil {
		return nil, fmt.Errorf("verify flat state before releasing strategy capital: %w", err)
	}
	if err := acquireCapitalProofLock(ctx, ca.mu.TryLock, ca.mu.Unlock); err != nil {
		return nil, err
	}
	defer ca.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if ca.revision != revision {
		return nil, fmt.Errorf("capital accounting changed during flatness verification; reconciliation must be repeated")
	}
	names := make([]string, 0, len(ca.strategies))
	seen := make(map[*StrategyCapital]bool)
	for name, capital := range ca.strategies {
		if strategyName != "" && name != strategyName {
			continue
		}
		if capital == nil || seen[capital] {
			return nil, fmt.Errorf("capital accounting for strategy %s has invalid row identity", name)
		}
		seen[capital] = true
		names = append(names, name)
	}
	sort.Strings(names)
	locked := make([]*StrategyCapital, 0, len(names))
	defer func() {
		for i := len(locked) - 1; i >= 0; i-- {
			locked[i].mu.Unlock()
		}
	}()
	// Acquire every selected row before mutation. Cancellation while waiting
	// for a later row releases earlier locks without partially clearing capital.
	for _, name := range names {
		capital := ca.strategies[name]
		if err := acquireCapitalProofLock(ctx, capital.mu.TryLock, capital.mu.Unlock); err != nil {
			return nil, err
		}
		locked = append(locked, capital)
		if !finiteNumber(capital.Used) || capital.Used < 0 || !finiteNumber(capital.Allocated) || capital.Allocated < 0 {
			return nil, fmt.Errorf("capital accounting for strategy %s is invalid", name)
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// Commit boundary: all locks and evidence are held. Once mutation starts,
	// finish the entire commit and report known amounts, even if ctx cancels.
	released := make(map[string]float64, len(names))
	for _, name := range names {
		capital := ca.strategies[name]
		released[name] = capital.Used
		capital.Used = 0
		capital.Available = capital.Allocated
	}
	ca.revision++
	return released, nil
}
