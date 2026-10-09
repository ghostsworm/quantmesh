package strategy

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"quantmesh/exchange"
)

// Only the final read-only phase of a live reverse close may create this marker.
// Older generic UNKNOWN snapshots are deliberately not inferred to be complete.
func (s *FundingCarryStrategy) checkpointMarginCloseVerification(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.verifyDebtCommitLocked(ctx); err != nil {
		return err
	}
	if s.unownedExposure || s.marginDebt != 0 || s.futQty != 0 || s.strategySpotQty != 0 || s.marginRepayIntent != nil || s.marginCoverIntent != nil {
		return fmt.Errorf("reverse close has unresolved financial legs before final verification")
	}
	s.marginCloseVerificationPending = true
	if err := s.persistRuntimeStateLocked(); err != nil {
		s.runtimeStateErr = err
		return fmt.Errorf("checkpoint final margin close verification: %w", err)
	}
	return nil
}

// Caller owns the operation token and account-wallet coordination. It must not
// submit, cancel, repay, dispose of balances, or settle shared execution intents.
func (s *FundingCarryStrategy) reconcileMarginCloseVerification(ctx context.Context, store RuntimeStateStore, version int, payload string, state fundingCarryRuntimeState) error {
	return s.reconcileMarginCloseVerificationMode(ctx, store, version, payload, state, false, nil, nil)
}

func (s *FundingCarryStrategy) reconcileMarginCloseVerificationMode(ctx context.Context, store RuntimeStateStore, version int, payload string, state fundingCarryRuntimeState, stopped bool, verified *fundingCarryRuntimeState, ownershipGuard func() error) error {
	checkOwnership := func() error {
		if ownershipGuard == nil {
			return nil
		}
		return ownershipGuard()
	}
	if err := checkOwnership(); err != nil {
		return err
	}
	validation := state
	if stopped && !state.MarginCloseVerificationPending && state.Direction == DirectionNone && !state.IntentInFlight && !state.ExposureUnknown && state.MarginBorrowTransferID == 0 && state.MarginBorrowedAt.IsZero() {
		// A prior CAS may have committed before cancellation or an ACK error.
		// Only this same stopped instance retains the original marker identity.
		s.mu.RLock()
		pending, borrowID, borrowedAt := s.marginCloseVerificationPending, s.marginBorrowTransferID, s.marginBorrowedAt
		s.mu.RUnlock()
		if !pending || borrowID <= 0 || borrowedAt.IsZero() {
			return fmt.Errorf("clean checkpoint lacks this stopped close's recovery provenance")
		}
		validation.MarginCloseVerificationPending, validation.IntentInFlight = true, true
		validation.Direction, validation.MarginBorrowTransferID, validation.MarginBorrowedAt = DirectionReverse, borrowID, borrowedAt
	}
	state = validation
	if version < 7 || version > fundingCarryRuntimeStateVersion || !state.MarginCloseVerificationPending || !state.OwnershipReady ||
		state.Direction != DirectionReverse || state.MarginDebt != 0 || state.OwnedFutures != 0 || state.OwnedSpot != 0 ||
		!state.IntentInFlight || state.MarginRepayIntent != nil || state.MarginCoverIntent != nil || len(state.MarginCoverOrders) == 0 ||
		state.MarginBorrowTransferID <= 0 || state.MarginBorrowedAt.IsZero() || len(state.MarginDebtEvents) < 2 {
		return fmt.Errorf("final margin close verification lacks complete owned close evidence")
	}
	if err := validateFundingCarryDebtPrincipalBalance(state); err != nil {
		return err
	}
	lastBorrowMatches := false
	for _, event := range state.MarginDebtEvents {
		if event.Action == "borrow" {
			lastBorrowMatches = event.TransferID == state.MarginBorrowTransferID && event.OccurredAt.Equal(state.MarginBorrowedAt)
		}
	}
	if !lastBorrowMatches {
		return fmt.Errorf("final margin close verification lacks exact completed borrow/cover provenance")
	}
	if err := validateFundingCarryDebtAsset(state, s.spot.GetBaseAsset()); err != nil {
		return err
	}
	if err := requireNoFundingCarryCoverRemaining(state, s.spot.GetBaseAsset()); err != nil {
		return err
	}
	s.mu.RLock()
	scope, executionPending := s.marginAccountScope, s.executionRecoveryRequired
	s.mu.RUnlock()
	if executionPending || scope == "" || scope != state.MarginAccountScope {
		return fmt.Errorf("final margin close verification has pending executions or wrong account scope")
	}
	if _, ok := s.marginEx.(exchange.MarginLiabilityReader); !ok {
		return fmt.Errorf("final margin close recovery requires independent authoritative liability")
	}
	reader, ok := store.(RuntimeStateContextReader)
	if !ok {
		return fmt.Errorf("final margin close recovery requires cancellable checkpoint reads")
	}
	writer, ok := store.(RuntimeStateConditionalWriter)
	if !ok {
		return fmt.Errorf("final margin close recovery requires atomic conditional writes")
	}
	if _, _, err := s.readMarginLiabilityForClose(ctx, true); err != nil {
		return err
	}
	if err := checkOwnership(); err != nil {
		return err
	}
	positions, err := readScopedPositionSnapshot(ctx, s.fut, s.symbol)
	if err != nil {
		return err
	}
	for _, p := range positions {
		if p.Size != 0 { // No quantity tolerance may hide an outstanding leg.
			return fmt.Errorf("final margin close recovery still has futures exposure")
		}
	}
	if err := checkOwnership(); err != nil {
		return err
	}
	for name, venue := range map[string]exchange.IExchange{"futures": s.fut, "spot": s.spot, "spot-margin": s.marginEx} {
		if err := checkOwnership(); err != nil {
			return err
		}
		if err := requireAccountHasNoOpenOrders(ctx, venue, name); err != nil {
			return err
		}
		if err := checkOwnership(); err != nil {
			return err
		}
	}
	currentVersion, currentPayload, found, err := reader.LoadRuntimeStateContext(ctx, "funding_carry")
	if err != nil {
		return err
	}
	if !found || currentVersion != version || currentPayload != payload {
		return fmt.Errorf("final margin close checkpoint changed during verification")
	}
	if err := checkOwnership(); err != nil {
		return err
	}
	state.Direction, state.IntentInFlight, state.ExposureUnknown = DirectionNone, false, false
	state.IntentPhase = ""
	state.MarginCloseVerificationPending = false
	state.MarginBorrowTransferID, state.MarginBorrowedAt = 0, time.Time{}
	encoded, err := json.Marshal(state)
	if err != nil {
		return err
	}
	if _, err := decodeFundingCarryRuntimeState(version, string(encoded), s.fut.GetName(), s.spot.GetName(), s.symbol); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.verifyDebtCommitLocked(ctx); err != nil {
		return err
	}
	producerActive := s.started && (s.ctx == nil || s.ctx.Err() == nil)
	if s.executionRecoveryRequired || s.marginAccountScope != scope || (!stopped && s.runtimeStateErr != nil) || producerActive {
		return fmt.Errorf("final margin close runtime changed during verification")
	}
	saved, err := writer.CompareAndSwapRuntimeState(ctx, "funding_carry", version, payload, version, string(encoded))
	if err != nil {
		return err
	}
	if !saved {
		return fmt.Errorf("final margin close checkpoint changed before conditional commit")
	}
	if err := checkOwnership(); err != nil {
		return err
	}
	if err := s.verifyDebtCommitLocked(ctx); err != nil {
		return err
	}
	if verified != nil {
		*verified = state
	}
	return nil // Ordinary restore still independently validates and imports it.
}
