package order

import (
	"bytes"
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

func (oe *ExchangeOrderExecutor) verifyPersistedSettledIntent(ctx context.Context, clientOrderID, strategyName string) error {
	oe.intentMu.Lock()
	if !oe.journalRequired || !oe.journalLoaded || oe.intentJournal == nil || oe.intentScopeKey == "" ||
		oe.intentScope.Bot != oe.botID || oe.intentScope.Symbol != oe.symbol {
		oe.intentMu.Unlock()
		return fmt.Errorf("settled intent journal is unavailable or has a mismatched owner scope")
	}
	journal, scopeKey := oe.intentJournal, oe.intentScopeKey
	oe.intentMu.Unlock()
	var after int64
	for {
		page, err := journal.LoadExecutionIntents(ctx, scopeKey, after, intentJournalPageSize)
		if err != nil {
			return fmt.Errorf("verify persisted settlement for %s: %w", clientOrderID, err)
		}
		if len(page) > intentJournalPageSize {
			return fmt.Errorf("execution intent journal returned an oversized page")
		}
		for _, record := range page {
			if record.ID <= after || record.Revision <= 0 {
				return fmt.Errorf("execution intent journal returned an invalid cursor")
			}
			after = record.ID
			if !oe.matchesOwnedClientOrderID(record.ClientOrderID, clientOrderID) {
				continue
			}
			intent, decodeErr := oe.decodeJournalIntent(record)
			if decodeErr != nil {
				return decodeErr
			}
			if intent.request.StrategyName != strategyName || !intent.settled || intent.unknown || intent.ledgerPending || intent.order == nil || !terminalOrderStatus(intent.order.Status) {
				return fmt.Errorf("persisted intent %s is not settled for strategy %s", clientOrderID, strategyName)
			}
			return nil
		}
		if len(page) < intentJournalPageSize {
			return fmt.Errorf("no persisted settled intent found for %s", clientOrderID)
		}
	}
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
	// A fresh executor must fail closed as soon as journal configuration is
	// attempted, including nil/unavailable/mismatched bindings. Do not alter an
	// already-established binding on a rejected reconfiguration attempt.
	if oe.intentJournal == nil && oe.intentScopeKey == "" {
		oe.journalRequired = true
		oe.journalLoaded = false
		oe.openingGate.Block(IntentRecoveryBlock)
	}
	key, err := scope.Key()
	if err != nil {
		return err
	}
	if journal == nil || scope.Bot != oe.botID || scope.Symbol != oe.symbol || scope.Exchange != oe.exchange.GetName() || scope.Market != oe.exchange.GetMarketType() {
		return fmt.Errorf("intent journal owner mismatch or storage unavailable")
	}
	identity, err := execution.StableIntentJournalIdentity(journal)
	if err != nil {
		return fmt.Errorf("identify intent journal backend: %w", err)
	}
	if oe.intentJournal != nil || oe.intentScopeKey != "" {
		boundIdentity, identityErr := execution.StableIntentJournalIdentity(oe.intentJournal)
		if identityErr != nil || identity != boundIdentity || key != oe.intentScopeKey || scope != oe.intentScope {
			return fmt.Errorf("intent journal is already bound to a different backend or owner scope")
		}
		// Refresh the interface value when a same-backend decorator is supplied.
		// Its stable identity proves it still delegates to the bound durable store;
		// this also preserves observability/instrumentation wrappers on retries.
		oe.intentJournal = journal
		if oe.journalLoaded {
			if hasUnresolvedIntents(oe.intents) {
				oe.openingGate.Block(IntentRecoveryBlock)
				return &loadedIntentRecoveryRequiredError{}
			}
			oe.openingGate.Unblock(IntentRecoveryBlock)
			return nil
		}
		if hasUncertainIntentWrites(oe.intents) {
			if err := oe.reconcileUncertainIntentWritesLocked(ctx); err != nil {
				oe.openingGate.Block(IntentRecoveryBlock)
				oe.openingGate.Block(IntentJournalFailureBlock)
				return fmt.Errorf("reconcile uncertain intent journal writes: %w", err)
			}
			oe.journalLoaded = true
			oe.openingGate.Unblock(IntentJournalFailureBlock)
			if hasUnresolvedIntents(oe.intents) {
				oe.openingGate.Block(IntentRecoveryBlock)
				return &loadedIntentRecoveryRequiredError{}
			}
			oe.openingGate.Unblock(IntentRecoveryBlock)
			return nil
		}
		if len(oe.intents) != 0 {
			oe.openingGate.Block(IntentRecoveryBlock)
			return fmt.Errorf("intent journal is unloaded with owned intents requiring reconciliation")
		}
	} else if len(oe.intents) != 0 {
		return fmt.Errorf("cannot bind journal after submissions")
	}
	oe.journalRequired = true
	oe.journalLoaded = false
	oe.openingGate.Block(IntentRecoveryBlock)
	oe.intentJournal, oe.intentScope, oe.intentScopeKey = journal, scope, key
	loadedIntents := make(map[string]*ownedIntent)
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
			intent.durablePayload = append([]byte(nil), r.Payload...)
			if _, duplicate := loadedIntents[r.ClientOrderID]; duplicate {
				return fmt.Errorf("duplicate restored intent")
			}
			if intent.rejected {
				continue
			}
			if intent.settled {
				if intent.unknown || intent.ledgerPending || intent.order == nil || !terminalOrderStatus(intent.order.Status) {
					return fmt.Errorf("persisted execution intent %s has invalid settled state", intent.request.ClientOrderID)
				}
				// Keep settled owner identity loaded so duplicate terminal callbacks
				// remain attributable and idempotent after restart.
				loadedIntents[r.ClientOrderID] = intent
				continue
			}
			if isVerifiedZeroFillTerminalIntent(intent, oe.symbol) {
				// A previously stored websocket terminal status is not venue proof.
				// Keep it in the owned set and re-query the venue before clearing the
				// startup recovery gate.
				pendingZeroFillRecovery = append(pendingZeroFillRecovery, r.ClientOrderID)
				loadedIntents[r.ClientOrderID] = intent
				continue
			}
			intent.unknown = true // Includes PREPARED and unaccounted terminal fills.
			if intent.ledgerPending {
				intent.unknown = true
				if len(intent.ledgerPayload) > 0 && intent.order != nil && terminalOrderStatus(intent.order.Status) {
					pendingTradeRecovery = append(pendingTradeRecovery, intent)
				}
			}
			loadedIntents[r.ClientOrderID] = intent
		}
		if len(page) < intentJournalPageSize {
			break
		}
	}
	oe.intents = loadedIntents
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
	}
	oe.intentMu.Lock()
	locked = true
	if hasUnresolvedIntents(oe.intents) {
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
	expected := intent.revision
	if err := oe.intentJournal.SaveExecutionIntent(ctx, oe.intentScopeKey, intent.request.ClientOrderID, expected, data); err != nil {
		record, found, readErr := readIntentJournalRecord(ctx, oe.intentJournal, oe.intentScopeKey, intent.request.ClientOrderID)
		if readErr == nil && found && record.Revision == expected+1 && bytes.Equal(record.Payload, data) {
			intent.revision = record.Revision
			intent.durablePayload = append(intent.durablePayload[:0], data...)
			intent.uncertainPayload = nil
			intent.uncertainRevision = 0
			return nil
		}
		baseMatches := expected == 0 && !found
		if expected > 0 && readErr == nil && found && record.Revision == expected && bytes.Equal(record.Payload, intent.durablePayload) {
			baseMatches = true
		}
		if baseMatches {
			retryErr := oe.intentJournal.SaveExecutionIntent(ctx, oe.intentScopeKey, intent.request.ClientOrderID, expected, data)
			if retryErr == nil {
				intent.revision = expected + 1
				intent.durablePayload = append(intent.durablePayload[:0], data...)
				intent.uncertainPayload = nil
				intent.uncertainRevision = 0
				return nil
			}
			record, found, readErr = readIntentJournalRecord(ctx, oe.intentJournal, oe.intentScopeKey, intent.request.ClientOrderID)
			if readErr == nil && found && record.Revision == expected+1 && bytes.Equal(record.Payload, data) {
				intent.revision = record.Revision
				intent.durablePayload = append(intent.durablePayload[:0], data...)
				intent.uncertainPayload = nil
				intent.uncertainRevision = 0
				return nil
			}
			err = errors.Join(err, retryErr)
		}
		intent.uncertainPayload = append(intent.uncertainPayload[:0], data...)
		intent.uncertainRevision = expected
		return fmt.Errorf("intent journal CAS outcome is uncertain: %w (readback: %v)", err, readErr)
	}
	intent.revision++
	intent.durablePayload = append(intent.durablePayload[:0], data...)
	intent.uncertainPayload = nil
	intent.uncertainRevision = 0
	return nil
}

