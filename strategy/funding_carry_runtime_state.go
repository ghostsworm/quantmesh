package strategy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"quantmesh/exchange"
	"quantmesh/storage"
)

const fundingCarryRuntimeStateVersion = 8

const (
	fundingCarryIntentPhasePrepared    = "prepared"
	fundingCarryIntentPhaseDispatching = "dispatching"
)

const fundingCarryPreSubmitRecoveryTimeout = 5 * time.Second

var errFundingCarryPreSubmitIntentCanceled = errors.New("funding_carry pre-submit intent was safely rolled back before any financial RPC")
var errFundingCarryPreSubmitRecoveryUnresolved = errors.New("funding_carry pre-submit intent recovery is unresolved")

type fundingCarryRuntimeStateContextWriter interface {
	SaveRuntimeStateContext(context.Context, string, int, string) error
}

type fundingCarryRuntimeState struct {
	Strategy                       string                        `json:"strategy"`
	FuturesExchange                string                        `json:"futures_exchange"`
	SpotExchange                   string                        `json:"spot_exchange"`
	Symbol                         string                        `json:"symbol"`
	MarginAccountScope             string                        `json:"margin_account_scope,omitempty"`
	OwnershipReady                 bool                          `json:"ownership_ready"`
	IntentInFlight                 bool                          `json:"intent_in_flight"`
	IntentPhase                    string                        `json:"intent_phase,omitempty"`
	StandaloneSpotOwned            bool                          `json:"standalone_spot_owned,omitempty"`
	ExposureUnknown                bool                          `json:"exposure_unknown"`
	Direction                      CarryDirection                `json:"direction"`
	OwnedSpot                      float64                       `json:"owned_spot"`
	OwnedFutures                   float64                       `json:"owned_futures"`
	MarginDebt                     float64                       `json:"margin_debt"`
	MarginBorrowTransferID         int64                         `json:"margin_borrow_transfer_id,omitempty"`
	MarginBorrowedAt               time.Time                     `json:"margin_borrowed_at,omitempty"`
	MarginDebtEvents               []fundingCarryMarginDebtEvent `json:"margin_debt_events,omitempty"`
	MarginRepayIntent              *fundingCarryRepayIntent      `json:"margin_repay_intent,omitempty"`
	MarginCoverOrders              []fundingCarryCoverOrder      `json:"margin_cover_orders,omitempty"`
	MarginCoverIntent              *fundingCarryCoverIntent      `json:"margin_cover_intent,omitempty"`
	MarginCloseVerificationPending bool                          `json:"margin_close_verification_pending,omitempty"`
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
		IntentPhase:         s.intentPhase,
		StandaloneSpotOwned: s.direction == DirectionNone && s.strategySpotQty > 0,
		ExposureUnknown:     s.unownedExposure, Direction: s.direction, OwnedSpot: s.strategySpotQty,
		OwnedFutures: s.futQty, MarginDebt: s.marginDebt,
		MarginBorrowTransferID: s.marginBorrowTransferID, MarginBorrowedAt: s.marginBorrowedAt,
		MarginDebtEvents:               append([]fundingCarryMarginDebtEvent(nil), s.marginDebtEvents...),
		MarginRepayIntent:              cloneFundingCarryRepayIntent(s.marginRepayIntent),
		MarginCoverOrders:              cloneFundingCarryCoverOrders(s.marginCoverOrders),
		MarginCoverIntent:              cloneFundingCarryCoverIntent(s.marginCoverIntent),
		MarginCloseVerificationPending: s.marginCloseVerificationPending,
	}
}

func (s *FundingCarryStrategy) persistRuntimeStateLocked() error {
	_, err := s.persistRuntimeStateSnapshotLocked()
	return err
}

func (s *FundingCarryStrategy) persistRuntimeStateSnapshotLocked() (string, error) {
	return s.persistRuntimeStateSnapshotContextLocked(nil)
}

