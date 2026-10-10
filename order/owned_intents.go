package order

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"quantmesh/exchange"
	"quantmesh/execution"
	"quantmesh/utils"
)

// ownedIntent records economic intent before the physical call. Never infer
// ownership from BUY/SELL: another bot or a protective close can have that side.
type ownedIntent struct {
	request           OrderRequest
	opening           bool
	order             *Order
	unknown           bool
	ledgerPending     bool
	ledgerReason      string
	ledgerPayload     []byte
	revision          int64
	durablePayload    []byte
	uncertainPayload  []byte
	uncertainRevision int64
	attempts          int
	attemptPrice      float64
	rejected          bool
	settled           bool
}

type retryableZeroFillSettlementError struct{ cause error }

func (e retryableZeroFillSettlementError) Error() string { return e.cause.Error() }
func (e retryableZeroFillSettlementError) Unwrap() error { return e.cause }

// OwnedIntentSnapshot is a read-only projection of one journal-loaded intent.
// It deliberately excludes account credentials and opaque account tokens.
type OwnedIntentSnapshot struct {
	BotID             string
	Symbol            string
	StrategyName      string
	StrategyType      string
	Side              string
	PositionSide      string
	OrderSource       string
	ExposureKey       string
	ReduceOnly        bool
	Opening           bool
	RequestedQuantity float64
	VenueOrderID      int64
	ClientOrderID     string
	VenueStatus       string
	Terminal          bool
	ExecutedQuantity  float64
	Revision          int64
	Unknown           bool
	LedgerPending     bool
	Settled           bool
	Rejected          bool
}

// ReadOwnedIntentSnapshot resolves exactly one already-loaded intent by both
// venue order ID and canonical client order ID. VenueStatus is the latest
// journal-loaded venue observation, not a fresh exchange query. It never reads
// from or writes to the journal, and never changes intent or gate state.
func (oe *ExchangeOrderExecutor) ReadOwnedIntentSnapshot(venueOrderID int64, canonicalClientOrderID string) (OwnedIntentSnapshot, error) {
	if venueOrderID <= 0 || canonicalClientOrderID == "" || strings.TrimSpace(canonicalClientOrderID) != canonicalClientOrderID {
		return OwnedIntentSnapshot{}, fmt.Errorf("venue order ID and canonical client order ID are required")
	}
	oe.intentMu.Lock()
	defer oe.intentMu.Unlock()
	if !oe.journalRequired || !oe.journalLoaded || oe.intentJournal == nil || oe.intentScopeKey == "" {
		return OwnedIntentSnapshot{}, fmt.Errorf("owned intent journal is not loaded")
	}

	var matched *ownedIntent
	for key, intent := range oe.intents {
		if intent == nil {
			continue
		}
		cidMatch := key == canonicalClientOrderID || intent.request.ClientOrderID == canonicalClientOrderID
		orderMatch := intent.order != nil && intent.order.OrderID == venueOrderID
		if cidMatch || orderMatch {
			if !cidMatch || !orderMatch || matched != nil {
				return OwnedIntentSnapshot{}, fmt.Errorf("owned intent identity is mismatched or ambiguous")
			}
			matched = intent
		}
	}
	if matched == nil {
		return OwnedIntentSnapshot{}, fmt.Errorf("no exact owned intent match")
	}
	request, venueOrder := matched.request, matched.order
	if request.ClientOrderID != canonicalClientOrderID || request.Symbol == "" || request.Symbol != oe.symbol ||
		request.StrategyName == "" || request.StrategyType == "" || (request.Side != "BUY" && request.Side != "SELL") ||
		request.Quantity <= 0 || math.IsNaN(request.Quantity) || math.IsInf(request.Quantity, 0) ||
		matched.opening != oe.isOpeningOrder(&request) || matched.revision <= 0 || venueOrder == nil ||
		venueOrder.OrderID != venueOrderID || venueOrder.Symbol != request.Symbol || venueOrder.Side != request.Side ||
		venueOrder.Quantity != request.Quantity || venueOrder.ClientOrderID != "" && !oe.matchesOwnedClientOrderID(canonicalClientOrderID, venueOrder.ClientOrderID) ||
		venueOrder.ExecutedQty < 0 || math.IsNaN(venueOrder.ExecutedQty) || math.IsInf(venueOrder.ExecutedQty, 0) ||
		venueOrder.ExecutedQty > request.Quantity || strings.TrimSpace(venueOrder.Status) == "" {
		return OwnedIntentSnapshot{}, fmt.Errorf("owned intent fields conflict or are incomplete")
	}
	return OwnedIntentSnapshot{
		BotID: oe.botID, Symbol: request.Symbol, StrategyName: request.StrategyName, StrategyType: request.StrategyType,
		Side: request.Side, PositionSide: request.PositionSide, OrderSource: request.OrderSource,
		ExposureKey: request.ExposureKey, ReduceOnly: request.ReduceOnly,
		Opening: matched.opening, RequestedQuantity: request.Quantity,
		VenueOrderID: venueOrder.OrderID, ClientOrderID: canonicalClientOrderID, VenueStatus: venueOrder.Status,
		Terminal: terminalOrderStatus(venueOrder.Status), ExecutedQuantity: venueOrder.ExecutedQty,
		Revision: matched.revision, Unknown: matched.unknown, LedgerPending: matched.ledgerPending,
		Settled: matched.settled, Rejected: matched.rejected,
	}, nil
}

