package strategy

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"quantmesh/exchange"
)

// Import an accepted borrow receipt, never replay its financial request or
// declare the interrupted operation complete. The original requested quantity
// is absent from old ACK snapshots; the receipt proves actual debt only.
func (s *FundingCarryStrategy) reconcileSavedMarginBorrowReceipt(ctx context.Context) error {
	s.mu.RLock()
	store := s.runtimeStateStore
	s.mu.RUnlock()
	if store == nil {
		return nil
	}
	reader, hasContextReader := store.(RuntimeStateContextReader)
	if !hasContextReader {
		return fmt.Errorf("pending borrow recovery requires cancellable checkpoint reads")
	}
	_, payload, found, err := reader.LoadRuntimeStateContext(ctx, "funding_carry")
	if err != nil || !found {
		return err
	}
	var probe fundingCarryRuntimeState
	if json.Unmarshal([]byte(payload), &probe) != nil || probe.Direction != DirectionNone || probe.MarginBorrowTransferID <= 0 {
		return nil
	}
	if err := s.acquireOperation(ctx); err != nil {
		return err
	}
	defer s.releaseOperation()
	err = s.withAccountWalletCoordination(ctx, func(operationCtx context.Context) error {
		version, payload, found, err := reader.LoadRuntimeStateContext(operationCtx, "funding_carry")
		if err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("saved borrow acknowledgement disappeared")
		}
		var ack fundingCarryRuntimeState
		if err := json.Unmarshal([]byte(payload), &ack); err != nil {
			return err
		}
		if version < 7 || version > fundingCarryRuntimeStateVersion || ack.Direction != DirectionNone || ack.MarginBorrowTransferID <= 0 ||
			!ack.IntentInFlight || !ack.MarginBorrowedAt.IsZero() || ack.MarginDebt != 0 || ack.OwnedSpot != 0 || ack.OwnedFutures != 0 ||
			ack.MarginRepayIntent != nil || ack.MarginCoverIntent != nil || len(ack.MarginCoverOrders) != 0 || ack.MarginAccountScope == "" {
			return fmt.Errorf("saved borrow acknowledgement is not an isolated pending receipt")
		}
		// Ordinary startup must continue rejecting bare ACKs. Validate the rest
		// of this recovery-only envelope without weakening its decoder.
		validation := ack
		validation.MarginBorrowTransferID = 0
		encoded, err := json.Marshal(validation)
		if err != nil {
			return err
		}
		if _, err := decodeFundingCarryRuntimeStateForRecovery(version, string(encoded), s.fut.GetName(), s.spot.GetName(), s.symbol, true); err != nil {
			return err
		}
		if err := validateFundingCarryDebtPrincipalBalance(validation); err != nil {
			return err
		}
		s.mu.RLock()
		scope := s.marginAccountScope
		s.mu.RUnlock()
		if scope == "" || scope != ack.MarginAccountScope {
			return fmt.Errorf("saved borrow account scope does not match runtime")
		}
		asset := strings.TrimSpace(s.spot.GetBaseAsset())
		querier, ok := s.marginEx.(exchange.MarginTransactionByIDQuerier)
		if !ok || asset == "" {
			return fmt.Errorf("saved borrow receipt cannot be queried by exact identity")
		}
		row, err := querier.GetMarginTransactionByID(operationCtx, asset, "BORROW", ack.MarginBorrowTransferID)
		if err != nil {
			return err
		}
		if row.TransferID != ack.MarginBorrowTransferID || !strings.EqualFold(strings.TrimSpace(row.Asset), asset) ||
			!strings.EqualFold(strings.TrimSpace(row.Status), "CONFIRMED") || row.Timestamp <= 0 || row.Interest != 0 ||
			!finitePositive(row.Principal) || !finitePositive(row.Amount) || !fundingCarryFinancialAmountsMatch(row.Principal, row.Amount) {
			return fmt.Errorf("saved borrow receipt has invalid identity or financial components")
		}
		event := fundingCarryMarginDebtEvent{Action: "borrow", TransferID: row.TransferID, Asset: asset, Amount: row.Amount,
			Principal: row.Principal, OccurredAt: time.UnixMilli(row.Timestamp).UTC(), AccountScope: scope}
		if err := validateFundingCarryDebtEventIntegrity(event, scope); err != nil {
			return err
		}
		ack.Direction, ack.MarginDebt, ack.MarginBorrowedAt = DirectionReverse, event.Principal, event.OccurredAt
		ack.ExposureUnknown, ack.IntentInFlight = true, true
		ack.MarginDebtEvents = append(ack.MarginDebtEvents, event)
		encoded, err = json.Marshal(ack)
		if err != nil {
			return err
		}
		if _, err := decodeFundingCarryRuntimeStateForRecovery(version, string(encoded), s.fut.GetName(), s.spot.GetName(), s.symbol, true); err != nil {
			return err
		}
		if err := validateFundingCarryDebtPrincipalBalance(ack); err != nil {
			return err
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		// Check freshness before ownership validation; the conditional write
		// below also rejects updates occurring after this read.
		currentVersion, currentPayload, currentFound, err := reader.LoadRuntimeStateContext(operationCtx, "funding_carry")
		if err != nil {
			return fmt.Errorf("reload borrow acknowledgement before receipt commit: %w", err)
		}
		if !currentFound || currentVersion != version || currentPayload != payload {
			return fmt.Errorf("borrow acknowledgement changed during receipt query")
		}
		if err := s.verifyDebtCommitLocked(operationCtx); err != nil {
			return err
		}
		if s.marginAccountScope != scope {
			return fmt.Errorf("borrow receipt scope changed during query")
		}
		writer, ok := store.(RuntimeStateConditionalWriter)
		if !ok {
			return fmt.Errorf("borrow receipt requires atomic conditional checkpoint writes")
		}
		saved, err := writer.CompareAndSwapRuntimeState(operationCtx, "funding_carry", version, payload, version, string(encoded))
		if err != nil {
			s.unownedExposure, s.runtimeStateErr = true, err
			return err
		}
		if !saved {
			return fmt.Errorf("borrow acknowledgement changed before conditional receipt commit")
		}
		// The durable checkpoint and the stopped object's financial view must
		// agree. Otherwise StopContext could report success from an empty local
		// direction despite the newly verified, still-unresolved borrowed debt.
		s.direction, s.marginDebt = ack.Direction, ack.MarginDebt
		s.marginBorrowTransferID, s.marginBorrowedAt = ack.MarginBorrowTransferID, ack.MarginBorrowedAt
		s.marginDebtEvents = append([]fundingCarryMarginDebtEvent(nil), ack.MarginDebtEvents...)
		s.strategySpotQty, s.spotQty, s.futQty = 0, 0, 0
		s.strategySpotKnown, s.unownedExposure, s.intentInFlight = true, true, true
		s.runtimeStateErr = nil
		return nil
	})
	if err != nil {
		return err
	}
	return fmt.Errorf("saved borrow receipt recorded; current debt, assets and interrupted operation still require reconciliation")
}
