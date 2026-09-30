package main

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"quantmesh/config"
	"quantmesh/logger"
	"quantmesh/order"
)

const (
	runtimeShutdownPrepareTimeout = 30 * time.Second
	processShutdownTotalTimeout   = 5 * time.Minute
)

// Freeze API-triggered start/stop transitions before taking the authoritative
// shutdown inventory. In-flight startup may create an executor, so sealing only
// one early List() snapshot would miss it. Timeout is not proof of quiescence.
func (bm *BotManager) sealProcessRuntimes(ctx context.Context) ([]*SymbolRuntime, error) {
	bm.runtimeAdmissions.Block(order.RuntimeShutdownBlock)
	for _, rt := range bm.ListSymbolRuntimes() {
		sealRuntimeShutdown(rt)
	}
	err := bm.runtimeAdmissions.Drain(ctx)
	if ctx.Err() != nil {
		err = ctx.Err()
	}
	if bm.shutdownTransitionUnverified.Load() {
		err = errors.Join(err, fmt.Errorf("overlapping Bot transition requires reconciliation"))
	}
	runtimes := bm.ListSymbolRuntimes()
	for _, rt := range runtimes {
		sealRuntimeShutdown(rt)
		if err != nil {
			rt.markShutdownCloseUnverified("Bot 生命周期尚未排空，禁止追加账户平仓")
		}
	}
	return runtimes, err
}

func sealRuntimeShutdown(rt *SymbolRuntime) {
	if rt == nil {
		return
	}
	if rt.SuperPositionManager != nil {
		rt.SuperPositionManager.OpeningGate().Block(order.RuntimeShutdownBlock)
	}
	if rt.OpeningGate != nil {
		rt.OpeningGate.Block(order.RuntimeShutdownBlock)
	}
	if rt.ExchangeExecutor != nil {
		rt.ExchangeExecutor.BeginShutdown()
	}
}

// Keep order streams alive while draining/cancelling so accepted fills can
// reach economic consumers. Every runtime is sealed before any network wait.
// A failure poisons the account/symbol close group, not just its first Bot.
func prepareProcessShutdown(ctx context.Context, runtimes []*SymbolRuntime, cancelOrders bool) error {
	for _, rt := range runtimes {
		sealRuntimeShutdown(rt)
	}
	failed := make(map[string]string)
	var problems []error
	for _, rt := range runtimes {
		if rt == nil {
			continue
		}
		qctx, cancel := context.WithTimeout(ctx, runtimeShutdownPrepareTimeout)
		err := prepareRuntimeShutdown(qctx, rt, cancelOrders)
		cancel()
		if err != nil {
			reason := fmt.Sprintf("退出准备未核实，禁止追加平仓：%v", err)
			failed[shutdownRuntimeScopeKey(rt)] = reason
			problems = append(problems, fmt.Errorf("runtime %s: %w", rt.Config.Symbol, err))
		}
	}
	for _, rt := range runtimes {
		if rt != nil {
			if reason := failed[shutdownRuntimeScopeKey(rt)]; reason != "" {
				rt.markShutdownCloseUnverified(reason)
			}
		}
	}
	return errors.Join(problems...)
}

func prepareRuntimeShutdown(ctx context.Context, rt *SymbolRuntime, cancelOrders bool) error {
	if rt.PrepareShutdown != nil {
		return rt.PrepareShutdown(ctx, cancelOrders)
	}
	if rt.ExchangeExecutor == nil || rt.SuperPositionManager == nil {
		return fmt.Errorf("runtime has no verified submission barrier")
	}
	if rt.DynamicAdjuster != nil {
		if err := rt.DynamicAdjuster.StopContext(ctx); err != nil {
			return fmt.Errorf("volatility controller not stopped: %w", err)
		}
	}
	if err := rt.stopManagedClose(ctx); err != nil {
		return fmt.Errorf("managed close worker not stopped: %w", err)
	}
	if err := rt.ExchangeExecutor.DrainShutdown(ctx); err != nil {
		return fmt.Errorf("ordinary submissions have not drained: %w", err)
	}
	if err := rt.SuperPositionManager.StopProtectiveLiquidation(ctx); err != nil {
		return fmt.Errorf("protective liquidation has not settled: %w", err)
	}
	if cancelOrders {
		if err := rt.ExchangeExecutor.CancelOwnedShutdownOrders(ctx); err != nil {
			return fmt.Errorf("owned cancellation unverified: %w", err)
		}
		logger.Info("[%s] 已核实本运行时自有委托终止；其他 Bot/人工委托保持不变", rt.Config.Symbol)
	}
	return nil
}

