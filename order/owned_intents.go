package order

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"quantmesh/execution"
	"quantmesh/utils"
)

// ownedIntent records economic intent before the physical call. Never infer
// ownership from BUY/SELL: another bot or a protective close can have that side.
type ownedIntent struct {
	request       OrderRequest
	opening       bool
	order         *Order
	unknown       bool
	ledgerPending bool
	ledgerReason  string
	ledgerPayload []byte
	revision      int64
	attempts      int
	attemptPrice  float64
	rejected      bool
	settled       bool
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
	oe.intentMu.Lock()
	intent := oe.intents[clientOrderID]
	if intent == nil || intent.order == nil || intent.unknown || intent.ledgerPending || intent.rejected {
		oe.intentMu.Unlock()
		return fmt.Errorf("execution intent %s is not eligible for settlement", clientOrderID)
	}
	orderID := intent.order.OrderID
	oe.intentMu.Unlock()
	if orderID <= 0 {
		return fmt.Errorf("execution intent %s has invalid venue order id", clientOrderID)
	}
	observed, err := oe.exchange.GetOrder(ctx, oe.symbol, orderID)
	if err != nil {
		return fmt.Errorf("verify terminal order %d before settlement: %w", orderID, err)
	}
	if observed == nil || observed.OrderID != orderID || observed.Symbol != oe.symbol ||
		(observed.ClientOrderID != "" && !oe.matchesOwnedClientOrderID(clientOrderID, observed.ClientOrderID)) || !terminalOrderStatus(string(observed.Status)) {
		return fmt.Errorf("execution intent %s has no matching terminal venue order", clientOrderID)
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
