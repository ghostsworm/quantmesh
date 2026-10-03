package strategy

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// Startup reconciles only an already accepted repayment. It cannot declare the
// whole interrupted operation complete without its order and asset evidence.
func (s *FundingCarryStrategy) reconcileSavedMarginRepayment(ctx context.Context) error {
	s.mu.RLock()
	store := s.runtimeStateStore
	s.mu.RUnlock()
	if store == nil {
		return nil // normal restore reports the missing store
	}
	_, payload, found, err := store.LoadRuntimeState("funding_carry")
	if err != nil || !found {
		return err
	}
	var probe fundingCarryRuntimeState
	if err := json.Unmarshal([]byte(payload), &probe); err != nil || probe.MarginRepayIntent == nil {
		return nil // normal restore validates all other snapshots
	}
	if err := s.acquireOperation(ctx); err != nil {
		return err
	}
	defer s.releaseOperation()
	return s.withAccountWalletCoordination(ctx, func(operationCtx context.Context) error {
		version, payload, found, err := store.LoadRuntimeState("funding_carry")
		if err != nil {
			return fmt.Errorf("reload pending repayment snapshot: %w", err)
		}
		if !found {
			return fmt.Errorf("pending repayment snapshot disappeared during recovery")
		}
		state, err := decodeFundingCarryRuntimeStateForRecovery(version, payload, s.fut.GetName(), s.spot.GetName(), s.symbol, true)
		if err != nil {
			return err
		}
		pending := state.MarginRepayIntent
		if pending == nil || !state.IntentInFlight || state.Direction != DirectionReverse ||
			pending.TransferID <= 0 || pending.BorrowTransferID <= 0 || pending.BorrowTransferID != state.MarginBorrowTransferID ||
			pending.AccountScope == "" || pending.AccountScope != state.MarginAccountScope ||
			!strings.EqualFold(pending.Asset, s.spot.GetBaseAsset()) || !validRuntimeAmount(pending.Amount) || pending.Amount <= 0 ||
			!validRuntimeAmount(pending.ExpectedRemaining) {
			return fmt.Errorf("pending repayment identity is incomplete or invalid")
		}
		if err := validateFundingCarryDebtAsset(state, s.spot.GetBaseAsset()); err != nil {
			return err
		}
		s.mu.Lock()
		if err := s.verifyDebtCommitLocked(operationCtx); err != nil {
			s.mu.Unlock()
			return err
		}
		if s.marginAccountScope == "" || s.marginAccountScope != state.MarginAccountScope {
			s.mu.Unlock()
			return fmt.Errorf("pending repayment account scope does not match current runtime")
		}
		s.direction, s.strategySpotQty, s.spotQty, s.futQty = state.Direction, state.OwnedSpot, state.OwnedSpot, state.OwnedFutures
		s.marginDebt, s.marginBorrowTransferID, s.marginBorrowedAt = state.MarginDebt, state.MarginBorrowTransferID, state.MarginBorrowedAt
		s.marginDebtEvents = append([]fundingCarryMarginDebtEvent(nil), state.MarginDebtEvents...)
		s.marginRepayIntent = cloneFundingCarryRepayIntent(pending)
		s.strategySpotKnown, s.unownedExposure, s.intentInFlight = true, true, true
		s.mu.Unlock()
		if err := s.repayMarginPrincipal(operationCtx, pending.Asset, pending.Amount, pending.ExpectedRemaining); err != nil {
			return err
		}
		return fmt.Errorf("saved repayment reconciled; interrupted orders and assets still require recovery")
	})
}
