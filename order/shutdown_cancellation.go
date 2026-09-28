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

// CancelOwnedShutdownOrders terminates both opening and protective orders, but
// never scans/cancels by account, symbol or side. Ordinary submissions must be
// sealed first so a new close cannot appear behind this snapshot. Newly found
// fills are retained as UNKNOWN until the economic consumers reconcile them.
func (oe *ExchangeOrderExecutor) CancelOwnedShutdownOrders(ctx context.Context) error {
	if err := oe.DrainShutdown(ctx); err != nil {
		return err
	}
	oe.intentMu.Lock()
	journalUnavailable := oe.journalRequired && (!oe.journalLoaded || oe.intentJournal == nil)
	oe.intentMu.Unlock()
	if journalUnavailable {
		return fmt.Errorf("owned order inventory is not durably loaded")
	}
	for !oe.cancellationMu.TryLock() {
		if err := waitForOrderRetry(ctx, 10*time.Millisecond); err != nil {
			return err
		}
	}
	defer oe.cancellationMu.Unlock()
	var problems []error
	for _, intent := range oe.snapshotOwnedIntents() {
		if err := ctx.Err(); err != nil {
			return errors.Join(append(problems, err)...)
		}
		if intent.rejected || (intent.order != nil && terminalOrderStatus(intent.order.Status) && !intent.unknown) {
			continue
		}
		if err := oe.cancelShutdownIntent(ctx, intent); err != nil {
			oe.retainShutdownUncertainty(intent.request.ClientOrderID)
			problems = append(problems, fmt.Errorf("owned shutdown order %s: %w", intent.request.ClientOrderID, err))
		}
	}
	return errors.Join(problems...)
}

func (oe *ExchangeOrderExecutor) queryShutdownIntent(ctx context.Context, intent ownedIntent) (*exchange.Order, error) {
	qctx, cancel := context.WithTimeout(ctx, orderLookupTimeout)
	defer cancel()
	if intent.order != nil && intent.order.OrderID > 0 {
		return oe.exchange.GetOrder(qctx, oe.symbol, intent.order.OrderID)
	}
	if querier, ok := oe.exchange.(exchange.OrderByClientIDQuerier); ok {
		return querier.GetOrderByClientOrderID(qctx, oe.symbol, intent.request.ClientOrderID)
	}
	return nil, fmt.Errorf("no confirmed identity or client-ID lookup: %w", execution.ErrOrderUnknown)
}

func (oe *ExchangeOrderExecutor) validateShutdownIntent(intent ownedIntent, state *exchange.Order) error {
	if state == nil || state.OrderID <= 0 ||
		(intent.order != nil && intent.order.OrderID > 0 && state.OrderID != intent.order.OrderID) ||
		(state.Symbol != "" && state.Symbol != oe.symbol) ||
		(state.Side != "" && string(state.Side) != intent.request.Side) {
		return fmt.Errorf("order identity not verified")
	}
	cid := intent.request.ClientOrderID
	if state.ClientOrderID != cid && state.ClientOrderID != utils.AddBrokerPrefix(strings.ToLower(oe.exchange.GetName()), cid) {
		if state.ClientOrderID != "" || intent.order == nil || intent.order.OrderID != state.OrderID {
			return fmt.Errorf("client order identity not verified")
		}
	}
	if math.IsNaN(state.ExecutedQty) || math.IsInf(state.ExecutedQty, 0) || state.ExecutedQty < 0 ||
		math.IsNaN(state.Quantity) || math.IsInf(state.Quantity, 0) || state.Quantity < 0 {
		return fmt.Errorf("invalid order quantity")
	}
	expected := intent.request.Quantity
	if intent.order != nil && intent.order.Quantity > 0 {
		expected = intent.order.Quantity
	}
	if state.Quantity > 0 && state.Quantity != expected || state.ExecutedQty > expected ||
		strings.EqualFold(string(state.Status), "FILLED") && state.ExecutedQty != expected {
		return fmt.Errorf("terminal or original quantity inconsistent")
	}
	if intent.order != nil && state.ExecutedQty < intent.order.ExecutedQty {
		return fmt.Errorf("cumulative execution regressed")
	}
	if state.ExecutedQty > 0 && (state.AvgPrice <= 0 || math.IsNaN(state.AvgPrice) || math.IsInf(state.AvgPrice, 0)) {
		return fmt.Errorf("executed order has no valid cumulative price")
	}
	return nil
}

func (oe *ExchangeOrderExecutor) cancelShutdownIntent(ctx context.Context, intent ownedIntent) error {
	state, err := oe.queryShutdownIntent(ctx, intent)
	if err != nil {
		return err
	}
	if err := oe.validateShutdownIntent(intent, state); err != nil {
		return err // Never cancel an unverified numeric ID.
	}
	if !terminalOrderStatus(string(state.Status)) {
		cctx, cancel := context.WithTimeout(ctx, orderLookupTimeout)
		cancelErr := oe.exchange.CancelOrder(cctx, oe.symbol, state.OrderID)
		cancel()
		// A late cancellation error can race a valid terminal fill. Query the
		// exact observed ID, including when the original request had no ACK.
		intent.order = &Order{OrderID: state.OrderID, Quantity: state.Quantity, ExecutedQty: state.ExecutedQty}
		state, err = oe.queryShutdownIntent(ctx, intent)
		if err != nil {
			return errors.Join(cancelErr, err)
		}
		if err := oe.validateShutdownIntent(intent, state); err != nil {
			return errors.Join(cancelErr, err)
		}
	}
	if !terminalOrderStatus(string(state.Status)) {
		return fmt.Errorf("cancellation acknowledged but order remains %s", state.Status)
	}
	// Compare with the current durable observation, not the pre-cancel query:
	// querying a fill does not mean the strategy/slot has accounted for it.
	oe.intentMu.Lock()
	known := oe.intents[intent.request.ClientOrderID]
	unsettled := known == nil || known.unknown
	if known != nil {
		priorFill := 0.0
		if known.order != nil {
			priorFill = known.order.ExecutedQty
		}
		unsettled = unsettled || state.ExecutedQty > priorFill
	}
	oe.intentMu.Unlock()
	if !oe.ObserveOrder(state) {
		return fmt.Errorf("terminal observation rejected")
	}
	oe.intentMu.Lock()
	journalFailed := oe.journalRequired && !oe.journalLoaded
	oe.intentMu.Unlock()
	if journalFailed || unsettled {
		return fmt.Errorf("terminal order requires durable fill reconciliation: %w", execution.ErrOrderUnknown)
	}
	return ctx.Err()
}

func (oe *ExchangeOrderExecutor) retainShutdownUncertainty(cid string) {
	oe.intentMu.Lock()
	defer oe.intentMu.Unlock()
	if intent := oe.intents[cid]; intent != nil {
		intent.unknown = true
		oe.markExposureUnknownLocked(cid)
		if err := oe.saveJournalIntentLocked(intent); err != nil {
			oe.blockJournalFailureLocked()
		}
	}
}
