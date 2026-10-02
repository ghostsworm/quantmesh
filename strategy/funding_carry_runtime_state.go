package strategy

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"

	"quantmesh/exchange"
)

const fundingCarryRuntimeStateVersion = 1

type fundingCarryRuntimeState struct {
	Strategy               string                        `json:"strategy"`
	FuturesExchange        string                        `json:"futures_exchange"`
	SpotExchange           string                        `json:"spot_exchange"`
	Symbol                 string                        `json:"symbol"`
	MarginAccountScope     string                        `json:"margin_account_scope,omitempty"`
	OwnershipReady         bool                          `json:"ownership_ready"`
	IntentInFlight         bool                          `json:"intent_in_flight"`
	ExposureUnknown        bool                          `json:"exposure_unknown"`
	Direction              CarryDirection                `json:"direction"`
	OwnedSpot              float64                       `json:"owned_spot"`
	OwnedFutures           float64                       `json:"owned_futures"`
	MarginDebt             float64                       `json:"margin_debt"`
	MarginBorrowTransferID int64                         `json:"margin_borrow_transfer_id,omitempty"`
	MarginBorrowedAt       time.Time                     `json:"margin_borrowed_at,omitempty"`
	MarginDebtEvents       []fundingCarryMarginDebtEvent `json:"margin_debt_events,omitempty"`
}

type fundingCarryMarginDebtEvent struct {
	Action       string    `json:"action"`
	TransferID   int64     `json:"transfer_id"`
	Asset        string    `json:"asset"`
	Amount       float64   `json:"amount"`
	Principal    float64   `json:"principal,omitempty"`
	InterestPaid float64   `json:"interest_paid,omitempty"`
	OccurredAt   time.Time `json:"occurred_at"`
	AccountScope string    `json:"account_scope,omitempty"`
}

func (s *FundingCarryStrategy) runtimeStateSnapshotLocked() fundingCarryRuntimeState {
	return fundingCarryRuntimeState{
		Strategy: "funding_carry", FuturesExchange: s.fut.GetName(), SpotExchange: s.spot.GetName(),
		Symbol: s.symbol, MarginAccountScope: s.marginAccountScope, OwnershipReady: s.strategySpotKnown, IntentInFlight: s.intentInFlight,
		ExposureUnknown: s.unownedExposure, Direction: s.direction, OwnedSpot: s.strategySpotQty,
		OwnedFutures: s.futQty, MarginDebt: s.marginDebt,
		MarginBorrowTransferID: s.marginBorrowTransferID, MarginBorrowedAt: s.marginBorrowedAt,
		MarginDebtEvents: append([]fundingCarryMarginDebtEvent(nil), s.marginDebtEvents...),
	}
}

func (s *FundingCarryStrategy) persistRuntimeStateLocked() error {
	if s.runtimeStateStore == nil {
		return fmt.Errorf("funding_carry runtime state store is unavailable")
	}
	payload, err := json.Marshal(s.runtimeStateSnapshotLocked())
	if err != nil {
		return fmt.Errorf("encode funding_carry runtime state: %w", err)
	}
	if err := s.runtimeStateStore.SaveRuntimeState("funding_carry", fundingCarryRuntimeStateVersion, string(payload)); err != nil {
		return fmt.Errorf("persist funding_carry runtime state: %w", err)
	}
	s.runtimeStateErr = nil
	return nil
}

