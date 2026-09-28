package order

import (
	"context"
	"encoding/json"
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
)

type persistedIntent struct {
	Version       int
	Scope         execution.IntentScope
	Request       OrderRequest
	Opening       bool
	Order         *Order
	Unknown       bool
	LedgerPending bool
	LedgerReason  string
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
	defer oe.intentMu.Unlock()
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
				continue
			}
			intent.unknown = true // Includes PREPARED and unaccounted terminal fills.
			if intent.ledgerPending {
				intent.unknown = true
			}
			oe.intents[r.ClientOrderID] = intent
		}
		if len(page) < intentJournalPageSize {
			break
		}
	}
	oe.journalLoaded = true
	if len(oe.intents) > 0 {
		return fmt.Errorf("persisted intents require economic reconciliation: %w", execution.ErrOrderUnknown)
	}
	oe.openingGate.Unblock(IntentRecoveryBlock)
	return nil
}

// A verified terminal order with no fills has no position, capital, or PnL
// effect. It can be omitted from recovery; every other accepted intent remains
// unresolved until the strategy and financial ledgers are reconciled.
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
	return &ownedIntent{request: p.Request, opening: p.Opening, order: p.Order, unknown: p.Unknown, ledgerPending: p.LedgerPending, ledgerReason: p.LedgerReason, rejected: p.Rejected, settled: p.Settled, revision: r.Revision, attempts: p.Attempts, attemptPrice: p.AttemptPrice}, nil
}

func (oe *ExchangeOrderExecutor) saveJournalIntentLocked(intent *ownedIntent) error {
	if !oe.journalRequired {
		return nil
	} // Isolated tests/replay can use memory only.
	if oe.intentJournal == nil || !oe.journalLoaded {
		return fmt.Errorf("intent journal unavailable")
	}
	data, err := json.Marshal(persistedIntent{Version: 1, Scope: oe.intentScope, Request: intent.request, Opening: intent.opening,
		Order: intent.order, Unknown: intent.unknown, LedgerPending: intent.ledgerPending, LedgerReason: intent.ledgerReason,
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
