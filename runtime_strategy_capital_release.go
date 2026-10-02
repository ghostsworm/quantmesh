package main

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"

	"quantmesh/strategy"
	"quantmesh/web"
)

func newRuntimeStrategyCapitalProvider(allocation func() map[string]web.StrategyCapitalInfo, runtimes func() []*SymbolRuntime) web.StrategyProvider {
	return web.NewVerifiedStrategyProviderAdapter(allocation,
		func(ctx context.Context, name string) (float64, error) {
			released, err := releaseRuntimeStrategyCapital(ctx, runtimes(), name)
			return released[name], err
		},
		func(ctx context.Context) (map[string]float64, error) {
			return releaseRuntimeStrategyCapital(ctx, runtimes(), "")
		})
}

// Each runtime is verified and committed separately. Never claim a distributed
// all-or-nothing release: the HTTP contract exposes actual partial amounts.
// Do not hold multiple leases for a shared exchange/symbol (self-deadlock).
func releaseRuntimeStrategyCapital(ctx context.Context, runtimes []*SymbolRuntime, name string) (map[string]float64, error) {
	released := make(map[string]float64)
	if ctx == nil {
		return released, fmt.Errorf("runtime capital release requires context")
	}
	var failures []error
	matched := false
	for _, rt := range runtimes {
		if err := ctx.Err(); err != nil {
			failures = append(failures, err)
			break
		}
		if rt == nil {
			failures = append(failures, fmt.Errorf("runtime capital release encountered missing runtime"))
			continue
		}
		if rt.StrategyManager == nil {
			continue
		} // Specialized runtimes without this allocator own no strategy rows.
		allocator := rt.StrategyManager.GetCapitalAllocator()
		if allocator == nil {
			failures = append(failures, fmt.Errorf("runtime capital allocator is unavailable"))
			continue
		}
		hasTarget, err := allocator.HasCapitalReleaseTarget(ctx, name)
		if err != nil {
			failures = append(failures, err)
			continue
		}
		if !hasTarget {
			continue
		}
		matched = true
		amounts, err := releaseOneRuntimeStrategyCapital(ctx, rt, name)
		for strategyName, amount := range amounts {
			released[strategyName] += amount
		}
		if err != nil {
			failures = append(failures, err)
		}
	}
	if name != "" && !matched {
		failures = append(failures, fmt.Errorf("requested strategy capital was not found"))
	}
	return released, errors.Join(failures...)
}

func releaseOneRuntimeStrategyCapital(ctx context.Context, rt *SymbolRuntime, name string) (map[string]float64, error) {
	if rt.CapitalExecutor == nil || rt.ExchangeExecutor == nil || rt.Exchange == nil {
		return nil, fmt.Errorf("runtime capital release requires reservation and physical submission barriers")
	}
	if err := verifyRuntimeStrategyCapitalBinding(ctx, rt); err != nil {
		return nil, err
	}
	if rt.capitalReleaseScope.Market == "spot" || rt.capitalReleaseScope.Market == "spot_margin" {
		var amounts map[string]float64
		err := strategy.WithAccountWalletCoordination(ctx, rt.capitalReleaseWalletLock, "funding_carry_wallet:"+rt.capitalReleaseScope.Account, func(walletCtx context.Context) error {
			var releaseErr error
			amounts, releaseErr = releaseCoordinatedRuntimeStrategyCapital(walletCtx, rt, name)
			return releaseErr
		})
		// A committed release followed by unlock failure must retain the actual
		// amounts, so callers cannot misreport "nothing changed".
		return amounts, err
	}
	return releaseCoordinatedRuntimeStrategyCapital(ctx, rt, name)
}