func readIntentJournalRecord(ctx context.Context, journal execution.IntentJournal, scopeKey, clientOrderID string) (execution.IntentJournalRecord, bool, error) {
	if reader, ok := journal.(execution.IntentJournalRecordReader); ok {
		return reader.LoadExecutionIntent(ctx, scopeKey, clientOrderID)
	}
	var after int64
	for {
		page, err := journal.LoadExecutionIntents(ctx, scopeKey, after, intentJournalPageSize)
		if err != nil {
			return execution.IntentJournalRecord{}, false, err
		}
		if len(page) > intentJournalPageSize {
			return execution.IntentJournalRecord{}, false, fmt.Errorf("oversized intent journal readback page")
		}
		for _, record := range page {
			if record.ID <= after || record.Revision <= 0 {
				return execution.IntentJournalRecord{}, false, fmt.Errorf("invalid intent journal readback cursor")
			}
			after = record.ID
			if record.ClientOrderID == clientOrderID {
				return record, true, nil
			}
		}
		if len(page) < intentJournalPageSize {
			return execution.IntentJournalRecord{}, false, nil
		}
	}
}

func hasUnresolvedIntents(intents map[string]*ownedIntent) bool {
	for _, intent := range intents {
		if intent == nil || (!intent.settled && !intent.rejected) || intent.unknown || intent.ledgerPending {
			return true
		}
	}
	return false
}

