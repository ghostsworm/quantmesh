package strategy

import (
	"context"
	"encoding/json"
	"fmt"
)

// Restore historical remaining-asset accounting only. No venue balance is
// adopted, no financial RPC is made, and this cannot resume trading.
func (s *FundingCarryStrategy) reconcileSavedMarginRemaining(ctx context.Context) error {
	s.mu.RLock()
	store, gate := s.runtimeStateStore, s.openingGate
	s.mu.RUnlock()
	if store == nil {
		return nil
	}
	_, payload, found, err := store.LoadRuntimeState("funding_carry")
	if err != nil || !found {
		return err
	}
	var probe fundingCarryRuntimeState
	if err := json.Unmarshal([]byte(payload), &probe); err != nil || len(probe.MarginCoverOrders) == 0 {
		return nil // ordinary restore validates missing or malformed evidence
	}
	if err := verifyStrategyWalletRuntimeOwner(gate); err != nil {
		return err
	}
	if err := s.acquireOperation(ctx); err != nil {
		return err
	}
	defer s.releaseOperation()
	var pending *FundingCarryReconciliationRequiredError
	coordinationErr := s.withAccountWalletCoordination(ctx, func(operationCtx context.Context) error {
		version, payload, found, err := store.LoadRuntimeState("funding_carry")
		if err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("remaining margin accounting disappeared during recovery")
		}
		state, err := decodeFundingCarryRuntimeStateForRecovery(version, payload, s.fut.GetName(), s.spot.GetName(), s.symbol, true)
		if err != nil {
			return err
		}
		if state.MarginRepayIntent != nil || state.MarginCoverIntent != nil {
			return fmt.Errorf("remaining margin accounting conflicts with a pending financial request")
		}
		remaining, err := fundingCarryCoverRemaining(state, s.spot.GetBaseAsset())
		if err != nil {
			return err
		}
		if remaining.Sign() == 0 {
			return nil // ordinary restore must still validate the complete state
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		if err := s.restoreRemainingMarginAccountingLocked(operationCtx, state); err != nil {
			return err
		}
		pending = &FundingCarryReconciliationRequiredError{message: fmt.Sprintf("remaining margin assets restored for reconciliation: %s %s; current inventory and disposal remain unverified", fundingCarryCoverRemainingString(remaining), s.spot.GetBaseAsset())}
		return nil
	})
	if coordinationErr != nil {
		return coordinationErr
	}
	if pending != nil {
		return pending
	}
	return nil
}

func (s *FundingCarryStrategy) restoreRemainingMarginAccountingLocked(ctx context.Context, state fundingCarryRuntimeState) error {
	if err := s.verifyDebtCommitLocked(ctx); err != nil {
		return err
	}
	if s.marginRepayIntent != nil || s.marginCoverIntent != nil || s.runtimeStateErr != nil {
		return fmt.Errorf("remaining margin accounting cannot overwrite unresolved local financial evidence")
	}
	if s.marginAccountScope == "" || s.marginAccountScope != state.MarginAccountScope {
		return fmt.Errorf("remaining margin account scope does not match current runtime")
	}
	s.direction, s.strategySpotQty, s.spotQty, s.futQty = state.Direction, state.OwnedSpot, state.OwnedSpot, state.OwnedFutures
	s.marginDebt, s.marginBorrowTransferID, s.marginBorrowedAt = state.MarginDebt, state.MarginBorrowTransferID, state.MarginBorrowedAt
	s.marginDebtEvents = append([]fundingCarryMarginDebtEvent(nil), state.MarginDebtEvents...)
	s.marginCoverOrders = cloneFundingCarryCoverOrders(state.MarginCoverOrders)
	s.strategySpotKnown, s.unownedExposure, s.intentInFlight = true, true, true
	return nil
}