// SetUnknownOrderHandler is configured before the runtime starts. The callback
// runs without intentMu and must not try to synchronously drain this same call.
func (oe *ExchangeOrderExecutor) SetUnknownOrderHandler(handler func(OrderRequest)) {
	oe.unknownOrderHandler = handler
}

// SetTradeLedgerRecoveryHandler installs the owner-scoped idempotent replay
// used for durable trade rows that failed before a prior process stopped.
func (oe *ExchangeOrderExecutor) SetTradeLedgerRecoveryHandler(handler func(context.Context, execution.IntentScope, int64, float64, []byte) error) {
	oe.tradeLedgerRecoveryHandler = handler
}

func (oe *ExchangeOrderExecutor) intentAcceptanceObserved(cid string) bool {
	oe.intentMu.Lock()
	defer oe.intentMu.Unlock()
	i := oe.intents[cid]
	return i != nil && i.order != nil
}

// OwnsOpenOrder proves that a venue open order belongs to a retained runtime
// intent. Matching by side, symbol, or price is deliberately insufficient.
func (oe *ExchangeOrderExecutor) OwnsOpenOrder(venueOrder *exchange.Order) bool {
	if venueOrder == nil || (venueOrder.OrderID <= 0 && venueOrder.ClientOrderID == "") || strings.TrimSpace(venueOrder.Symbol) == "" ||
		!strings.EqualFold(strings.TrimSpace(venueOrder.Symbol), strings.TrimSpace(oe.symbol)) {
		return false
	}
	oe.intentMu.Lock()
	defer oe.intentMu.Unlock()
	for clientOrderID, intent := range oe.intents {
		if intent == nil || intent.settled || intent.rejected || !strings.EqualFold(strings.TrimSpace(intent.request.Symbol), strings.TrimSpace(venueOrder.Symbol)) {
			continue
		}
		if intent.order != nil && terminalOrderStatus(intent.order.Status) {
			continue
		}
		if venueOrder.ClientOrderID != "" {
			if oe.matchesOwnedClientOrderID(clientOrderID, venueOrder.ClientOrderID) {
				return true
			}
			continue
		}
		if intent.order != nil && intent.order.OrderID == venueOrder.OrderID {
			return true
		}
	}
	return false
}

func (oe *ExchangeOrderExecutor) matchesOwnedClientOrderID(clientOrderID, candidate string) bool {
	return candidate == clientOrderID || utils.AddBrokerPrefix(strings.ToLower(oe.exchange.GetName()), clientOrderID) == candidate
}

// IntentStrategyType returns the durable strategy identity for an owned CID.
func (oe *ExchangeOrderExecutor) IntentStrategyType(clientOrderID string) (string, string, bool) {
	oe.intentMu.Lock()
	defer oe.intentMu.Unlock()
	for cid, intent := range oe.intents {
		if oe.matchesOwnedClientOrderID(cid, clientOrderID) {
			return intent.request.StrategyName, intent.request.StrategyType, true
		}
	}
	return "", "", false
}