func (s *FundingCarryStrategy) confirmMarginDebtTransaction(ctx context.Context, action string, transferID int64, asset string, amount float64) (fundingCarryMarginDebtEvent, error) {
	if (action != "borrow" && action != "repay") || transferID <= 0 || strings.TrimSpace(asset) == "" || !validRuntimeAmount(amount) || amount <= 0 {
		return fundingCarryMarginDebtEvent{}, fmt.Errorf("margin debt event acknowledgement is invalid")
	}
	querier, ok := s.marginEx.(exchange.MarginTransactionByIDQuerier)
	if !ok {
		return fundingCarryMarginDebtEvent{}, fmt.Errorf("exchange cannot verify margin %s transaction %d by ID", action, transferID)
	}
	transaction, err := querier.GetMarginTransactionByID(ctx, asset, strings.ToUpper(action), transferID)
	if err != nil {
		return fundingCarryMarginDebtEvent{}, fmt.Errorf("query confirmed margin %s transaction %d: %w", action, transferID, err)
	}
	tolerance := math.Max(1e-10, amount*1e-8)
	if transaction.TransferID != transferID || !strings.EqualFold(strings.TrimSpace(transaction.Asset), strings.TrimSpace(asset)) ||
		!strings.EqualFold(strings.TrimSpace(transaction.Status), "CONFIRMED") || transaction.Timestamp <= 0 ||
		!validRuntimeAmount(transaction.Amount) || math.Abs(transaction.Amount-amount) > tolerance ||
		!validRuntimeAmount(transaction.Principal) || !validRuntimeAmount(transaction.Interest) ||
		math.Abs(transaction.Principal+transaction.Interest-transaction.Amount) > tolerance {
		return fundingCarryMarginDebtEvent{}, fmt.Errorf("margin %s transaction %d does not match confirmed asset and amount", action, transferID)
	}
	principal := transaction.Principal
	if action == "borrow" {
		principal = transaction.Amount
	}
	s.mu.RLock()
	accountScope := s.marginAccountScope
	s.mu.RUnlock()
	confirmed := fundingCarryMarginDebtEvent{
		Action: action, TransferID: transferID, Asset: asset, Amount: transaction.Amount, Principal: principal, InterestPaid: transaction.Interest,
		OccurredAt: time.UnixMilli(transaction.Timestamp).UTC(), AccountScope: accountScope,
	}
	if err := validateFundingCarryDebtEventIntegrity(confirmed, accountScope); err != nil {
		return fundingCarryMarginDebtEvent{}, err
	}
	return confirmed, nil
}

func (s *FundingCarryStrategy) recordMarginDebtEvent(ctx context.Context, action string, transferID int64, asset string, amount float64) error {
	confirmed, err := s.confirmMarginDebtTransaction(ctx, action, transferID, asset, amount)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.verifyDebtCommitLocked(ctx); err != nil {
		return err
	}
	if replay, err := s.debtEventReplayLocked(confirmed); replay || err != nil {
		return err
	}
	s.marginDebtEvents = append(s.marginDebtEvents, confirmed)
	if err := s.persistRuntimeStateLocked(); err != nil {
		s.marginDebtEvents = s.marginDebtEvents[:len(s.marginDebtEvents)-1]
		s.runtimeStateErr = err
		s.unownedExposure = true
		return fmt.Errorf("persist margin debt event: %w", err)
	}
	return nil
}

func (s *FundingCarryStrategy) restoreRuntimeState() error {
	s.mu.RLock()
	store := s.runtimeStateStore
	s.mu.RUnlock()
	if store == nil {
		return fmt.Errorf("durable runtime state store is required")
	}
	version, payload, found, err := store.LoadRuntimeState("funding_carry")
	if err != nil {
		return fmt.Errorf("load runtime state: %w", err)
	}
	if !found {
		return nil
	}
	state, err := decodeFundingCarryRuntimeState(version, payload, s.fut.GetName(), s.spot.GetName(), s.symbol)
	if err != nil {
		return err
	}
	s.mu.Lock()
	hasMarginEvidence := len(state.MarginDebtEvents) > 0 || state.MarginDebt > 0 || state.MarginBorrowTransferID > 0 || !state.MarginBorrowedAt.IsZero()
	if s.marginAccountScope != "" && state.MarginAccountScope != s.marginAccountScope && (state.MarginAccountScope != "" || hasMarginEvidence) {
		s.mu.Unlock()
		return fmt.Errorf("funding_carry runtime state margin account scope mismatch")
	}
	if state.MarginAccountScope != "" {
		s.marginAccountScope = state.MarginAccountScope
	}
	s.direction = state.Direction
	s.strategySpotQty = state.OwnedSpot
	s.spotQty = state.OwnedSpot
	s.futQty = state.OwnedFutures
	s.marginDebt = state.MarginDebt
	s.marginBorrowTransferID = state.MarginBorrowTransferID
	s.marginBorrowedAt = state.MarginBorrowedAt
	s.marginDebtEvents = append([]fundingCarryMarginDebtEvent(nil), state.MarginDebtEvents...)
	s.strategySpotKnown = true
	s.unownedExposure = false
	s.intentInFlight = false
	s.mu.Unlock()
	return nil
}

