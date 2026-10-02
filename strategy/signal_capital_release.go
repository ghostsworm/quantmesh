package strategy

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"quantmesh/config"
)

func (s *TrendFollowingStrategy) VerifyCapitalReleaseState(ctx context.Context) error {
	if err := acquireCapitalProofLock(ctx, s.mu.TryRLock, s.mu.RUnlock); err != nil {
		return err
	}
	defer s.mu.RUnlock()
	return verifySignalCapitalRelease(ctx, s.cfg, s.runtimeStateStore, signalRuntimeState{
		StrategyName: s.name, Symbol: signalStrategySymbol(s.cfg, s.strategyCfg),
		Position: s.position, EntryPrice: s.entryPrice, ActiveOrder: s.activeOrder, PendingAction: s.pendingAction,
	}, s.runtimeStateErr)
}

func (s *MeanReversionStrategy) VerifyCapitalReleaseState(ctx context.Context) error {
	if err := acquireCapitalProofLock(ctx, s.mu.TryRLock, s.mu.RUnlock); err != nil {
		return err
	}
	defer s.mu.RUnlock()
	return verifySignalCapitalRelease(ctx, s.cfg, s.runtimeStateStore, signalRuntimeState{
		StrategyName: s.name, Symbol: signalStrategySymbol(s.cfg, s.strategyCfg),
		Position: s.position, EntryPrice: s.entryPrice, ActiveOrder: s.activeOrder, PendingAction: s.pendingAction,
	}, s.runtimeStateErr)
}

func (s *MomentumStrategy) VerifyCapitalReleaseState(ctx context.Context) error {
	if err := acquireCapitalProofLock(ctx, s.mu.TryRLock, s.mu.RUnlock); err != nil {
		return err
	}
	defer s.mu.RUnlock()
	return verifySignalCapitalRelease(ctx, s.cfg, s.runtimeStateStore, signalRuntimeState{
		StrategyName: s.name, Symbol: signalStrategySymbol(s.cfg, s.strategyCfg),
		Position: s.position, EntryPrice: s.entryPrice, ActiveOrder: s.activeOrder, PendingAction: s.pendingAction,
	}, s.runtimeStateErr)
}

// Caller holds the runtime submission barriers and verifies live inventory
// independently. Read recovery state without applying, migrating or clearing it.
func verifySignalCapitalRelease(ctx context.Context, cfg *config.Config, store RuntimeStateStore, memory signalRuntimeState, stateErr error) error {
	if ctx == nil {
		return fmt.Errorf("signal capital release verification requires context")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if stateErr != nil {
		return fmt.Errorf("signal accounting requires reconciliation: %w", stateErr)
	}
	if err := verifySignalReleaseFlat(memory); err != nil {
		return err
	}
	if cfg == nil || strings.TrimSpace(cfg.Trading.BotID) == "" || memory.StrategyName == "" || memory.Symbol == "" {
		return fmt.Errorf("signal durable owner identity is unavailable")
	}
	reader, ok := store.(RuntimeStateContextReader)
	if !ok {
		return fmt.Errorf("signal capital release requires cancellable state storage")
	}
	version, payload, found, err := reader.LoadRuntimeStateContext(ctx, memory.StrategyName)
	if err != nil {
		return fmt.Errorf("read signal state before capital release: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if !found {
		return nil
	}
	if version != signalRuntimeStateSchemaVersion {
		return fmt.Errorf("signal state schema requires recovery")
	}
	var state signalRuntimeState
	if err := json.Unmarshal([]byte(payload), &state); err != nil {
		return fmt.Errorf("decode signal state before capital release: %w", err)
	}
	if state.BotID != strings.TrimSpace(cfg.Trading.BotID) || state.StrategyName != memory.StrategyName || state.Symbol != memory.Symbol {
		return fmt.Errorf("signal durable owner identity is mismatched")
	}
	return verifySignalReleaseFlat(state)
}

func verifySignalReleaseFlat(state signalRuntimeState) error {
	if state.Position != nil || state.ActiveOrder != nil || state.PendingAction != "" || state.OrderAlias != "" || !signalFinite(state.EntryPrice) || state.EntryPrice != 0 {
		return fmt.Errorf("signal inventory or order state requires reconciliation")
	}
	return nil
}