// SettledIntentExecutedQty returns the durable cumulative quantity for a
// settled owner intent. It lets runtime callbacks suppress duplicate terminal
// accounting while still detecting any venue quantity that exceeds settlement.
func (oe *ExchangeOrderExecutor) SettledIntentExecutedQty(clientOrderID string) (float64, bool) {
	oe.intentMu.Lock()
	defer oe.intentMu.Unlock()
	for cid, intent := range oe.intents {
		if intent == nil || !intent.settled || intent.order == nil || !oe.matchesOwnedClientOrderID(cid, clientOrderID) {
			continue
		}
		return intent.order.ExecutedQty, true
	}
	return 0, false
}

// OwnedIntentClientOrderID resolves exchange-prefixed callback IDs to the
// canonical ID used as the execution journal key.
func (oe *ExchangeOrderExecutor) OwnedIntentClientOrderID(clientOrderID string) (string, bool) {
	oe.intentMu.Lock()
	defer oe.intentMu.Unlock()
	for cid := range oe.intents {
		if oe.matchesOwnedClientOrderID(cid, clientOrderID) {
			return cid, true
		}
	}
	return "", false
}

// SettleIntent is called only after the owning strategy has durably accounted
// for the terminal venue fill in its own runtime state.
func (oe *ExchangeOrderExecutor) SettleIntent(ctx context.Context, clientOrderID string) error {
	return oe.settleIntent(ctx, clientOrderID, false)
}

// SettleReconciledIntent settles an intent only for its persisted strategy owner.
// It also makes retries idempotent when settlement succeeded but the strategy's
// durable outbox acknowledgement did not survive a crash.
func (oe *ExchangeOrderExecutor) SettleReconciledIntent(ctx context.Context, clientOrderID, strategyName string) error {
	if ctx == nil || strings.TrimSpace(clientOrderID) == "" || strings.TrimSpace(strategyName) == "" {
		return fmt.Errorf("reconciled intent settlement requires context, client order ID, and strategy owner")
	}
	owner, _, found := oe.IntentStrategyType(clientOrderID)
	if found {
		if owner != strategyName {
			return fmt.Errorf("reconciled intent owner mismatch")
		}
		if err := oe.SettleIntent(ctx, clientOrderID); err == nil {
			return nil
		} else if recoveryErr := oe.SettleRecoveredIntent(ctx, clientOrderID, strategyName); recoveryErr == nil {
			return nil
		} else {
			return errors.Join(err, recoveryErr)
		}
	}
	return oe.verifyPersistedSettledIntent(ctx, clientOrderID, strategyName)
}

// SettleZeroFillIntent is stricter than ordinary strategy settlement: it is
// reserved for an accounted zero-fill callback and requires the venue's
// authoritative terminal query and merged journal cursor to remain zero-fill.
func (oe *ExchangeOrderExecutor) SettleZeroFillIntent(ctx context.Context, clientOrderID string) error {
	delay := 200 * time.Millisecond
	for {
		err := oe.settleIntent(ctx, clientOrderID, true)
		if err == nil {
			return nil
		}
		var retryable retryableZeroFillSettlementError
		if !errors.As(err, &retryable) {
			return err
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return fmt.Errorf("retry zero-fill settlement for %s: %w", clientOrderID, ctx.Err())
		case <-timer.C:
		}
		if delay < 2*time.Second {
			delay *= 2
			if delay > 2*time.Second {
				delay = 2 * time.Second
			}
		}
	}
}

func zeroFillTerminalStatus(status string) bool {
	switch strings.ToUpper(strings.TrimSpace(status)) {
	case "CANCELED", "CANCELLED", "EXPIRED", "REJECTED":
		return true
	default:
		return false
	}
}

