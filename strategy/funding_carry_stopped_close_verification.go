package strategy

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// ReconcileStoppedMarginClose retries only the read-only final verification of
// a completed financial phase. It never restarts producers, replays a close,
// settles a shared execution, or releases capital/ownership. Callers must still
// independently verify capital and the current runtime ownership before release.
func (s *FundingCarryStrategy) ReconcileStoppedMarginClose(ctx context.Context) error {
	if ctx == nil {
		return fmt.Errorf("stopped margin close verification requires context")
	}
	if err := s.acquireOperation(ctx); err != nil {
		return err
	}
	defer s.releaseOperation()
	s.stopMu.Lock()
	defer s.stopMu.Unlock()
	return s.reconcileStoppedMarginCloseLocked(ctx, nil)
}

// ReconcilePersistedStoppedMarginClose reconstructs only the exact final-read
// verification phase recorded by a previous process. It never starts strategy
// producers or repeats a financial operation. The caller must separately own
// the account/runtime leases and release capital only after this proof succeeds.
func (s *FundingCarryStrategy) ReconcilePersistedStoppedMarginClose(ctx context.Context, ownershipGuard func() error) error {
	if s == nil || ctx == nil || ownershipGuard == nil {
		return fmt.Errorf("persisted stopped margin close verification requires strategy, context, and ownership guard")
	}
	if err := ownershipGuard(); err != nil {
		return err
	}
	if err := s.acquireOperation(ctx); err != nil {
		return err
	}
	defer s.releaseOperation()
	s.stopMu.Lock()
	defer s.stopMu.Unlock()
	if s.stopCompleted {
		return nil
	}
	s.mu.RLock()
	store := s.runtimeStateStore
	started, attempted, done := s.started, s.stopAttempted, s.runDone
	scope, executionPending := s.marginAccountScope, s.executionRecoveryRequired
	s.mu.RUnlock()
	if started || executionPending {
		return fmt.Errorf("persisted stopped close cannot run on an active or execution-pending strategy")
	}
	if attempted {
		if done == nil {
			return fmt.Errorf("stopped close provenance has no producer completion signal")
		}
		select {
		case <-done:
		default:
			return fmt.Errorf("stopped close producers have not finished stopping")
		}
		return s.reconcileStoppedMarginCloseLocked(ctx, ownershipGuard)
	}
	if done != nil {
		return fmt.Errorf("fresh persisted recovery verifier has unexpected producer state")
	}
	if scope == "" {
		return fmt.Errorf("persisted stopped close recovery requires the configured margin account scope")
	}
	if s.fut == nil || s.spot == nil || s.marginEx == nil {
		return fmt.Errorf("persisted stopped close recovery requires futures, spot, and margin evidence sources")
	}
	reader, ok := store.(RuntimeStateContextReader)
	if !ok {
		return fmt.Errorf("persisted stopped close recovery requires cancellable durable reads")
	}
	version, payload, found, err := reader.LoadRuntimeStateContext(ctx, "funding_carry")
	if err != nil {
		return err
	}
	if err := ownershipGuard(); err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("persisted funding_carry close checkpoint is missing")
	}
	if version < 7 || version > fundingCarryRuntimeStateVersion {
		return fmt.Errorf("persisted stopped close recovery requires runtime schema 7 through %d", fundingCarryRuntimeStateVersion)
	}
	state, err := decodeFundingCarryRuntimeStateForRecovery(version, payload, s.fut.GetName(), s.spot.GetName(), s.symbol, true)
	if err != nil {
		return err
	}
	if !state.MarginCloseVerificationPending || state.MarginAccountScope != scope || !state.IntentInFlight {
		return fmt.Errorf("durable checkpoint is not an eligible final-verification-only close")
	}
	if err := validateFundingCarryDebtAsset(state, s.spot.GetBaseAsset()); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := ownershipGuard(); err != nil {
		return err
	}
	if _, ok := s.marginEx.(interface {
		GetMarginLiability(context.Context, string) (float64, float64, error)
	}); !ok {
		return fmt.Errorf("persisted stopped close recovery requires an authoritative margin liability reader")
	}
	if err := ownershipGuard(); err != nil {
		return err
	}

	// This instance is a verifier only. Restore the checkpoint's exact identity
	// and mark its producer as already stopped before invoking the shared proof.
	s.mu.Lock()
	if s.started || s.stopAttempted || s.executionRecoveryRequired || s.marginAccountScope != scope {
		s.mu.Unlock()
		return fmt.Errorf("persisted stopped close recovery state changed during reconstruction")
	}
	s.direction, s.strategySpotQty, s.spotQty = state.Direction, state.OwnedSpot, state.OwnedSpot
	s.futQty, s.marginDebt = state.OwnedFutures, state.MarginDebt
	s.marginBorrowTransferID, s.marginBorrowedAt = state.MarginBorrowTransferID, state.MarginBorrowedAt
	s.marginDebtEvents = append([]fundingCarryMarginDebtEvent(nil), state.MarginDebtEvents...)
	s.marginCoverOrders = cloneFundingCarryCoverOrders(state.MarginCoverOrders)
	s.marginRepayIntent = cloneFundingCarryRepayIntent(state.MarginRepayIntent)
	s.marginCoverIntent = cloneFundingCarryCoverIntent(state.MarginCoverIntent)
	s.marginCloseVerificationPending = true
	s.strategySpotKnown, s.unownedExposure, s.intentInFlight = state.OwnershipReady, state.ExposureUnknown, state.IntentInFlight
	s.intentPhase = state.IntentPhase
	s.stopAttempted = true
	s.stopErr = &fundingCarryFinalVerificationPendingError{cause: errors.New("reconstructed durable final-verification marker")}
	s.runDone = make(chan struct{})
	close(s.runDone)
	s.mu.Unlock()
	return s.reconcileStoppedMarginCloseLocked(ctx, ownershipGuard)
}

