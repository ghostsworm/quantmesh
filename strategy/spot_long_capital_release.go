package strategy

import (
	"context"
	"encoding/json"
	"fmt"
)

// VerifyCapitalReleaseState supplements generic inventory snapshots, which do
// not expose SpotLong's prepared/unresolved order recovery state. The caller
// holds runtime submission barriers and independently verifies live inventory.
// This proof never restores, migrates, or clears recovery state.
func (s *SpotLongStrategy) VerifyCapitalReleaseState(ctx context.Context) error {
	if ctx == nil {
		return fmt.Errorf("SpotLong capital release verification requires context")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := acquireCapitalProofLock(ctx, s.mu.TryRLock, s.mu.RUnlock); err != nil {
		return err
	}
	defer s.mu.RUnlock()
	if len(s.pendingOrders) != 0 || len(s.pendingIntents) != 0 {
		return fmt.Errorf("SpotLong order intent requires reconciliation")
	}
	if s.runtimeStateStore == nil || spotLongBotID(s.cfg) == "" || s.name == "" || s.symbol == "" || s.baseAsset == "" {
		return fmt.Errorf("SpotLong durable order owner evidence is unavailable")
	}
	reader, ok := s.runtimeStateStore.(RuntimeStateContextReader)
	if !ok {
		return fmt.Errorf("SpotLong order storage requires cancellable reads before capital release")
	}
	version, payload, found, err := reader.LoadRuntimeStateContext(ctx, s.name)
	if err != nil {
		return fmt.Errorf("read SpotLong order state before capital release: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if !found {
		return nil
	}
	if version != spotLongRuntimeStateSchemaVersion {
		return fmt.Errorf("SpotLong order state schema requires recovery")
	}
	var state spotLongRuntimeState
	if err := json.Unmarshal([]byte(payload), &state); err != nil {
		return fmt.Errorf("decode SpotLong order state before capital release: %w", err)
	}
	if state.BotID != spotLongBotID(s.cfg) || state.Strategy != s.name || state.GroupID != s.groupID || state.Symbol != s.symbol || state.BaseAsset != s.baseAsset {
		return fmt.Errorf("SpotLong durable order owner identity is mismatched")
	}
	if len(state.PendingOrders) != 0 || len(state.PendingIntents) != 0 {
		return fmt.Errorf("SpotLong durable order intent requires reconciliation")
	}
	return nil
}
