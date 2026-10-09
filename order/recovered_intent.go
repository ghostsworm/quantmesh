package order

import (
	"context"
	"fmt"
	"math"
	"strings"

	"quantmesh/exchange"
)

// SettleRecoveredIntent is reserved for a strategy startup reconciler after it
// has durably applied the exact terminal order fills to its own economic
// ledger. It records that exact terminal venue state but deliberately leaves
// IntentRecoveryBlock in place; runtime bootstrap must reload all owner ledgers
// and seed the shared ExposureBook before opening can resume.
func (oe *ExchangeOrderExecutor) SettleRecoveredIntent(ctx context.Context, clientOrderID, strategyName string) error {
	if ctx == nil || strings.TrimSpace(clientOrderID) == "" || strings.TrimSpace(strategyName) == "" {
		return fmt.Errorf("recovered intent settlement requires context, client order ID, and strategy owner")
	}
	if oe == nil || oe.exchange == nil || oe.openingGate == nil {
		return fmt.Errorf("recovered intent settlement executor is unavailable")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	oe.intentMu.Lock()
	if !oe.journalRequired || !oe.journalLoaded || oe.intentJournal == nil || !oe.openingGate.HasBlock(IntentRecoveryBlock) ||
		oe.exposureBook == nil || oe.exposureBook.Initialized() {
		oe.intentMu.Unlock()
		return fmt.Errorf("recovered intent settlement is only allowed before exposure bootstrap completes")
	}
	canonicalID := ""
	var intent *ownedIntent
	for cid, candidate := range oe.intents {
		if oe.matchesOwnedClientOrderID(cid, clientOrderID) {
			canonicalID, intent = cid, candidate
			break
		}
	}
	if intent == nil || !intent.unknown || intent.ledgerPending || intent.rejected || intent.settled || intent.request.StrategyName != strategyName {
		oe.intentMu.Unlock()
		return fmt.Errorf("recovered intent is not unresolved under the requested strategy owner")
	}
	request := intent.request
	prior := intent.order
	revision := intent.revision
	oe.intentMu.Unlock()

	var observed *exchange.Order
	var err error
	if prior != nil && prior.OrderID > 0 {
		observed, err = oe.exchange.GetOrder(ctx, oe.symbol, prior.OrderID)
	} else if lookup, ok := oe.exchange.(exchange.OrderByClientIDQuerier); ok {
		observed, err = lookup.GetOrderByClientOrderID(ctx, oe.symbol, canonicalID)
	} else {
		return fmt.Errorf("exchange cannot query recovered intent by its exact durable identity")
	}
	if err != nil {
		return fmt.Errorf("query recovered strategy order %s: %w", canonicalID, err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if !validOrderObservationNumbers(observed) || observed.OrderID <= 0 || observed.Symbol != request.Symbol ||
		!strings.EqualFold(string(observed.Side), request.Side) || (observed.ClientOrderID != "" && !oe.matchesOwnedClientOrderID(canonicalID, observed.ClientOrderID)) ||
		!terminalOrderStatus(string(observed.Status)) || observed.Quantity <= 0 || observed.ExecutedQty > observed.Quantity ||
		observed.Quantity > request.Quantity+recoveredIntentQuantityTolerance(request.Quantity) ||
		observed.ExecutedQty > request.Quantity+recoveredIntentQuantityTolerance(request.Quantity) ||
		(prior != nil && prior.OrderID > 0 && prior.OrderID != observed.OrderID) {
		return fmt.Errorf("recovered strategy order %s lacks exact terminal identity or quantity evidence", canonicalID)
	}
	next := &Order{OrderID: observed.OrderID, ClientOrderID: canonicalID, Symbol: request.Symbol, Side: request.Side,
		Price: observed.Price, Quantity: observed.Quantity, Status: string(observed.Status),
		ExecutedQty: observed.ExecutedQty, AvgPrice: observed.AvgPrice}
	merged, err := mergeIntentObservation(prior, next)
	if err != nil {
		return fmt.Errorf("recovered strategy order %s conflicts with durable execution progress: %w", canonicalID, err)
	}
	if merged == nil || !terminalOrderStatus(merged.Status) {
		return fmt.Errorf("recovered strategy order %s did not produce a terminal merged state", canonicalID)
	}

	oe.intentMu.Lock()
	defer oe.intentMu.Unlock()
	current := oe.intents[canonicalID]
	if current != intent || current.revision != revision || !current.unknown || current.ledgerPending || current.rejected ||
		current.request.StrategyName != strategyName || !oe.openingGate.HasBlock(IntentRecoveryBlock) || oe.exposureBook == nil || oe.exposureBook.Initialized() {
		return fmt.Errorf("recovered strategy intent changed before conditional settlement")
	}
	current.order = merged
	current.unknown = false
	current.settled = true
	if err := oe.saveJournalIntentLocked(current); err != nil {
		current.settled = false
		current.unknown = true
		oe.blockJournalFailureLocked()
		return fmt.Errorf("persist recovered strategy intent settlement: %w", err)
	}
	delete(oe.intents, canonicalID)
	return nil
}

func recoveredIntentQuantityTolerance(quantity float64) float64 {
	return math.Max(1e-8, math.Abs(quantity)*1e-8)
}
