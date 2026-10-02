package strategy

import (
	"context"
	"encoding/json"
	"fmt"
)

// VerifyCapitalReleaseState is called while the runtime holds the account
// wallet coordination lease. Generic positions/orders omit these debt intents.
// Re-read storage without restoring/migrating or clearing any recovery state.
func (s *SpotShortStrategy) VerifyCapitalReleaseState(ctx context.Context) error {
	if ctx == nil {
		return fmt.Errorf("SpotShort capital release verification requires context")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := acquireCapitalProofLock(ctx, s.mu.TryRLock, s.mu.RUnlock); err != nil {
		return err
	}
	defer s.mu.RUnlock()
	if len(s.pendingBorrow) != 0 || len(s.pendingRepay) != 0 || len(s.pendingBuy) != 0 {
		return fmt.Errorf("SpotShort debt or buy intent requires reconciliation")
	}
	if s.runtimeStateStore == nil || spotShortBotID(s.cfg) == "" || s.name == "" || s.symbol == "" || s.baseAsset == "" {
		return fmt.Errorf("SpotShort durable debt owner evidence is unavailable")
	}
	reader, ok := s.runtimeStateStore.(RuntimeStateContextReader)
	if !ok {
		return fmt.Errorf("SpotShort debt storage requires cancellable reads before capital release")
	}
	version, payload, found, err := reader.LoadRuntimeStateContext(ctx, s.name)
	if err != nil {
		return fmt.Errorf("read SpotShort debt state before capital release: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if !found {
		return nil
	} // No durable intent; wallet lease and live account proof are still required.
	if version != spotShortRuntimeStateSchemaVersion {
		return fmt.Errorf("SpotShort debt state schema requires recovery")
	}
	var state spotShortRuntimeState
	if err := json.Unmarshal([]byte(payload), &state); err != nil {
		return fmt.Errorf("decode SpotShort debt state before capital release: %w", err)
	}
	if state.BotID != spotShortBotID(s.cfg) || state.Strategy != s.name || state.GroupID != s.groupID || state.Symbol != s.symbol || state.BaseAsset != s.baseAsset {
		return fmt.Errorf("SpotShort durable debt owner identity is mismatched")
	}
	if len(state.PendingBorrow) != 0 || len(state.PendingRepay) != 0 || len(state.PendingBuy) != 0 {
		return fmt.Errorf("SpotShort durable debt or buy intent requires reconciliation")
	}
	return nil
}