func (s *FundingCarryStrategy) persistRuntimeStateSnapshotContextLocked(ctx context.Context) (string, error) {
	if s.runtimeStateStore == nil {
		return "", fmt.Errorf("funding_carry runtime state store is unavailable")
	}
	if ownerErr := verifyStrategyWalletRuntimeOwner(s.openingGate); ownerErr != nil {
		return "", fmt.Errorf("refuse funding_carry runtime state write without verified ownership: %w", ownerErr)
	}
	payload, err := json.Marshal(s.runtimeStateSnapshotLocked())
	if err != nil {
		return "", fmt.Errorf("encode funding_carry runtime state: %w", err)
	}
	var saveErr error
	if ctx != nil {
		if writer, ok := s.runtimeStateStore.(fundingCarryRuntimeStateContextWriter); ok {
			saveErr = writer.SaveRuntimeStateContext(ctx, "funding_carry", fundingCarryRuntimeStateVersion, string(payload))
		} else {
			saveErr = s.runtimeStateStore.SaveRuntimeState("funding_carry", fundingCarryRuntimeStateVersion, string(payload))
		}
	} else {
		saveErr = s.runtimeStateStore.SaveRuntimeState("funding_carry", fundingCarryRuntimeStateVersion, string(payload))
	}
	if saveErr != nil {
		return string(payload), fmt.Errorf("persist funding_carry runtime state: %w", saveErr)
	}
	s.runtimeStateErr = nil
	return string(payload), nil
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
	if transaction.TransferID != transferID || !strings.EqualFold(strings.TrimSpace(transaction.Asset), strings.TrimSpace(asset)) ||
		!strings.EqualFold(strings.TrimSpace(transaction.Status), "CONFIRMED") || transaction.Timestamp <= 0 ||
		!fundingCarryFinancialAmountsMatch(transaction.Amount, amount) ||
		!validRuntimeAmount(transaction.Principal) || !validRuntimeAmount(transaction.Interest) ||
		!fundingCarryFinancialAmountsMatch(transaction.Principal+transaction.Interest, transaction.Amount) ||
		(action == "borrow" && !fundingCarryFinancialAmountsMatch(transaction.Principal, transaction.Amount)) {
		return fundingCarryMarginDebtEvent{}, fmt.Errorf("margin %s transaction %d does not match confirmed asset and amount", action, transferID)
	}
	principal := transaction.Principal
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
		if errors.Is(err, storage.ErrFundingCarryRuntimeStateCommitConfirmedCanceled) {
			// SQLStorage has positively confirmed this exact generation-fenced
			// snapshot. Keep the matching in-memory debt event; the caller still
			// receives cancellation and must not continue its current operation.
			return fmt.Errorf("persist confirmed margin debt event after caller cancellation: %w", err)
		}
		s.marginDebtEvents = s.marginDebtEvents[:len(s.marginDebtEvents)-1]
		s.runtimeStateErr = err
		s.unownedExposure = true
		return fmt.Errorf("persist margin debt event: %w", err)
	}
	return nil
}

func (s *FundingCarryStrategy) restoreRuntimeState() error {
	return s.restoreRuntimeStateContext(context.Background())
}