func releaseCoordinatedRuntimeStrategyCapital(ctx context.Context, rt *SymbolRuntime, name string) (released map[string]float64, resultErr error) {
	// Drain logical placements first. A placement may already own a reservation
	// while waiting for the position lease that the snapshot will acquire next.
	releaseCapitalBarrier, err := rt.CapitalExecutor.BeginCapitalReconciliation(ctx)
	if err != nil {
		return nil, err
	}
	defer releaseCapitalBarrier()
	snapshotCtx, releaseSnapshot, err := rt.ExchangeExecutor.BeginCapitalReleaseSnapshot(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, releaseSnapshot()) }()
	return rt.StrategyManager.GetCapitalAllocator().ReleaseVerified(snapshotCtx, name, func(proofCtx context.Context) error {
		if err := verifyRuntimeStrategyCapitalOwner(proofCtx, rt); err != nil {
			return err
		}
		verifyInventory := func() error {
			return verifyStandardSpotBotInventoryFlatContext(proofCtx, rt.SuperPositionManager, rt.StrategyManager, rt.capitalReleaseScope.Symbol)
		}
		switch strings.ToLower(strings.TrimSpace(rt.AccountMarketType)) {
		case "futures":
			err = verifyStandardRuntimeFlat(proofCtx, rt.Exchange, rt.capitalReleaseScope.Market, rt.capitalReleaseScope.Symbol)
		case "spot":
			err = verifyStandardSpotRuntimeFlat(proofCtx, rt.Exchange, rt.capitalReleaseScope.Symbol, verifyInventory)
		case "spot_margin":
			err = verifyStandardSpotMarginRuntimeFlat(proofCtx, rt.Exchange, verifyInventory)
		default:
			err = fmt.Errorf("runtime capital release market is not verified")
		}
		if err != nil {
			return err
		}
		// Recheck owner evidence after slow venue reads and before committing.
		return verifyRuntimeStrategyCapitalOwner(proofCtx, rt)
	})
}

func verifyRuntimeStrategyCapitalBinding(ctx context.Context, rt *SymbolRuntime) error {
	if rt.capitalReleaseScope.Account != rt.AccountScope || rt.capitalReleaseScope.Market != rt.AccountMarketType {
		return fmt.Errorf("runtime capital release account or market binding is mismatched")
	}
	if err := rt.CapitalExecutor.VerifyCapitalReleaseBinding(rt.StrategyManager.GetCapitalAllocator(), rt.ExchangeExecutor); err != nil {
		return err
	}
	return rt.ExchangeExecutor.VerifyCapitalReleaseOwner(ctx, rt.capitalReleaseScope, rt.Exchange)
}

func verifyRuntimeStrategyCapitalOwner(ctx context.Context, rt *SymbolRuntime) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := verifyRuntimeStrategyCapitalBinding(ctx, rt); err != nil {
		return err
	}
	if err := rt.CapitalExecutor.VerifyCapitalReleaseAccounting(ctx); err != nil {
		return err
	}
	if err := rt.ExchangeExecutor.VerifyCapitalReleaseIntents(ctx); err != nil {
		return err
	}
	registered, err := rt.StrategyManager.GetAllStrategiesContext(ctx)
	if err != nil {
		return err
	}
	for name, current := range registered {
		if capitalReleaseStrategyMissing(current) {
			return fmt.Errorf("capital release strategy %s is unavailable", name)
		}
		if _, grid := current.(*strategy.GridStrategy); grid {
			// Grid has no separate private strategy ledger. Its exact startup
			// manager binding and complete slot proof are checked below.
			continue
		}
		verifier, ok := current.(strategy.CapitalReleaseStateVerifier)
		if !ok {
			return fmt.Errorf("capital release strategy %s lacks private accounting proof", name)
		}
		if err := verifier.VerifyCapitalReleaseState(ctx); err != nil {
			return err
		}
	}
	if err := verifyStandardSpotBotInventoryFlatContext(ctx, rt.SuperPositionManager, rt.StrategyManager, rt.capitalReleaseScope.Symbol); err != nil {
		return err
	}
	exposure, err := rt.ExchangeExecutor.CapitalReleaseExposureSnapshot(ctx)
	if err != nil {
		return err
	}
	if exposure == nil || !exposure.Ready || exposure.Layers != 0 {
		return fmt.Errorf("runtime exposure ownership is not verifiably empty")
	}
	for _, quantity := range []float64{exposure.PositionQuantity, exposure.PendingQuantity, exposure.ProjectedQuantity, exposure.ProjectedNotional} {
		if math.IsNaN(quantity) || math.IsInf(quantity, 0) || quantity != 0 {
			return fmt.Errorf("runtime exposure ownership remains nonempty or invalid")
		}
	}
	return ctx.Err()
}
