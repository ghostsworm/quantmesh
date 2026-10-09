package strategy

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"quantmesh/exchange"
	"quantmesh/utils"
)

// Resolve only the original CID. Absence is not permission to submit again.
func (s *FundingCarryStrategy) reconcileSavedMarginCover(ctx context.Context) error {
	if ctx == nil {
		return fmt.Errorf("pending cover recovery requires context")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.RLock()
	store := s.runtimeStateStore
	s.mu.RUnlock()
	if store == nil {
		return nil
	}
	reader, hasContextReader := store.(RuntimeStateContextReader)
	if !hasContextReader {
		return fmt.Errorf("pending cover recovery requires cancellable checkpoint reads")
	}
	_, payload, found, err := reader.LoadRuntimeStateContext(ctx, "funding_carry")
	if err != nil || !found {
		return err
	}
	var probe fundingCarryRuntimeState
	if err := json.Unmarshal([]byte(payload), &probe); err != nil || probe.MarginCoverIntent == nil {
		return nil
	}
	if err := s.acquireOperation(ctx); err != nil {
		return err
	}
	defer s.releaseOperation()
	return s.withAccountWalletCoordination(ctx, func(operationCtx context.Context) error {
		version, payload, found, err := reader.LoadRuntimeStateContext(operationCtx, "funding_carry")
		if err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("pending cover snapshot disappeared")
		}
		state, err := decodeFundingCarryRuntimeStateForRecovery(version, payload, s.fut.GetName(), s.spot.GetName(), s.symbol, true)
		if err != nil {
			return err
		}
		pending := state.MarginCoverIntent
		if pending == nil || state.MarginRepayIntent != nil {
			return fmt.Errorf("pending cover operation is missing or conflicts with repayment")
		}
		if err := validateFundingCarryDebtAsset(state, s.spot.GetBaseAsset()); err != nil {
			return err
		}
		s.mu.RLock()
		scope := s.marginAccountScope
		s.mu.RUnlock()
		if scope == "" || scope != pending.AccountScope {
			return fmt.Errorf("pending cover account scope does not match current runtime")
		}
		if _, ok := store.(RuntimeStateConditionalWriter); !ok {
			return fmt.Errorf("cover acknowledgement recovery requires atomic conditional checkpoint writes")
		}
		operationCtx = context.WithValue(operationCtx, fundingCarryRecoveryCheckpointKey{}, &fundingCarryRecoveryCheckpoint{store: store, version: version, payload: payload})
		querier, ok := s.marginEx.(exchange.OrderByClientIDQuerier)
		if !ok {
			return fmt.Errorf("margin venue cannot query exact cover CID")
		}
		order, err := querier.GetOrderByClientOrderID(operationCtx, pending.Symbol, pending.ClientOrderID)
		if err != nil {
			return err
		}
		if err := validateFundingCarryRecoveredCover(pending, order, s.marginEx.GetName()); err != nil {
			return err
		}
		s.mu.Lock()
		if err := s.verifyDebtCommitLocked(operationCtx); err != nil {
			s.mu.Unlock()
			return err
		}
		if s.marginAccountScope != scope {
			s.mu.Unlock()
			return fmt.Errorf("cover recovery account changed")
		}
		s.direction, s.strategySpotQty, s.spotQty, s.futQty = state.Direction, state.OwnedSpot, state.OwnedSpot, state.OwnedFutures
		s.marginDebt, s.marginBorrowTransferID, s.marginBorrowedAt = state.MarginDebt, state.MarginBorrowTransferID, state.MarginBorrowedAt
		s.marginDebtEvents = append([]fundingCarryMarginDebtEvent(nil), state.MarginDebtEvents...)
		s.marginCoverOrders = cloneFundingCarryCoverOrders(state.MarginCoverOrders)
		s.marginCoverIntent = cloneFundingCarryCoverIntent(pending)
		s.strategySpotKnown, s.intentInFlight, s.unownedExposure = true, true, true
		s.mu.Unlock()
		if err := s.checkpointMarginCoverOrder(operationCtx, order, pending.Quantity, pending.DebtToCover); err != nil {
			return err
		}
		return nil // Start continues into exact-order fill reconciliation
	})
}

func validateFundingCarryRecoveredCover(pending *fundingCarryCoverIntent, order *exchange.Order, venue string) error {
	if order == nil || order.OrderID <= 0 || !strings.EqualFold(order.Symbol, pending.Symbol) || order.Side != exchange.SideBuy || order.Type != exchange.OrderTypeLimit || order.CreatedAt.IsZero() || order.CreatedAt.UnixMilli() <= 0 ||
		(order.ClientOrderID != pending.ClientOrderID && order.ClientOrderID != utils.AddBrokerPrefix(strings.ToLower(venue), pending.ClientOrderID)) ||
		!fundingCarryFinancialAmountsMatch(order.Quantity, pending.Quantity) || !fundingCarryFinancialAmountsMatch(order.Price, pending.Price) || !validRuntimeAmount(order.ExecutedQty) ||
		(order.ExecutedQty > order.Quantity && !fundingCarryFinancialAmountsMatch(order.ExecutedQty, order.Quantity)) {
		return fmt.Errorf("cover CID lookup does not match original request")
	}
	switch order.Status {
	case exchange.OrderStatusNew, exchange.OrderStatusPartiallyFilled, exchange.OrderStatusFilled, exchange.OrderStatusCanceled, exchange.OrderStatusExpired, exchange.OrderStatusRejected:
	default:
		return fmt.Errorf("cover CID lookup returned unknown order status")
	}
	if order.Status == exchange.OrderStatusFilled && !fundingCarryFinancialAmountsMatch(order.ExecutedQty, pending.Quantity) {
		return fmt.Errorf("filled cover lookup has incomplete execution")
	}
	return nil
}