// ReconcilePersistedStoppedFlat accepts exactly two durable restart states:
// an explicitly marked final-close verification, or an already-clean flat
// checkpoint left after that verification committed. It then verifies the
// resulting clean checkpoint again before the caller may release claims.
func (s *FundingCarryStrategy) ReconcilePersistedStoppedFlat(ctx context.Context, ownershipGuard func() error) error {
	if s == nil || ctx == nil || ownershipGuard == nil {
		return fmt.Errorf("persisted stopped flat reconciliation requires strategy, context, and ownership guard")
	}
	reconcileErr := s.ReconcilePersistedStoppedMarginClose(ctx, ownershipGuard)
	if reconcileErr != nil {
		// A clean checkpoint is the other valid crash boundary. The strict flat
		// verifier rejects every unresolved marker/state before live proof.
		flatErr := s.VerifyPersistedStoppedFlat(ctx, ownershipGuard)
		if flatErr == nil {
			return nil
		}
		return errors.Join(reconcileErr, flatErr)
	}
	return s.VerifyPersistedStoppedFlat(ctx, ownershipGuard)
}

func (s *FundingCarryStrategy) reconcileStoppedMarginCloseLocked(ctx context.Context, ownershipGuard func() error) error {
	if s.stopCompleted {
		return nil
	}
	if ownershipGuard != nil {
		if err := ownershipGuard(); err != nil {
			return err
		}
	}
	s.mu.RLock()
	pending, store, done := s.marginCloseVerificationPending, s.runtimeStateStore, s.runDone
	producerActive := s.started && (s.ctx == nil || s.ctx.Err() == nil)
	s.mu.RUnlock()
	if !s.stopAttempted || s.stopErr == nil || !pending || store == nil || done == nil || producerActive {
		return fmt.Errorf("strategy is not a stopped final-verification-only margin close")
	}
	select {
	case <-done:
	default:
		return fmt.Errorf("strategy producers have not finished stopping")
	}
	reader, ok := store.(RuntimeStateContextReader)
	if !ok {
		return fmt.Errorf("stopped close verification requires cancellable durable reads")
	}
	var verified fundingCarryRuntimeState
	if err := s.withAccountWalletCoordination(ctx, func(operationCtx context.Context) error {
		if ownershipGuard != nil {
			if err := ownershipGuard(); err != nil {
				return err
			}
		}
		version, payload, found, err := reader.LoadRuntimeStateContext(operationCtx, "funding_carry")
		if err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("stopped close verification checkpoint disappeared")
		}
		state, err := decodeFundingCarryRuntimeStateForRecovery(version, payload, s.fut.GetName(), s.spot.GetName(), s.symbol, true)
		if err != nil {
			return err
		}
		return s.reconcileMarginCloseVerificationMode(operationCtx, store, version, payload, state, true, &verified, ownershipGuard)
	}); err != nil {
		return err
	}
	if ownershipGuard != nil {
		if err := ownershipGuard(); err != nil {
			return err
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.verifyDebtCommitLocked(ctx); err != nil {
		return err
	}
	if !s.marginCloseVerificationPending || s.marginAccountScope != verified.MarginAccountScope || s.executionRecoveryRequired {
		return fmt.Errorf("stopped close state changed before verified import")
	}
	// Import the exact committed snapshot only after wallet cleanup succeeds.
	// A failed cleanup leaves the original marker for another read-only retry.
	s.direction, s.strategySpotQty, s.spotQty, s.futQty, s.marginDebt = DirectionNone, 0, 0, 0, 0
	s.marginBorrowTransferID, s.marginBorrowedAt = 0, time.Time{}
	s.marginDebtEvents = append([]fundingCarryMarginDebtEvent(nil), verified.MarginDebtEvents...)
	s.marginCoverOrders = cloneFundingCarryCoverOrders(verified.MarginCoverOrders)
	s.marginRepayIntent, s.marginCoverIntent = nil, nil
	s.strategySpotKnown, s.unownedExposure, s.intentInFlight, s.marginCloseVerificationPending = true, false, false, false
	s.intentPhase = ""
	s.runtimeStateErr = nil
	s.stopErr, s.stopCompleted = nil, true
	return nil
}
