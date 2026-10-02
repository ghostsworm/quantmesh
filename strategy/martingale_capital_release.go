package strategy

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// VerifyCapitalReleaseState reads recovery evidence without applying it. A
// venue-flat snapshot alone cannot settle a prepared entry or close cursor.
func (s *MartingaleStrategy) VerifyCapitalReleaseState(ctx context.Context) error {
	if ctx == nil {
		return fmt.Errorf("martingale capital release verification requires context")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := acquireCapitalProofLock(ctx, s.mu.TryRLock, s.mu.RUnlock); err != nil {
		return err
	}
	defer s.mu.RUnlock()
	if s.runtimeStateErr != nil {
		return fmt.Errorf("martingale accounting requires reconciliation: %w", s.runtimeStateErr)
	}
	// Snapshot construction skips nil entries; they must not disappear from
	// safety evidence or reach generic GetOrders (which dereferences entries).
	if len(s.entries) != 0 {
		return fmt.Errorf("martingale entries require reconciliation")
	}
	if err := verifyMartingaleReleaseFlat(s.runtimeStateSnapshotLocked()); err != nil {
		return err
	}
	if s.cfg == nil || strings.TrimSpace(s.cfg.Trading.BotID) == "" || s.name == "" || s.strategyCfg.Symbol == "" {
		return fmt.Errorf("martingale durable owner identity is unavailable")
	}
	reader, ok := s.runtimeStateStore.(RuntimeStateContextReader)
	if !ok {
		return fmt.Errorf("martingale capital release requires cancellable state storage")
	}
	version, payload, found, err := reader.LoadRuntimeStateContext(ctx, s.name)
	if err != nil {
		return fmt.Errorf("read martingale state before capital release: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if !found {
		return nil
	}
	if version != martingaleRuntimeStateSchemaVersion {
		return fmt.Errorf("martingale state schema requires recovery")
	}
	var state martingaleRuntimeState
	if err := json.Unmarshal([]byte(payload), &state); err != nil {
		return fmt.Errorf("decode martingale state before capital release: %w", err)
	}
	if state.BotID != s.effectiveBotID() || state.StrategyName != s.name || state.Symbol != s.strategyCfg.Symbol || state.Direction != s.direction {
		return fmt.Errorf("martingale durable owner identity is mismatched")
	}
	return verifyMartingaleReleaseFlat(state)
}

func verifyMartingaleReleaseFlat(state martingaleRuntimeState) error {
	if len(state.Entries) != 0 || state.CurrentLevel != 0 || state.IsClosing || state.CloseOrderID != 0 || state.CloseClientOrderID != "" || state.CloseReason != "" || state.PendingCloseReason != "" {
		return fmt.Errorf("martingale entry or close state requires reconciliation")
	}
	for _, value := range []float64{state.TotalCost, state.TotalQty, state.AvgEntryPrice, state.CloseRequestedQty, state.CloseProgress.Quantity, state.CloseProgress.Notional, state.CloseRealizedPnL} {
		if !finiteNumber(value) || value != 0 {
			return fmt.Errorf("martingale inventory or execution progress is not empty")
		}
	}
	return nil
}