func (oe *ExchangeOrderExecutor) settleIntent(ctx context.Context, clientOrderID string, zeroFillOnly bool) error {
	oe.intentMu.Lock()
	intent := oe.intents[clientOrderID]
	if intent == nil || intent.order == nil || intent.unknown || intent.ledgerPending || intent.rejected {
		oe.intentMu.Unlock()
		return fmt.Errorf("execution intent %s is not eligible for settlement", clientOrderID)
	}
	if intent.settled {
		oe.intentMu.Unlock()
		return nil
	}
	if zeroFillOnly && intent.order.ExecutedQty != 0 {
		oe.intentMu.Unlock()
		return fmt.Errorf("execution intent %s has previously observed fills", clientOrderID)
	}
	orderID := intent.order.OrderID
	oe.intentMu.Unlock()
	if orderID <= 0 {
		return fmt.Errorf("execution intent %s has invalid venue order id", clientOrderID)
	}
	observed, err := oe.exchange.GetOrder(ctx, oe.symbol, orderID)
	if err != nil {
		if zeroFillOnly {
			return retryableZeroFillSettlementError{cause: fmt.Errorf("verify terminal order %d: %w", orderID, err)}
		}
		return fmt.Errorf("verify terminal order %d before settlement: %w", orderID, err)
	}
	if observed == nil {
		if zeroFillOnly {
			return retryableZeroFillSettlementError{cause: fmt.Errorf("terminal venue order %d is not visible yet", orderID)}
		}
		return fmt.Errorf("execution intent %s has no matching terminal venue order", clientOrderID)
	}
	if observed.OrderID != orderID || observed.Symbol != oe.symbol ||
		(observed.ClientOrderID != "" && !oe.matchesOwnedClientOrderID(clientOrderID, observed.ClientOrderID)) {
		return fmt.Errorf("execution intent %s has no matching terminal venue order", clientOrderID)
	}
	if !terminalOrderStatus(string(observed.Status)) {
		if zeroFillOnly {
			return retryableZeroFillSettlementError{cause: fmt.Errorf("venue order %d is not terminal yet", orderID)}
		}
		return fmt.Errorf("execution intent %s has no matching terminal venue order", clientOrderID)
	}
	if zeroFillOnly && (!zeroFillTerminalStatus(string(observed.Status)) || observed.ExecutedQty != 0 || math.IsNaN(observed.ExecutedQty) || math.IsInf(observed.ExecutedQty, 0)) {
		return fmt.Errorf("execution intent %s has no matching zero-fill terminal venue order", clientOrderID)
	}
	observed.ClientOrderID = clientOrderID
	if !oe.ObserveOrder(observed) {
		return fmt.Errorf("terminal order %d observation conflicts with its persisted intent", orderID)
	}
	oe.intentMu.Lock()
	defer oe.intentMu.Unlock()
	intent = oe.intents[clientOrderID]
	if intent == nil || intent.unknown || intent.ledgerPending || intent.order == nil || !terminalOrderStatus(intent.order.Status) {
		return fmt.Errorf("execution intent %s became unresolved during settlement", clientOrderID)
	}
	if zeroFillOnly && intent.order.ExecutedQty != 0 {
		return fmt.Errorf("execution intent %s acquired fills during settlement", clientOrderID)
	}
	intent.settled = true
	if err := oe.saveJournalIntentLocked(intent); err != nil {
		intent.settled = false
		intent.unknown = true
		oe.blockJournalFailureLocked()
		return fmt.Errorf("persist settled execution intent %s: %w", clientOrderID, err)
	}
	return nil
}

func (oe *ExchangeOrderExecutor) beginIntent(req *OrderRequest) error {
	oe.intentMu.Lock()
	defer oe.intentMu.Unlock()
	if oe.journalRequired && (oe.intentJournal == nil || !oe.journalLoaded) {
		return fmt.Errorf("intent journal is not ready")
	}
	if oe.intents == nil {
		oe.intents = make(map[string]*ownedIntent)
	}
	if _, exists := oe.intents[req.ClientOrderID]; exists {
		return fmt.Errorf("clientOrderID %s already owned: %w", req.ClientOrderID, execution.ErrIntentPending)
	}
	for _, intent := range oe.intents {
		if intent.unknown && !intent.opening && intent.request.Symbol == req.Symbol && intent.request.Side == req.Side {
			return fmt.Errorf("uncertain close %s must be reconciled first: %w", intent.request.ClientOrderID, execution.ErrIntentPending)
		}
	}
	if oe.exposureRequired && oe.exposureBook == nil && oe.isOpeningOrder(req) {
		return fmt.Errorf("opening exposure book is not configured: %w", execution.ErrExposureUnverified)
	}
	if oe.exposureBook != nil {
		oe.refreshExposureMark()
		exposure, err := oe.exposureRequest(req)
		if err != nil {
			return err
		}
		if err = oe.exposureBook.Reserve(exposure, time.Now()); err != nil {
			return err
		}
	}
	intent := &ownedIntent{request: *req, opening: oe.isOpeningOrder(req)}
	oe.intents[req.ClientOrderID] = intent
	if err := oe.saveJournalIntentLocked(intent); err != nil {
		oe.blockJournalFailureLocked()
		// No venue call has happened. Keep identity, but release the unused
		// admission reservation; recovery still checks the uncertain DB write.
		if oe.exposureBook != nil {
			if releaseErr := oe.exposureBook.Reject(req.ClientOrderID); releaseErr != nil {
				return errors.Join(err, releaseErr)
			}
		}
		return fmt.Errorf("persist intent before submission: %w", err)
	}
	return nil
}