func (s *FundingCarryStrategy) restoreRuntimeStateContext(ctx context.Context) error {
	if ctx == nil {
		return fmt.Errorf("runtime state recovery requires context")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.RLock()
	store := s.runtimeStateStore
	s.mu.RUnlock()
	if store == nil {
		return fmt.Errorf("durable runtime state store is required")
	}
	reader, ok := store.(RuntimeStateContextReader)
	if !ok {
		return fmt.Errorf("runtime state recovery requires cancellable checkpoint reads")
	}
	version, payload, found, err := reader.LoadRuntimeStateContext(ctx, "funding_carry")
	if err != nil {
		return fmt.Errorf("load runtime state: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if !found {
		return nil
	}
	state, err := decodeFundingCarryRuntimeState(version, payload, s.fut.GetName(), s.spot.GetName(), s.symbol)
	if err != nil {
		return err
	}
	if err := validateFundingCarryDebtAsset(state, s.spot.GetBaseAsset()); err != nil {
		return err
	}
	s.mu.Lock()
	if err := ctx.Err(); err != nil {
		s.mu.Unlock()
		return err
	}
	hasMarginEvidence := len(state.MarginDebtEvents) > 0 || len(state.MarginCoverOrders) > 0 || state.MarginDebt > 0 || state.MarginBorrowTransferID > 0 || !state.MarginBorrowedAt.IsZero()
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
	s.marginCoverOrders = cloneFundingCarryCoverOrders(state.MarginCoverOrders)
	s.marginCoverIntent = cloneFundingCarryCoverIntent(state.MarginCoverIntent)
	s.marginCloseVerificationPending = state.MarginCloseVerificationPending
	s.strategySpotKnown = true
	s.unownedExposure = false
	s.intentInFlight = false
	s.intentPhase = state.IntentPhase
	s.intentSourcePayload = ""
	s.mu.Unlock()
	return nil
}

func decodeFundingCarryRuntimeState(version int, payload, futuresExchange, spotExchange, symbol string) (fundingCarryRuntimeState, error) {
	return decodeFundingCarryRuntimeStateForRecovery(version, payload, futuresExchange, spotExchange, symbol, false)
}

func decodeFundingCarryRuntimeStateForRecovery(version int, payload, futuresExchange, spotExchange, symbol string, allowPending bool) (fundingCarryRuntimeState, error) {
	if version != 1 && version != 2 && version != 3 && version != 4 && version != 5 && version != 6 && version != 7 && version != fundingCarryRuntimeStateVersion {
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
	if (version < 8 && state.IntentPhase != "") || (state.IntentPhase != "" &&
		state.IntentPhase != fundingCarryIntentPhasePrepared && state.IntentPhase != fundingCarryIntentPhaseDispatching) ||
		(!state.IntentInFlight && state.IntentPhase != "") {
		return fundingCarryRuntimeState{}, fmt.Errorf("funding_carry runtime state contains an invalid intent phase")
	}
	if version < 7 {
		for _, record := range state.MarginCoverOrders {
			if record.TerminalStatus != "" {
				return fundingCarryRuntimeState{}, fmt.Errorf("terminal partial cover requires runtime schema7")
			}
		}
	}
	if !state.OwnershipReady || (!allowPending && (state.IntentInFlight || state.ExposureUnknown || state.MarginRepayIntent != nil || state.MarginCoverIntent != nil || state.MarginCloseVerificationPending)) ||
		state.Direction < DirectionNone || state.Direction > DirectionReverse ||
		!validRuntimeAmount(state.OwnedSpot) || !validRuntimeAmount(state.OwnedFutures) || !validRuntimeAmount(state.MarginDebt) ||
		state.MarginBorrowTransferID < 0 {
		return fundingCarryRuntimeState{}, fmt.Errorf("funding_carry runtime state is unresolved or invalid")
	}
	if state.StandaloneSpotOwned && (version < 8 || state.Direction != DirectionNone || state.OwnedSpot <= 0 || state.OwnedFutures > 0 || state.MarginDebt > 0) {
		return fundingCarryRuntimeState{}, fmt.Errorf("standalone spot ownership marker is inconsistent")
	}
	if state.Direction == DirectionNone && ((state.OwnedSpot > 0 && !state.StandaloneSpotOwned) || state.OwnedFutures > 0 || state.MarginDebt > 0) {
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
	if err := validateFundingCarryDebtPrincipalBalance(state); err != nil {
		return fundingCarryRuntimeState{}, err
	}
	if err := validateFundingCarryCoverOrders(state, allowPending); err != nil {
		return fundingCarryRuntimeState{}, err
	}
	if err := validateFundingCarryCoverIntent(state); err != nil {
		return fundingCarryRuntimeState{}, err
	}
	if !allowPending && len(state.MarginCoverOrders) > 0 {
		if err := requireNoFundingCarryCoverRemaining(state, state.MarginCoverOrders[0].Asset); err != nil {
			return fundingCarryRuntimeState{}, err
		}
	}
	return state, nil
}

func validRuntimeAmount(value float64) bool {
	return value >= 0 && !math.IsNaN(value) && !math.IsInf(value, 0)
}

func (s *FundingCarryStrategy) beginRuntimeIntent(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.verifyDebtCommitLocked(ctx); err != nil {
		return err
	}
	if s.unownedExposure || s.intentInFlight || s.marginCloseVerificationPending || s.marginRepayIntent != nil || s.marginCoverIntent != nil || hasUnverifiedFundingCarryCover(s.marginCoverOrders) {
		return fmt.Errorf("previous funding_carry intent still requires reconciliation")
	}
	if err := requireNoFundingCarryCoverRemaining(s.coverRemainingStateLocked(), s.spot.GetBaseAsset()); err != nil {
		return err
	}
	if s.runtimeStateErr != nil {
		s.unownedExposure = true
		return fmt.Errorf("previous runtime state persistence failed: %w", s.runtimeStateErr)
	}
	previousSnapshot := s.runtimeStateSnapshotLocked()
	previousPayload, err := json.Marshal(previousSnapshot)
	if err != nil {
		return fmt.Errorf("encode pre-submit funding_carry source snapshot: %w", err)
	}
	previousUnknown := s.unownedExposure
	previousRuntimeStateErr := s.runtimeStateErr
	s.intentInFlight = true
	s.intentPhase = fundingCarryIntentPhasePrepared
	intentPayload, err := s.persistRuntimeStateSnapshotContextLocked(ctx)
	if err != nil {
		if errors.Is(err, storage.ErrFundingCarryRuntimeStateCommitConfirmedCanceled) ||
			errors.Is(err, storage.ErrFundingCarryRuntimeStateCommitOutcomeUnknown) {
			if rollbackErr := s.rollbackExactPreSubmitIntentLocked(
				fundingCarryRuntimeStateVersion, string(previousPayload), intentPayload,
			); rollbackErr == nil {
				s.intentInFlight = previousSnapshot.IntentInFlight
				s.intentPhase = previousSnapshot.IntentPhase
				s.unownedExposure = previousUnknown
				s.runtimeStateErr = previousRuntimeStateErr
				s.intentSourcePayload = ""
				return errors.Join(errFundingCarryPreSubmitIntentCanceled, err)
			} else {
				err = errors.Join(err, rollbackErr)
			}
		}
		err = errors.Join(errFundingCarryPreSubmitRecoveryUnresolved, err)
		s.unownedExposure = true
		s.runtimeStateErr = err
		s.intentSourcePayload = ""
		return err
	}
	s.intentSourcePayload = string(previousPayload)
	return nil
}

// markRuntimeIntentDispatching must commit before the first financial write
// RPC. A durable prepared phase is recoverable after restart; dispatching is
// intentionally ambiguous and remains blocked until external evidence exists.
func (s *FundingCarryStrategy) markRuntimeIntentDispatching(ctx context.Context) error {
	if ctx == nil {
		return fmt.Errorf("funding_carry dispatch marker requires context")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.intentInFlight {
		return fmt.Errorf("funding_carry financial write requires a prepared runtime intent")
	}
	if s.intentPhase == fundingCarryIntentPhaseDispatching {
		return nil
	}
	if s.intentPhase != fundingCarryIntentPhasePrepared {
		return fmt.Errorf("funding_carry financial write requires a prepared runtime intent")
	}
	if err := ctx.Err(); err != nil {
		preparedPayload, encodeErr := json.Marshal(s.runtimeStateSnapshotLocked())
		if encodeErr == nil && s.intentSourcePayload != "" {
			if rollbackErr := s.rollbackExactPreSubmitIntentLocked(
				fundingCarryRuntimeStateVersion, s.intentSourcePayload, string(preparedPayload),
			); rollbackErr == nil {
				s.intentInFlight = false
				s.intentPhase = ""
				s.unownedExposure = false
				s.runtimeStateErr = nil
				s.intentSourcePayload = ""
				return errors.Join(errFundingCarryPreSubmitIntentCanceled, err)
			} else {
				err = errors.Join(err, rollbackErr)
			}
		} else if encodeErr != nil {
			err = errors.Join(err, encodeErr)
		}
		err = errors.Join(errFundingCarryPreSubmitRecoveryUnresolved, err)
		s.unownedExposure = true
		s.runtimeStateErr = err
		return err
	}
	s.intentPhase = fundingCarryIntentPhaseDispatching
	intentPayload, err := s.persistRuntimeStateSnapshotContextLocked(ctx)
	if err == nil {
		return nil
	}
	if (errors.Is(err, storage.ErrFundingCarryRuntimeStateCommitConfirmedCanceled) ||
		errors.Is(err, storage.ErrFundingCarryRuntimeStateCommitOutcomeUnknown)) && s.intentSourcePayload != "" {
		if rollbackErr := s.rollbackExactPreSubmitIntentLocked(
			fundingCarryRuntimeStateVersion, s.intentSourcePayload, intentPayload,
		); rollbackErr == nil {
			s.intentInFlight = false
			s.intentPhase = ""
			s.unownedExposure = false
			s.runtimeStateErr = nil
			s.intentSourcePayload = ""
			return errors.Join(errFundingCarryPreSubmitIntentCanceled, err)
		} else {
			err = errors.Join(err, rollbackErr)
		}
	}
	err = errors.Join(errFundingCarryPreSubmitRecoveryUnresolved, err)
	s.unownedExposure = true
	s.runtimeStateErr = err
	return err
}

func (s *FundingCarryStrategy) markRuntimeIntentDispatchingIfActive(ctx context.Context) error {
	s.mu.RLock()
	active := s.intentInFlight
	s.mu.RUnlock()
	if !active {
		return nil
	}
	return s.markRuntimeIntentDispatching(ctx)
}

// Caller holds s.mu. Lock order is strategy mutex -> owner verification ->
// context-bounded durable read -> generation-fenced conditional SQL write.
// No exchange RPC or wallet/distributed-lock acquisition is allowed here.
func (s *FundingCarryStrategy) rollbackExactPreSubmitIntentLocked(sourceVersion int, sourcePayload, intentPayload string) error {
	store := s.runtimeStateStore
	reader, canRead := store.(RuntimeStateContextReader)
	writer, canWrite := store.(RuntimeStateConditionalWriter)
	if !canRead || !canWrite {
		return fmt.Errorf("pre-submit intent rollback requires context reads and generation-fenced conditional writes")
	}
	ctx, cancel := context.WithTimeout(context.Background(), fundingCarryPreSubmitRecoveryTimeout)
	defer cancel()

	version, payload, found, err := reader.LoadRuntimeStateContext(ctx, "funding_carry")
	if err != nil {
		return fmt.Errorf("read back confirmed pre-submit intent: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("confirmed pre-submit intent read exceeded recovery context: %w", err)
	}
	if !found || version != fundingCarryRuntimeStateVersion || payload != intentPayload {
		return fmt.Errorf("durable pre-submit intent no longer exactly matches this write")
	}

	// The FundingCarry production adapter implements this conditional write
	// with its claimed owner generation. CAS conflict or lost generation is
	// intentionally terminal for this attempt; never retry a financial action.
	saved, err := writer.CompareAndSwapRuntimeState(ctx, "funding_carry", version, payload, sourceVersion, sourcePayload)
	if err != nil {
		if errors.Is(err, storage.ErrFundingCarryRuntimeStateCommitConfirmedCanceled) ||
			errors.Is(err, storage.ErrFundingCarryRuntimeStateCommitOutcomeUnknown) {
			// The original caller context may be expired or the storage receipt
			// may be uncertain. Use a fresh bounded read to prove that S0 is the
			// durable source before restoring memory.
			verifyCtx, verifyCancel := context.WithTimeout(context.Background(), fundingCarryPreSubmitRecoveryTimeout)
			defer verifyCancel()
			verifiedVersion, verifiedPayload, verifiedFound, verifyErr := reader.LoadRuntimeStateContext(verifyCtx, "funding_carry")
			if verifyErr != nil {
				return fmt.Errorf("read back confirmed pre-submit rollback: %w", verifyErr)
			}
			if !verifiedFound || verifiedVersion != sourceVersion || verifiedPayload != sourcePayload {
				return fmt.Errorf("confirmed pre-submit rollback no longer matches exact source snapshot")
			}
			return nil
		}
		return fmt.Errorf("generation-fenced pre-submit intent rollback is unresolved: %w", err)
	}
	if !saved {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("pre-submit intent rollback context ended without confirmation: %w", err)
		}
		return fmt.Errorf("generation-fenced pre-submit intent rollback conflicted with a newer snapshot")
	}
	return nil
}

func (s *FundingCarryStrategy) finishRuntimeIntent(ctx context.Context, success bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.verifyDebtCommitLocked(ctx); err != nil {
		s.unownedExposure = true // local latch only; the stale worker must not write durable state
		return err
	}
	remainingErr := requireNoFundingCarryCoverRemaining(s.coverRemainingStateLocked(), s.spot.GetBaseAsset())
	if s.marginRepayIntent != nil || s.marginCoverIntent != nil || s.marginCloseVerificationPending || remainingErr != nil {
		s.unownedExposure = true
		if err := s.persistRuntimeStateLocked(); err != nil {
			s.runtimeStateErr = err
			return err
		}
		return errors.Join(fmt.Errorf("margin repayment or cover submission still requires reconciliation"), remainingErr)
	}
	previousIntent := s.intentInFlight
	previousPhase := s.intentPhase
	s.intentInFlight = false
	s.intentPhase = ""
	if !success {
		s.unownedExposure = true
	}
	if err := s.persistRuntimeStateLocked(); err != nil {
		if errors.Is(err, storage.ErrFundingCarryRuntimeStateCommitConfirmedCanceled) {
			// Storage confirms this exact final intent snapshot. Keep memory at
			// the durable state, propagate cancellation, and do not reopen the
			// already completed intent in this process.
			s.runtimeStateErr = nil
			s.intentSourcePayload = ""
			return err
		}
		s.intentInFlight = previousIntent
		s.intentPhase = previousPhase
		s.unownedExposure = true
		s.runtimeStateErr = err
		return err
	}
	s.intentSourcePayload = ""
	return nil
}
