package strategy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"quantmesh/storage"
)

// recoverPreparedRuntimeIntentContext clears only a schema-8 intent whose
// durable phase proves that the strategy had not entered any write-RPC path.
// Older snapshots and dispatching/unknown intents remain untouched.
func (s *FundingCarryStrategy) recoverPreparedRuntimeIntentContext(ctx context.Context) error {
	if ctx == nil {
		return fmt.Errorf("prepared funding_carry recovery requires context")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.RLock()
	store, scope := s.runtimeStateStore, s.marginAccountScope
	s.mu.RUnlock()
	if store == nil {
		return nil
	}
	reader, canRead := store.(RuntimeStateContextReader)
	if !canRead {
		return fmt.Errorf("prepared funding_carry recovery requires cancellable reads and conditional durable writes")
	}
	if err := verifyStrategyWalletRuntimeOwner(s.openingGate); err != nil {
		return err
	}
	readCtx, cancel := context.WithTimeout(ctx, fundingCarryPreSubmitRecoveryTimeout)
	defer cancel()
	var version int
	var payload string
	var found bool
	var err error
	version, payload, found, err = reader.LoadRuntimeStateContext(readCtx, "funding_carry")
	if err != nil {
		return fmt.Errorf("read prepared funding_carry runtime intent: %w", err)
	}
	if err := readCtx.Err(); err != nil {
		return err
	}
	if !found || version != fundingCarryRuntimeStateVersion {
		return nil
	}
	var envelope fundingCarryRuntimeState
	if err := json.Unmarshal([]byte(payload), &envelope); err != nil {
		return fmt.Errorf("read prepared funding_carry runtime envelope: %w", err)
	}
	if !envelope.IntentInFlight || envelope.IntentPhase != fundingCarryIntentPhasePrepared {
		return nil
	}
	state, err := decodeFundingCarryRuntimeStateForRecovery(version, payload, s.fut.GetName(), s.spot.GetName(), s.symbol, true)
	if err != nil {
		return fmt.Errorf("validate prepared funding_carry runtime intent: %w", err)
	}
	writer, canWrite := store.(RuntimeStateConditionalWriter)
	if !canWrite {
		return fmt.Errorf("prepared funding_carry recovery requires cancellable reads and conditional durable writes")
	}
	if state.ExposureUnknown || state.MarginRepayIntent != nil || state.MarginCloseVerificationPending ||
		hasUnverifiedFundingCarryCover(state.MarginCoverOrders) {
		return fmt.Errorf("prepared funding_carry intent also contains unresolved financial evidence")
	}
	if scope != "" && state.MarginAccountScope != scope {
		return fmt.Errorf("prepared funding_carry intent margin account scope mismatch")
	}
	if err := validateFundingCarryDebtAsset(state, s.spot.GetBaseAsset()); err != nil {
		return err
	}
	state.IntentInFlight = false
	state.IntentPhase = ""
	state.ExposureUnknown = false
	// A durable prepared cover CID is safe to discard only here: the phase
	// proves the order RPC was never entered. Dispatching snapshots remain held.
	state.MarginCoverIntent = nil
	cleanPayload, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("encode recovered funding_carry source state: %w", err)
	}
	if err := verifyStrategyWalletRuntimeOwner(s.openingGate); err != nil {
		return err
	}
	saved, err := writer.CompareAndSwapRuntimeState(readCtx, "funding_carry", version, payload, fundingCarryRuntimeStateVersion, string(cleanPayload))
	if err != nil {
		if errors.Is(err, storage.ErrFundingCarryRuntimeStateCommitConfirmedCanceled) {
			verifyCtx, verifyCancel := context.WithTimeout(context.Background(), fundingCarryPreSubmitRecoveryTimeout)
			defer verifyCancel()
			gotVersion, gotPayload, gotFound, readErr := reader.LoadRuntimeStateContext(verifyCtx, "funding_carry")
			if readErr != nil || !gotFound || gotVersion != fundingCarryRuntimeStateVersion || gotPayload != string(cleanPayload) {
				return fmt.Errorf("confirm prepared funding_carry rollback after caller cancellation: %w", errors.Join(err, readErr))
			}
			return ctx.Err()
		}
		return fmt.Errorf("conditionally clear prepared funding_carry intent: %w", err)
	}
	if !saved {
		return fmt.Errorf("prepared funding_carry intent changed during recovery")
	}
	return nil
}
