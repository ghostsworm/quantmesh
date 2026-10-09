package order

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"quantmesh/execution"
	"quantmesh/utils"
)

const (
	IntentRecoveryBlock       = "execution_intent_recovery"
	IntentJournalFailureBlock = "execution_intent_journal_failed"
	intentJournalPageSize     = 500
	intentJournalTimeout      = 5 * time.Second
	maxTradeLedgerReplayBytes = 64 * 1024
)

type loadedIntentRecoveryRequiredError struct{}

func (*loadedIntentRecoveryRequiredError) Error() string {
	return "persisted intents require economic reconciliation: " + execution.ErrOrderUnknown.Error()
}
func (*loadedIntentRecoveryRequiredError) Unwrap() error { return execution.ErrOrderUnknown }

// Read-only recovery admission requires a completely loaded, owner-validated
// journal. Neither an arbitrary UNKNOWN error nor a partial load qualifies.
func (oe *ExchangeOrderExecutor) LoadedIntentRecoveryRequired(err error) bool {
	var pending *loadedIntentRecoveryRequiredError
	if !errors.As(err, &pending) {
		return false
	}
	oe.intentMu.Lock()
	defer oe.intentMu.Unlock()
	return oe.journalRequired && oe.journalLoaded && oe.intentJournal != nil && oe.intentScopeKey != "" && len(oe.intents) > 0 && oe.openingGate.HasBlock(IntentRecoveryBlock)
}

type persistedIntent struct {
	Version       int
	Scope         execution.IntentScope
	Request       OrderRequest
	Opening       bool
	Order         *Order
	Unknown       bool
	LedgerPending bool
	LedgerReason  string
	LedgerPayload json.RawMessage `json:"LedgerPayload,omitempty"`
	Rejected      bool
	Settled       bool
	Attempts      int
	AttemptPrice  float64
}

