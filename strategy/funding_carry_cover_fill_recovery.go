package strategy

import (
	"context"
	"encoding/json"
	"fmt"

	"quantmesh/exchange"
)

func hasUnverifiedFundingCarryCover(orders []fundingCarryCoverOrder) bool {
	for _, record := range orders {
		if !record.Verified {
			return true
		}
	}
	return false
}

// Confirm an already submitted order, never re-buy or spend shared balances.
func (s *FundingCarryStrategy) reconcileSavedMarginCoverFills(ctx context.Context) error {
	s.mu.RLock()
	store := s.runtimeStateStore
	s.mu.RUnlock()
	if store == nil {
		return nil
	}
	_, payload, found, err := store.LoadRuntimeState("funding_carry")
	if err != nil || !found {
		return err
	}
	var probe fundingCarryRuntimeState
	if err := json.Unmarshal([]byte(payload), &probe); err != nil || !hasUnverifiedFundingCarryCover(probe.MarginCoverOrders) {
		return nil
	}
	if err := s.acquireOperation(ctx); err != nil {
		return err
	}
	defer s.releaseOperation()
	return s.withAccountWalletCoordination(ctx, func(operationCtx context.Context) error {
		version, payload, found, err := store.LoadRuntimeState("funding_carry")
		if err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("cover fill recovery snapshot disappeared")
		}
		state, err := decodeFundingCarryRuntimeStateForRecovery(version, payload, s.fut.GetName(), s.spot.GetName(), s.symbol, true)
		if err != nil {
			return err
		}
		if state.MarginCoverIntent != nil || state.MarginRepayIntent != nil || !state.IntentInFlight {
			return fmt.Errorf("cover fill recovery conflicts with another pending request")
		}
		if err := validateFundingCarryDebtAsset(state, s.spot.GetBaseAsset()); err != nil {
			return err
		}
		var record *fundingCarryCoverOrder
		for i := range state.MarginCoverOrders {
			if state.MarginCoverOrders[i].Verified {
				continue
			}
			if record != nil {
				return fmt.Errorf("multiple unresolved cover orders require attribution")
			}
			record = &state.MarginCoverOrders[i]
		}
		if record == nil || record.ClientOrderID == "" || record.RequestPrice <= 0 || record.PreparedAt.IsZero() {
			return fmt.Errorf("cover fill recovery lacks exact original request metadata")
		}
		s.mu.RLock()
		scope := s.marginAccountScope
		s.mu.RUnlock()
		if scope == "" || scope != record.AccountScope {
			return fmt.Errorf("cover fills account scope mismatch")
		}
		order, err := s.marginEx.GetOrder(operationCtx, state.Symbol, record.OrderID)
		if err != nil {
			return err
		}
		original := &fundingCarryCoverIntent{ClientOrderID: record.ClientOrderID, Symbol: state.Symbol, Quantity: record.Requested, Price: record.RequestPrice}
		if err := validateFundingCarryRecoveredCover(original, order, s.marginEx.GetName()); err != nil {
			return err
		}
		if order.OrderID != record.OrderID || order.Status != exchange.OrderStatusFilled {
			return fmt.Errorf("cover order remains open, partial or otherwise unresolved")
		}
		s.mu.Lock()
		if err := s.verifyDebtCommitLocked(operationCtx); err != nil {
			s.mu.Unlock()
			return err
		}
		if s.marginAccountScope != scope {
			s.mu.Unlock()
			return fmt.Errorf("cover fill recovery account changed")
		}
		s.direction, s.strategySpotQty, s.spotQty, s.futQty = state.Direction, state.OwnedSpot, state.OwnedSpot, state.OwnedFutures
		s.marginDebt, s.marginBorrowTransferID, s.marginBorrowedAt = state.MarginDebt, state.MarginBorrowTransferID, state.MarginBorrowedAt
		s.marginDebtEvents = append([]fundingCarryMarginDebtEvent(nil), state.MarginDebtEvents...)
		s.marginCoverOrders = cloneFundingCarryCoverOrders(state.MarginCoverOrders)
		s.strategySpotKnown, s.intentInFlight, s.unownedExposure = true, true, true
		s.mu.Unlock()
		if err := s.verifyMarginNetDebtCover(operationCtx, record.OrderID, order.ExecutedQty, record.Requested, record.DebtToCover); err != nil {
			return err
		}
		return fmt.Errorf("cover net fills recovered; physical assets and repayment still require reconciliation")
	})
}