func hasUncertainIntentWrites(intents map[string]*ownedIntent) bool {
	for _, intent := range intents {
		if intent != nil && len(intent.uncertainPayload) != 0 {
			return true
		}
	}
	return false
}

func (oe *ExchangeOrderExecutor) reconcileUncertainIntentWritesLocked(ctx context.Context) error {
	for cid, intent := range oe.intents {
		if intent == nil || len(intent.uncertainPayload) == 0 {
			continue
		}
		expected := intent.uncertainRevision
		payload := append([]byte(nil), intent.uncertainPayload...)
		record, found, err := readIntentJournalRecord(ctx, oe.intentJournal, oe.intentScopeKey, cid)
		if err != nil {
			return fmt.Errorf("read back uncertain intent %s: %w", cid, err)
		}
		if found && record.Revision == expected+1 && bytes.Equal(record.Payload, payload) {
			if err := oe.applyPersistedIntentLocked(cid, record); err != nil {
				return err
			}
			continue
		}
		baseMatches := expected == 0 && !found
		if expected > 0 && found && record.Revision == expected && bytes.Equal(record.Payload, intent.durablePayload) {
			baseMatches = true
		}
		if !baseMatches {
			return fmt.Errorf("uncertain intent %s conflicts with durable revision", cid)
		}
		if err := oe.intentJournal.SaveExecutionIntent(ctx, oe.intentScopeKey, cid, expected, payload); err != nil {
			record, found, readErr := readIntentJournalRecord(ctx, oe.intentJournal, oe.intentScopeKey, cid)
			if readErr != nil || !found || record.Revision != expected+1 || !bytes.Equal(record.Payload, payload) {
				return errors.Join(fmt.Errorf("retry uncertain intent %s CAS: %w", cid, err), readErr)
			}
		}
		record, found, err = readIntentJournalRecord(ctx, oe.intentJournal, oe.intentScopeKey, cid)
		if err != nil || !found || record.Revision != expected+1 || !bytes.Equal(record.Payload, payload) {
			return errors.Join(fmt.Errorf("confirm reconciled intent %s CAS", cid), err)
		}
		if err := oe.applyPersistedIntentLocked(cid, record); err != nil {
			return err
		}
	}
	return nil
}

func (oe *ExchangeOrderExecutor) applyPersistedIntentLocked(cid string, record execution.IntentJournalRecord) error {
	current := oe.intents[cid]
	if current == nil || record.ClientOrderID != cid {
		return fmt.Errorf("uncertain intent owner changed during journal reconciliation")
	}
	updated, err := oe.decodeJournalIntent(record)
	if err != nil {
		return err
	}
	updated.durablePayload = append([]byte(nil), record.Payload...)
	*current = *updated
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
		if intent == nil || intent.settled || intent.rejected || intent.request.StrategyName == "" {
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