// ConfigureIntentJournal runs before Initialize/StartAll. Recovered orders are
// NOT economically settled merely because the venue status is terminal. Keep
// their identities and block opening until full strategy reconciliation exists.
func (oe *ExchangeOrderExecutor) ConfigureIntentJournal(ctx context.Context, journal execution.IntentJournal, scope execution.IntentScope) error {
	oe.intentMu.Lock()
	locked := true
	defer func() {
		if locked {
			oe.intentMu.Unlock()
		}
	}()
	oe.journalRequired = true
	oe.journalLoaded = false
	oe.openingGate.Block(IntentRecoveryBlock)
	if len(oe.intents) != 0 {
		return fmt.Errorf("cannot bind journal after submissions")
	}
	key, err := scope.Key()
	if err != nil {
		return err
	}
	if journal == nil || scope.Bot != oe.botID || scope.Symbol != oe.symbol || scope.Exchange != oe.exchange.GetName() || scope.Market != oe.exchange.GetMarketType() {
		return fmt.Errorf("intent journal owner mismatch or storage unavailable")
	}
	oe.intentJournal, oe.intentScope, oe.intentScopeKey = journal, scope, key
	oe.intents = make(map[string]*ownedIntent)
	var pendingTradeRecovery []*ownedIntent
	var pendingZeroFillRecovery []string
	var after int64
	for {
		page, err := journal.LoadExecutionIntents(ctx, key, after, intentJournalPageSize)
		if err != nil {
			return fmt.Errorf("load execution intents: %w", err)
		}
		if len(page) > intentJournalPageSize {
			return fmt.Errorf("oversized execution intent page")
		}
		for _, r := range page {
			if r.ID <= after || r.Revision <= 0 {
				return fmt.Errorf("invalid intent page cursor or revision")
			}
			after = r.ID
			intent, err := oe.decodeJournalIntent(r)
			if err != nil {
				return err
			}
			if _, duplicate := oe.intents[r.ClientOrderID]; duplicate {
				return fmt.Errorf("duplicate restored intent")
			}
			if intent.rejected {
				continue
			}
			if intent.settled {
				if intent.unknown || intent.ledgerPending || intent.order == nil || !terminalOrderStatus(intent.order.Status) {
					return fmt.Errorf("persisted execution intent %s has invalid settled state", intent.request.ClientOrderID)
				}
				continue
			}
			if isVerifiedZeroFillTerminalIntent(intent, oe.symbol) {
				// A previously stored websocket terminal status is not venue proof.
				// Keep it in the owned set and re-query the venue before clearing the
				// startup recovery gate.
				pendingZeroFillRecovery = append(pendingZeroFillRecovery, r.ClientOrderID)
				oe.intents[r.ClientOrderID] = intent
				continue
			}
			intent.unknown = true // Includes PREPARED and unaccounted terminal fills.
			if intent.ledgerPending {
				intent.unknown = true
				if len(intent.ledgerPayload) > 0 && intent.order != nil && terminalOrderStatus(intent.order.Status) {
					pendingTradeRecovery = append(pendingTradeRecovery, intent)
				}
			}
			oe.intents[r.ClientOrderID] = intent
		}
		if len(page) < intentJournalPageSize {
			break
		}
	}
	oe.journalLoaded = true
	for _, intent := range pendingTradeRecovery {
		if oe.tradeLedgerRecoveryHandler == nil {
			continue
		}
		if err := oe.tradeLedgerRecoveryHandler(ctx, scope, intent.order.OrderID, intent.order.ExecutedQty, append([]byte(nil), intent.ledgerPayload...)); err != nil {
			continue
		}
		intent.ledgerPending = false
		intent.ledgerReason = ""
		intent.ledgerPayload = nil
		intent.unknown = false
		intent.settled = true
		if err := oe.saveJournalIntentLocked(intent); err != nil {
			intent.settled = false
			intent.ledgerPending = true
			intent.unknown = true
			intent.ledgerReason = "trade ledger replay succeeded but intent settlement was not confirmed"
			oe.blockJournalFailureLocked()
			continue
		}
		delete(oe.intents, intent.request.ClientOrderID)
	}
	// Do not trust a stale zero-fill terminal observation across restart. Query
	// each exact owned order again before marking its journal record settled.
	locked = false
	oe.intentMu.Unlock()
	for _, cid := range pendingZeroFillRecovery {
		if err := oe.SettleZeroFillIntent(ctx, cid); err != nil {
			oe.intentMu.Lock()
			intent := oe.intents[cid]
			var orderID int64
			if intent != nil && intent.order != nil {
				orderID = intent.order.OrderID
			}
			oe.intentMu.Unlock()
			markErr := oe.MarkOrderReconciliationRequired(orderID, cid, err.Error())
			return fmt.Errorf("re-verify zero-fill intent %s: %w (cause: %v; persist hold: %v)", cid, execution.ErrOrderUnknown, err, markErr)
		}
		oe.intentMu.Lock()
		delete(oe.intents, cid)
		oe.intentMu.Unlock()
	}
	oe.intentMu.Lock()
	locked = true
	if len(oe.intents) > 0 {
		return &loadedIntentRecoveryRequiredError{}
	}
	oe.openingGate.Unblock(IntentRecoveryBlock)
	return nil
}

// isVerifiedZeroFillTerminalIntent identifies journal observations eligible
// for a fresh venue verification during startup; the stored status alone never
// authorizes the intent to be omitted from recovery.
func isVerifiedZeroFillTerminalIntent(intent *ownedIntent, symbol string) bool {
	if intent == nil || intent.unknown || intent.ledgerPending || intent.order == nil {
		return false
	}
	order := intent.order
	status := strings.ToUpper(order.Status)
	if status != "CANCELED" && status != "CANCELLED" && status != "EXPIRED" && status != "REJECTED" {
		return false
	}
	return order.OrderID > 0 && order.ClientOrderID == intent.request.ClientOrderID &&
		order.Symbol == symbol && order.Side == intent.request.Side &&
		order.Quantity == intent.request.Quantity && order.ExecutedQty == 0 &&
		!math.IsNaN(order.Quantity) && !math.IsInf(order.Quantity, 0) && order.Quantity > 0
}