func decodeFundingCarryRuntimeState(version int, payload, futuresExchange, spotExchange, symbol string) (fundingCarryRuntimeState, error) {
	if version != fundingCarryRuntimeStateVersion {
		return fundingCarryRuntimeState{}, fmt.Errorf("unsupported funding_carry runtime state schema %d", version)
	}
	var state fundingCarryRuntimeState
	if err := json.Unmarshal([]byte(payload), &state); err != nil {
		return fundingCarryRuntimeState{}, fmt.Errorf("decode funding_carry runtime state: %w", err)
	}
	if state.Strategy != "funding_carry" || !strings.EqualFold(state.FuturesExchange, futuresExchange) ||
		!strings.EqualFold(state.SpotExchange, spotExchange) || !strings.EqualFold(state.Symbol, symbol) {
		return fundingCarryRuntimeState{}, fmt.Errorf("funding_carry runtime state identity mismatch")
	}
	if !state.OwnershipReady || state.IntentInFlight || state.ExposureUnknown ||
		state.Direction < DirectionNone || state.Direction > DirectionReverse ||
		!validRuntimeAmount(state.OwnedSpot) || !validRuntimeAmount(state.OwnedFutures) || !validRuntimeAmount(state.MarginDebt) ||
		state.MarginBorrowTransferID < 0 {
		return fundingCarryRuntimeState{}, fmt.Errorf("funding_carry runtime state is unresolved or invalid")
	}
	if state.Direction == DirectionNone && (state.OwnedSpot > 0 || state.OwnedFutures > 0 || state.MarginDebt > 0) {
		return fundingCarryRuntimeState{}, fmt.Errorf("funding_carry flat state contains owned exposure")
	}
	if state.Direction == DirectionNone && (state.MarginBorrowTransferID != 0 || !state.MarginBorrowedAt.IsZero()) {
		return fundingCarryRuntimeState{}, fmt.Errorf("funding_carry flat state contains margin borrow identity")
	}
	seenDebtEvents := make(map[fundingCarryDebtEventIdentity]struct{}, len(state.MarginDebtEvents))
	for _, event := range state.MarginDebtEvents {
		if (event.Action != "borrow" && event.Action != "repay") || event.TransferID <= 0 || strings.TrimSpace(event.Asset) == "" ||
			!validRuntimeAmount(event.Amount) || event.Amount <= 0 || event.OccurredAt.IsZero() {
			return fundingCarryRuntimeState{}, fmt.Errorf("funding_carry runtime state contains an invalid margin debt event")
		}
		if err := validateFundingCarryDebtEventIntegrity(event, state.MarginAccountScope); err != nil {
			return fundingCarryRuntimeState{}, err
		}
		identity := fundingCarryDebtEventKey(event)
		if _, exists := seenDebtEvents[identity]; exists {
			return fundingCarryRuntimeState{}, fmt.Errorf("funding_carry runtime state repeats a margin debt transaction identity")
		}
		seenDebtEvents[identity] = struct{}{}
	}
	return state, nil
}

func validRuntimeAmount(value float64) bool {
	return value >= 0 && !math.IsNaN(value) && !math.IsInf(value, 0)
}

func (s *FundingCarryStrategy) beginRuntimeIntent() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.runtimeStateErr != nil {
		s.unownedExposure = true
		return fmt.Errorf("previous runtime state persistence failed: %w", s.runtimeStateErr)
	}
	s.intentInFlight = true
	if err := s.persistRuntimeStateLocked(); err != nil {
		s.unownedExposure = true
		return err
	}
	return nil
}

func (s *FundingCarryStrategy) finishRuntimeIntent(success bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.intentInFlight = false
	if !success {
		s.unownedExposure = true
	}
	if err := s.persistRuntimeStateLocked(); err != nil {
		s.unownedExposure = true
		s.runtimeStateErr = err
		return err
	}
	return nil
}
