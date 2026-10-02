package strategy

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"quantmesh/config"
)

func (s *FuturesLongStrategy) VerifyCapitalReleaseState(ctx context.Context) error {
	return verifyFuturesHedgeCapitalRelease(ctx, s.orderTracker, s.cfg, s.name, s.groupID, s.symbol)
}

func (s *FuturesShortStrategy) VerifyCapitalReleaseState(ctx context.Context) error {
	return verifyFuturesHedgeCapitalRelease(ctx, s.orderTracker, s.cfg, s.name, s.groupID, s.symbol)
}

// Generic hedge snapshots omit this tracker. Verify it without applying,
// querying/reconciling, migrating or clearing recovery evidence.
func verifyFuturesHedgeCapitalRelease(ctx context.Context, tracker *futuresHedgeOrderTracker, cfg *config.Config, name, groupID, symbol string) error {
	if ctx == nil {
		return fmt.Errorf("futures hedge capital release requires context")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if tracker == nil || cfg == nil || strings.TrimSpace(cfg.Trading.BotID) == "" || name == "" || symbol == "" {
		return fmt.Errorf("futures hedge owner or tracker evidence is unavailable")
	}
	if err := acquireCapitalProofLock(ctx, tracker.mu.TryLock, tracker.mu.Unlock); err != nil {
		return err
	}
	defer tracker.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if tracker.botID != cfg.Trading.BotID || tracker.strategy != name || tracker.groupID != groupID || tracker.symbol != symbol {
		return fmt.Errorf("futures hedge tracker owner binding is mismatched")
	}
	if tracker.pending != nil {
		return fmt.Errorf("futures hedge order requires reconciliation")
	}
	reader, ok := tracker.store.(RuntimeStateContextReader)
	if !ok {
		return fmt.Errorf("futures hedge release requires cancellable state storage")
	}
	version, payload, found, err := reader.LoadRuntimeStateContext(ctx, name)
	if err != nil {
		return fmt.Errorf("read futures hedge state before capital release: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if !found {
		return nil
	}
	if version != futuresHedgeRuntimeStateVersion {
		return fmt.Errorf("futures hedge state schema requires recovery")
	}
	var state futuresHedgeRuntimeState
	if err := json.Unmarshal([]byte(payload), &state); err != nil {
		return fmt.Errorf("decode futures hedge state before capital release: %w", err)
	}
	if state.BotID != cfg.Trading.BotID || state.Strategy != name || state.GroupID != groupID || state.Symbol != symbol {
		return fmt.Errorf("futures hedge durable owner identity is mismatched")
	}
	if state.Pending != nil {
		return fmt.Errorf("durable futures hedge order requires reconciliation")
	}
	return nil
}