func (oe *ExchangeOrderExecutor) decodeJournalIntent(r execution.IntentJournalRecord) (*ownedIntent, error) {
	var p persistedIntent
	if err := json.Unmarshal(r.Payload, &p); err != nil {
		return nil, fmt.Errorf("decode execution intent: %w", err)
	}
	if p.Version != 1 || p.Scope != oe.intentScope || p.Request.ClientOrderID != r.ClientOrderID || p.Request.Symbol != oe.symbol ||
		(p.Request.Side != "BUY" && p.Request.Side != "SELL") || p.Request.Quantity <= 0 || math.IsNaN(p.Request.Quantity) || math.IsInf(p.Request.Quantity, 0) ||
		p.Opening != oe.isOpeningOrder(&p.Request) || p.Attempts < 0 || (p.Rejected && (p.Unknown || p.Order != nil)) {
		return nil, fmt.Errorf("persisted intent identity or state invalid")
	}
	if _, _, err := validateOrderExecution(&p.Request); err != nil {
		return nil, err
	}
	if p.Request.BotWideClose {
		if _, err := oe.exposureRequest(&p.Request); err != nil || p.Request.ExposureKey != "" {
			return nil, fmt.Errorf("persisted bot-wide close allocation invalid")
		}
	}
	if len(p.LedgerPayload) > 0 && (!p.LedgerPending || p.Order == nil || !terminalOrderStatus(p.Order.Status) || len(p.LedgerPayload) > maxTradeLedgerReplayBytes) {
		return nil, fmt.Errorf("persisted trade ledger replay state invalid")
	}
	return &ownedIntent{request: p.Request, opening: p.Opening, order: p.Order, unknown: p.Unknown, ledgerPending: p.LedgerPending, ledgerReason: p.LedgerReason,
		ledgerPayload: append([]byte(nil), p.LedgerPayload...), rejected: p.Rejected, settled: p.Settled, revision: r.Revision, attempts: p.Attempts, attemptPrice: p.AttemptPrice}, nil
}

func (oe *ExchangeOrderExecutor) saveJournalIntentLocked(intent *ownedIntent) error {
	if !oe.journalRequired {
		return nil
	} // Isolated tests/replay can use memory only.
	if oe.intentJournal == nil || !oe.journalLoaded {
		return fmt.Errorf("intent journal unavailable")
	}
	data, err := json.Marshal(persistedIntent{Version: 1, Scope: oe.intentScope, Request: intent.request, Opening: intent.opening,
		Order: intent.order, Unknown: intent.unknown, LedgerPending: intent.ledgerPending, LedgerReason: intent.ledgerReason, LedgerPayload: intent.ledgerPayload,
		Rejected: intent.rejected, Settled: intent.settled, Attempts: intent.attempts, AttemptPrice: intent.attemptPrice})
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), intentJournalTimeout)
	defer cancel()
	if err := oe.intentJournal.SaveExecutionIntent(ctx, oe.intentScopeKey, intent.request.ClientOrderID, intent.revision, data); err != nil {
		return err
	}
	intent.revision++
	return nil
}

// MarkTradeLedgerReconciliationRequired records a known order whose venue
// outcome is settled but whose economic fill record is missing or invalid.
// Unlike an unknown-order hold, this does not mark the physical order unknown,
// so emergency close verification can continue while new openings stay blocked.
func (oe *ExchangeOrderExecutor) MarkTradeLedgerReconciliationRequired(orderID int64, clientOrderID, reason string) error {
	return oe.markTradeLedgerReconciliationRequired(orderID, clientOrderID, reason, nil)
}

// MarkTradeLedgerRecordReconciliationRequired stores the exact idempotent
// trade row alongside the hold for safe replay after a transient write error.
func (oe *ExchangeOrderExecutor) MarkTradeLedgerRecordReconciliationRequired(orderID int64, clientOrderID, reason string, payload []byte) error {
	if len(payload) == 0 || len(payload) > maxTradeLedgerReplayBytes || !json.Valid(payload) {
		return fmt.Errorf("trade ledger replay payload must be valid JSON")
	}
	return oe.markTradeLedgerReconciliationRequired(orderID, clientOrderID, reason, payload)
}

