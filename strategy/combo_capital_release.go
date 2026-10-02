package strategy

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
)

// The runtime holds submission barriers and independently verifies the venue.
// Parent snapshots cannot substitute for each child's durable recovery proof.
func (s *ComboStrategy) VerifyCapitalReleaseState(ctx context.Context) error {
	if ctx == nil {
		return fmt.Errorf("Combo capital release verification requires context")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := acquireCapitalProofLock(ctx, s.mu.TryRLock, s.mu.RUnlock); err != nil {
		return err
	}
	defer s.mu.RUnlock()
	if s.initErr != nil || s.strategyCfg == nil || s.runtimeStateDirty || len(s.strategies) == 0 {
		return fmt.Errorf("Combo initialization or accounting requires reconciliation")
	}
	if !finiteNumber(s.strategyCfg.MaxDrawdown) || s.strategyCfg.MaxDrawdown < 0 {
		return fmt.Errorf("Combo drawdown evidence configuration is invalid")
	}
	botID, symbol := s.runtimeStateIdentity()
	if botID == "" || symbol == "" || s.name == "" || !finiteNumber(s.peakEquity) || s.peakEquity < 0 {
		return fmt.Errorf("Combo owner or high-water state is invalid")
	}
	reader, ok := s.runtimeStateStore.(RuntimeStateContextReader)
	if !ok {
		return fmt.Errorf("Combo capital release requires cancellable state storage")
	}
	version, payload, found, err := reader.LoadRuntimeStateContext(ctx, s.name)
	if err != nil {
		return fmt.Errorf("read Combo state before capital release: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if found {
		if version != comboRuntimeStateSchemaVersion {
			return fmt.Errorf("Combo state schema requires recovery")
		}
		var state comboRuntimeState
		if err := json.Unmarshal([]byte(payload), &state); err != nil {
			return fmt.Errorf("decode Combo state before capital release: %w", err)
		}
		if state.BotID != botID || state.StrategyName != s.name || state.Symbol != symbol || !finiteNumber(state.PeakEquity) || state.PeakEquity < 0 || s.strategyCfg.MaxDrawdown > 0 && state.PeakEquity <= 0 {
			return fmt.Errorf("Combo durable owner or high-water evidence is invalid")
		}
	} else if s.strategyCfg.MaxDrawdown > 0 {
		return fmt.Errorf("Combo durable drawdown baseline is missing")
	}
	seen := make(map[string]struct{}, len(s.strategies))
	for _, child := range s.strategies {
		if child == nil || (reflect.ValueOf(child).Kind() == reflect.Ptr && reflect.ValueOf(child).IsNil()) {
			return fmt.Errorf("Combo child proof is unavailable")
		}
		name := child.Name()
		if strings.TrimSpace(name) == "" {
			return fmt.Errorf("Combo child identity is missing")
		}
		if _, duplicate := seen[name]; duplicate {
			return fmt.Errorf("Combo child identity is duplicated")
		}
		seen[name] = struct{}{}
		verifier, ok := child.(interface{ VerifyCapitalReleaseState(context.Context) error })
		if !ok {
			return fmt.Errorf("Combo child does not support capital release proof")
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := verifier.VerifyCapitalReleaseState(ctx); err != nil {
			return fmt.Errorf("verify Combo child before capital release: %w", err)
		}
	}
	return ctx.Err()
}