// closeProcessRuntimeGroup reconciles the full account scope, then liquidates
// each process-owned Bot through its own journal. Account-level direct orders
// cannot preserve per-Bot fills, costs, or restart recovery.
func closeProcessRuntimeGroup(ctx context.Context, runtimes []*SymbolRuntime) error {
	if len(runtimes) == 0 {
		return fmt.Errorf("empty shutdown runtime group")
	}
	for _, rt := range runtimes {
		if rt != nil && rt.CloseForShutdown != nil {
			if len(runtimes) != 1 || rt.AccountScope == "" || rt.shutdownCloseUnverifiedReason() != "" {
				return fmt.Errorf("specialized strategy shutdown requires a sole, verified runtime owner")
			}
			if err := rt.CloseForShutdown(ctx); err != nil {
				return errors.Join(errShutdownCloseUnverified, fmt.Errorf("close specialized Bot-owned positions: %w", err))
			}
			if rt.VerifyShutdownClose != nil {
				if err := rt.VerifyShutdownClose(ctx); err != nil {
					return errors.Join(errShutdownCloseUnverified, fmt.Errorf("verify specialized Bot-owned positions: %w", err))
				}
				return nil
			}
			remaining, err := queryShutdownPositions(ctx, rt.Exchange, rt.Config.Symbol)
			if err != nil {
				return errors.Join(errShutdownCloseUnverified, fmt.Errorf("verify specialized futures position: %w", err))
			}
			if len(remaining) != 0 {
				return errors.Join(errShutdownCloseUnverified, fmt.Errorf("specialized strategy left %d futures position legs", len(remaining)))
			}
			return nil
		}
	}
	var processOwners, botOwners []*SymbolRuntime
	for _, rt := range runtimes {
		if rt == nil || rt.Exchange == nil || rt.ExchangeExecutor == nil || rt.SuperPositionManager == nil || rt.AccountScope == "" {
			return fmt.Errorf("shutdown ownership evidence unavailable for runtime")
		}
		if rt.shutdownCloseUnverifiedReason() != "" {
			return fmt.Errorf("shutdown runtime already requires reconciliation: %s", rt.shutdownCloseUnverifiedReason())
		}
		if decideShutdownCloseOwner(true, rt.Config) == shutdownCloseProcess {
			processOwners = append(processOwners, rt)
		} else {
			botOwners = append(botOwners, rt)
		}
	}
	if len(processOwners) == 0 {
		return nil
	}
	venue := runtimes[0].Exchange
	symbol := runtimes[0].Config.Symbol
	var accountLong, accountShort float64
	positions, err := queryShutdownPositions(ctx, venue, symbol)
	if err != nil {
		return fmt.Errorf("shutdown account position preflight: %w", err)
	}
	for _, p := range positions {
		if p.Size > 0 {
			accountLong += p.Size
		} else {
			accountShort += -p.Size
		}
	}
	var ownedLong, ownedShort float64
	for _, rt := range runtimes {
		long, short, err := rt.SuperPositionManager.GetPositionLegQuantities()
		if err != nil {
			return fmt.Errorf("shutdown Bot %s inventory ledger is unverifiable: %w", rt.Config.ID, err)
		}
		ownedLong += long
		ownedShort += short
		if math.IsNaN(ownedLong) || math.IsInf(ownedLong, 0) || math.IsNaN(ownedShort) || math.IsInf(ownedShort, 0) {
			return fmt.Errorf("shutdown Bot inventory aggregate is invalid or overflowing")
		}
	}
	tolerance := shutdownQuantityTolerance(venue.GetQuantityDecimals())
	if math.Abs(accountLong-ownedLong) > tolerance || math.Abs(accountShort-ownedShort) > tolerance {
		return fmt.Errorf("shutdown account exposure does not reconcile with Bot ledgers: exchange long/short %.12g/%.12g, owned %.12g/%.12g", accountLong, accountShort, ownedLong, ownedShort)
	}
	openOrders, err := venue.GetOpenOrders(ctx, symbol)
	if err != nil {
		return fmt.Errorf("shutdown account open-order preflight: %w", err)
	}
	if openOrders == nil {
		return fmt.Errorf("shutdown account open-order preflight returned nil, not an authoritative empty snapshot")
	}
	for _, order := range openOrders {
		if order == nil {
			return fmt.Errorf("shutdown account open-order preflight contains an unverifiable nil order")
		}
		if order.Symbol == "" || !strings.EqualFold(order.Symbol, symbol) {
			return fmt.Errorf("shutdown account open-order preflight returned an out-of-scope order for %q", symbol)
		}
		return fmt.Errorf("shutdown account still has an order not proven terminal after owned cancellation")
	}
	completed := false
	for _, rt := range processOwners {
		shutdownCtx, err := rt.ExchangeExecutor.ShutdownCloseContext(ctx)
		if err != nil {
			if completed {
				return errors.Join(errShutdownCloseUnverified, fmt.Errorf("grant owner-scoped shutdown close for Bot %s after another Bot closed: %w", rt.Config.ID, err))
			}
			return fmt.Errorf("grant owner-scoped shutdown close for Bot %s: %w", rt.Config.ID, err)
		}
		botConfig := config.SymbolConfigToBotConfig(rt.Config, false)
		botID := rt.Config.ID
		if botID == "" {
			botID = config.BotIDOrGenerate(botConfig)
		}
		bot := &BotRuntime{BotID: botID, Config: botConfig, Inner: rt}
		if err := bot.CloseAllPositions(shutdownCtx, "process shutdown", 20); err != nil {
			return errors.Join(errShutdownCloseUnverified, fmt.Errorf("close Bot %s through its execution journal: %w", botID, err))
		}
		completed = true
	}
	remaining, err := queryShutdownPositions(ctx, venue, symbol)
	if err != nil {
		if completed {
			return errors.Join(errShutdownCloseUnverified, fmt.Errorf("verify shutdown residual positions: %w", err))
		}
		return err
	}
	var remainingLong, remainingShort float64
	for _, p := range remaining {
		if p.Size > 0 {
			remainingLong += p.Size
		} else {
			remainingShort += -p.Size
		}
	}
	var expectedLong, expectedShort float64
	for _, rt := range botOwners {
		long, short, err := rt.SuperPositionManager.GetPositionLegQuantities()
		if err != nil {
			return errors.Join(errShutdownCloseUnverified, fmt.Errorf("shutdown residual Bot %s inventory is unverifiable: %w", rt.Config.ID, err))
		}
		expectedLong += long
		expectedShort += short
		if math.IsNaN(expectedLong) || math.IsInf(expectedLong, 0) || math.IsNaN(expectedShort) || math.IsInf(expectedShort, 0) {
			return errors.Join(errShutdownCloseUnverified, fmt.Errorf("shutdown residual Bot inventory aggregate is invalid or overflowing"))
		}
	}
	if math.Abs(remainingLong-expectedLong) > tolerance || math.Abs(remainingShort-expectedShort) > tolerance {
		return errors.Join(errShutdownCloseUnverified, fmt.Errorf("shutdown residual does not match Bot-owned close_on_stop inventory: exchange %.12g/%.12g, expected %.12g/%.12g", remainingLong, remainingShort, expectedLong, expectedShort))
	}
	return nil
}

func shutdownQuantityTolerance(decimals int) float64 {
	if decimals < 0 || decimals > 18 {
		return 1e-8
	}
	return math.Pow10(-decimals) / 2
}