func (oe *ExchangeOrderExecutor) markTradeLedgerReconciliationRequired(orderID int64, clientOrderID, reason string, payload []byte) error {
	oe.intentMu.Lock()
	defer oe.intentMu.Unlock()
	for cid, intent := range oe.intents {
		if clientOrderID != "" && cid != clientOrderID && utils.AddBrokerPrefix(strings.ToLower(oe.exchange.GetName()), cid) != clientOrderID {
			continue
		}
		if orderID > 0 && (intent.order == nil || intent.order.OrderID != orderID) {
			continue
		}
		intent.ledgerPending = true
		intent.ledgerReason = reason
		if len(payload) > 0 {
			intent.ledgerPayload = append([]byte(nil), payload...)
		}
		if err := oe.saveJournalIntentLocked(intent); err != nil {
			oe.blockJournalFailureLocked()
			return fmt.Errorf("persist trade-ledger reconciliation hold (%s): %w", reason, err)
		}
		oe.openingGate.Block("trade_ledger_unverified")
		return nil
	}
	oe.openingGate.Block("trade_ledger_unverified")
	return fmt.Errorf("owned execution intent not found for order %d client id %q (%s)", orderID, clientOrderID, reason)
}

// MarkOrderReconciliationRequired keeps a known owned execution unresolved when
// a downstream financial ledger cannot durably record its fill.
func (oe *ExchangeOrderExecutor) MarkOrderReconciliationRequired(orderID int64, clientOrderID, reason string) error {
	oe.intentMu.Lock()
	defer oe.intentMu.Unlock()
	for cid, intent := range oe.intents {
		if clientOrderID != "" && cid != clientOrderID && utils.AddBrokerPrefix(strings.ToLower(oe.exchange.GetName()), cid) != clientOrderID {
			continue
		}
		if orderID > 0 && (intent.order == nil || intent.order.OrderID != orderID) {
			continue
		}
		intent.unknown = true
		oe.markExposureUnknownLocked(cid)
		if err := oe.saveJournalIntentLocked(intent); err != nil {
			oe.blockJournalFailureLocked()
			return fmt.Errorf("persist reconciliation hold (%s): %w", reason, err)
		}
		oe.openingGate.Block("unknown_orders")
		return nil
	}
	oe.openingGate.Block("unknown_orders")
	return fmt.Errorf("owned execution intent not found for order %d client id %q (%s)", orderID, clientOrderID, reason)
}

func (oe *ExchangeOrderExecutor) blockJournalFailureLocked() {
	// An uncertain write must be reloaded, never retried under a stale revision.
	oe.journalLoaded = false
	oe.openingGate.Block(IntentJournalFailureBlock)
}

func (oe *ExchangeOrderExecutor) prepareJournalSubmission(cid string, price float64) error {
	oe.intentMu.Lock()
	defer oe.intentMu.Unlock()
	intent := oe.intents[cid]
	if intent == nil {
		return fmt.Errorf("submission has no owned intent")
	}
	intent.attempts++
	intent.attemptPrice = price
	if err := oe.saveJournalIntentLocked(intent); err != nil {
		intent.unknown = true
		oe.blockJournalFailureLocked()
		return fmt.Errorf("journal submission attempt: %w: %v", execution.ErrOrderUnknown, err)
	}
	return nil
}

type RecoveredOrderRoute struct {
	OrderID       int64
	ClientOrderID string
	StrategyName  string
}

// RecoveredOrderRoutes never falls back to account-wide legacy order scans.
func (oe *ExchangeOrderExecutor) RecoveredOrderRoutes() []RecoveredOrderRoute {
	oe.intentMu.Lock()
	defer oe.intentMu.Unlock()
	if !oe.journalLoaded {
		return nil
	}
	var routes []RecoveredOrderRoute
	for cid, intent := range oe.intents {
		if intent.request.StrategyName == "" {
			continue
		}
		route := RecoveredOrderRoute{ClientOrderID: cid, StrategyName: intent.request.StrategyName}
		if intent.order != nil {
			route.OrderID = intent.order.OrderID
		}
		routes = append(routes, route)
	}
	return routes
}