func (oe *ExchangeOrderExecutor) finishIntent(req *OrderRequest, placed *Order, err error) (resultErr error) {
	oe.intentMu.Lock()
	defer oe.intentMu.Unlock()
	intent := oe.intents[req.ClientOrderID]
	if intent == nil {
		return err
	}
	defer func() {
		if journalErr := oe.saveJournalIntentLocked(intent); journalErr != nil {
			intent.unknown = true
			intent.rejected = false
			oe.intents[req.ClientOrderID] = intent
			oe.blockJournalFailureLocked()
			oe.markExposureUnknownLocked(req.ClientOrderID)
			resultErr = fmt.Errorf("persist order outcome: %w: %v", execution.ErrOrderUnknown, journalErr)
		}
	}()
	if err != nil && intent.order != nil && !errors.Is(err, execution.ErrOrderUnknown) {
		// An observed venue order contradicts a claimed deterministic refusal.
		// Never release its reservation on the basis of REST error text alone.
		err = fmt.Errorf("REST refusal conflicts with observed order: %w", execution.ErrOrderUnknown)
	}
	if placed != nil {
		merged, mergeErr := mergeIntentObservation(intent.order, placed)
		if mergeErr != nil {
			err = fmt.Errorf("conflicting order observation: %w: %v", execution.ErrOrderUnknown, mergeErr)
			placed = nil // Retain the last consistent observation below.
		} else {
			placed = merged
		}
	}
	if placed != nil {
		if exposureErr := oe.observeExposureLocked(req.ClientOrderID, placed); exposureErr != nil {
			err = fmt.Errorf("accepted order exposure requires reconciliation: %w: %v", execution.ErrOrderUnknown, exposureErr)
		}
	}
	if errors.Is(err, execution.ErrOrderUnknown) {
		intent.unknown = true
		oe.markExposureUnknownLocked(req.ClientOrderID)
		if placed != nil && (intent.order == nil || !terminalOrderStatus(intent.order.Status)) {
			copy := *placed
			intent.order = &copy
		}
		if oe.openingGate != nil {
			oe.openingGate.Block("unknown_orders")
		}
		return err
	}
	if err != nil {
		// A deterministic rejection or failure before submission owns no order.
		if oe.exposureBook != nil {
			if releaseErr := oe.exposureBook.Reject(req.ClientOrderID); releaseErr != nil {
				intent.unknown = true
				oe.markExposureUnknownLocked(req.ClientOrderID)
				return fmt.Errorf("refused order still owns exposure: %w", execution.ErrOrderUnknown)
			}
		}
		delete(oe.intents, req.ClientOrderID)
		intent.rejected = true
		return err
	}
	if placed != nil {
		if intent.order != nil && terminalOrderStatus(intent.order.Status) && !terminalOrderStatus(placed.Status) {
			return err // WS terminal report arrived before the REST acknowledgement.
		}
		copy := *placed
		intent.order = &copy
	}
	return err
}

func (oe *ExchangeOrderExecutor) snapshotOwnedIntents() []ownedIntent {
	oe.intentMu.Lock()
	defer oe.intentMu.Unlock()
	result := make([]ownedIntent, 0, len(oe.intents))
	for _, intent := range oe.intents {
		copy := *intent
		if intent.order != nil {
			ord := *intent.order
			copy.order = &ord
		}
		result = append(result, copy)
	}
	return result
}
