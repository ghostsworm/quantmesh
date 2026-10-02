package strategy

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// VerifyCapitalReleaseState supplements generic snapshots with durable entry,
// close and fee cursors, without applying/migrating/clearing recovery evidence.
func (s *DCAEnhancedStrategy) VerifyCapitalReleaseState(ctx context.Context) error {
	if ctx == nil {
		return fmt.Errorf("DCA capital release verification requires context")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := acquireCapitalProofLock(ctx, s.mu.TryRLock, s.mu.RUnlock); err != nil {
		return err
	}
	defer s.mu.RUnlock()
	if s.runtimeStateErr != nil {
		return fmt.Errorf("DCA accounting requires reconciliation: %w", s.runtimeStateErr)
	}
	// The serializer skips nil layers. Safety evidence must not drop them.
	if len(s.layers) != 0 || s.closeLayer != nil {
		return fmt.Errorf("DCA layers or close target require reconciliation")
	}
	if err := verifyDCAReleaseFlat(s.runtimeStateSnapshotLocked()); err != nil {
		return err
	}
	if s.cfg == nil || strings.TrimSpace(s.cfg.Trading.BotID) == "" || s.name == "" || s.strategyCfg.Symbol == "" {
		return fmt.Errorf("DCA durable owner identity is unavailable")
	}
	reader, ok := s.runtimeStateStore.(RuntimeStateContextReader)
	if !ok {
		return fmt.Errorf("DCA capital release requires cancellable state storage")
	}
	version, payload, found, err := reader.LoadRuntimeStateContext(ctx, s.name)
	if err != nil {
		return fmt.Errorf("read DCA state before capital release: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if !found {
		return nil
	}
	if version != dcaRuntimeStateSchemaVersion {
		return fmt.Errorf("DCA state schema requires recovery")
	}
	var state dcaRuntimeState
	if err := json.Unmarshal([]byte(payload), &state); err != nil {
		return fmt.Errorf("decode DCA state before capital release: %w", err)
	}
	if state.BotID != s.effectiveBotID() || state.StrategyName != s.name || state.Symbol != s.strategyCfg.Symbol {
		return fmt.Errorf("DCA durable owner identity is mismatched")
	}
	return verifyDCAReleaseFlat(state)
}

func verifyDCAReleaseFlat(state dcaRuntimeState) error {
	if len(state.Layers) != 0 || state.CurrentLayer != 0 || state.IsClosing || state.CloseOrderID != 0 || state.CloseClientOrderID != "" || state.CloseLayerIndex != -1 {
		return fmt.Errorf("DCA entry or close state requires reconciliation")
	}
	for _, value := range []float64{state.TotalCost, state.TotalQty, state.AvgEntryPrice, state.CloseRequestedQty, state.CloseLimitPrice, state.CloseProgress.Quantity, state.CloseProgress.Notional, state.CloseFeeVerifiedQty, state.CloseBaseFeeQty} {
		if !finiteNumber(value) || value != 0 {
			return fmt.Errorf("DCA inventory, execution or fee progress is not empty")
		}
	}
	return nil
}
