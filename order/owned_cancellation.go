package order

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"quantmesh/exchange"
	"quantmesh/execution"
	"quantmesh/logger"
	"quantmesh/utils"
)

func terminalOrderStatus(status string) bool {
	switch strings.ToUpper(status) {
	case "FILLED", "CANCELED", "CANCELLED", "EXPIRED", "REJECTED":
		return true
	default:
		return false
	}
}

// CancelOwnedOpeningOrders drains admitted calls, then cancels only economic
// OPEN intents this executor actually submitted. No account-wide side sweep.
// A cancel ACK or absence from open orders is not terminal-state evidence.
func (oe *ExchangeOrderExecutor) CancelOwnedOpeningOrders(ctx context.Context) error {
	for !oe.cancellationMu.TryLock() {
		if err := waitForOrderRetry(ctx, 10*time.Millisecond); err != nil {
			return err
		}
	}
	defer oe.cancellationMu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if oe.openingGate == nil || !oe.openingGate.HoldIfBlocked(execution.UnverifiedCancellationBlock) {
		return fmt.Errorf("opening gate must remain blocked while cancelling")
	}
	if err := oe.openingGate.Drain(ctx); err != nil {
		return fmt.Errorf("opening calls have not drained: %w", err)
	}
	if !oe.openingGate.Blocked() {
		return fmt.Errorf("opening resumed before residual cancellation")
	}
	intents := oe.snapshotOwnedIntents()
	open, err := oe.exchange.GetOpenOrders(ctx, oe.symbol)
	if err != nil {
		return fmt.Errorf("query owned opening orders: %w", err)
	}
	var problems []error
	for _, intent := range intents {
		if !intent.opening || intent.request.Symbol != oe.symbol {
			continue
		}
		if intent.order != nil && terminalOrderStatus(intent.order.Status) && !intent.unknown {
			continue
		}
		if !oe.openingGate.Blocked() {
			return errors.Join(append(problems, fmt.Errorf("opening resumed during cancellation"))...)
		}
		id := int64(0)
		if intent.order != nil {
			id = intent.order.OrderID
		}
		prefixed := utils.AddBrokerPrefix(strings.ToLower(oe.exchange.GetName()), intent.request.ClientOrderID)
		for _, live := range open {
			if live == nil || (live.Symbol != "" && live.Symbol != oe.symbol) {
				continue
			}
			if live.ClientOrderID == intent.request.ClientOrderID || live.ClientOrderID == prefixed || (id > 0 && live.OrderID == id) {
				id = live.OrderID
				if err := oe.exchange.CancelOrder(ctx, oe.symbol, id); err != nil {
					problems = append(problems, fmt.Errorf("cancel owned order %d: %w", id, err))
				}
				break
			}
		}
		if id <= 0 {
			problems = append(problems, fmt.Errorf("intent %s has no confirmed order identity: %w", intent.request.ClientOrderID, execution.ErrOrderUnknown))
			continue
		}
		state, err := oe.exchange.GetOrder(ctx, oe.symbol, id)
		if err != nil || state == nil || state.OrderID != id ||
			(state.Symbol != "" && state.Symbol != oe.symbol) ||
			(state.ClientOrderID != "" && state.ClientOrderID != intent.request.ClientOrderID && state.ClientOrderID != prefixed) ||
			!terminalOrderStatus(string(state.Status)) {
			problems = append(problems, fmt.Errorf("owned order %d termination not verified: %v", id, err))
			continue
		}
		if !oe.ObserveOrder(state) {
			problems = append(problems, fmt.Errorf("owned order %d terminal evidence could not be applied", id))
			continue
		}
		if intent.unknown {
			// Terminal status does not prove the strategy accounted for its fills.
			problems = append(problems, fmt.Errorf("owned intent %s terminated but awaits fill reconciliation: %w", intent.request.ClientOrderID, execution.ErrOrderUnknown))
		}
	}
	if len(problems) > 0 {
		oe.reconcileExposureLimitBlock()
		return errors.Join(problems...)
	}
	oe.openingGate.Unblock(execution.UnverifiedCancellationBlock)
	oe.reconcileExposureLimitBlock()
	return nil
}

// ObserveOrder records venue identity/status only. It does not silently clear an
// UNKNOWN block: fill accounting and durable strategy recovery must confirm it.
func (oe *ExchangeOrderExecutor) ObserveOrder(update *exchange.Order) bool {
	if update == nil || update.OrderID <= 0 || (update.Symbol != "" && update.Symbol != oe.symbol) {
		return false
	}
	oe.intentMu.Lock()
	defer oe.intentMu.Unlock()
	for cid, intent := range oe.intents {
		matches := cid == update.ClientOrderID || utils.AddBrokerPrefix(strings.ToLower(oe.exchange.GetName()), cid) == update.ClientOrderID
		if update.ClientOrderID == "" {
			// Some venue updates omit CID. Only an already observed, owned
			// numeric identity can substitute; never guess by symbol or side.
			matches = intent.order != nil && intent.order.OrderID == update.OrderID
		}
		if !matches {
			continue
		}
		if !validOrderObservationNumbers(update) {
			intent.unknown = true
			oe.markExposureUnknownLocked(cid)
			if err := oe.saveJournalIntentLocked(intent); err != nil {
				oe.blockJournalFailureLocked()
				logger.ErrorCtx(oe.logCtx(), "persist malformed order observation block failed: %v", err)
			}
			return false
		}
		if (intent.order != nil && intent.order.OrderID > 0 && intent.order.OrderID != update.OrderID) ||
			(update.Side != "" && string(update.Side) != intent.request.Side) {
			intent.unknown = true
			oe.markExposureUnknownLocked(cid)
			if err := oe.saveJournalIntentLocked(intent); err != nil {
				oe.blockJournalFailureLocked()
				logger.ErrorCtx(oe.logCtx(), "persist order identity conflict failed: %v", err)
			}
			return false
		}
		observed := &Order{OrderID: update.OrderID, ClientOrderID: cid, Symbol: intent.request.Symbol,
			Side: intent.request.Side, Price: update.Price, Quantity: update.Quantity, Status: string(update.Status),
			ExecutedQty: update.ExecutedQty, AvgPrice: update.AvgPrice}
		merged, mergeErr := mergeIntentObservation(intent.order, observed)
		if mergeErr != nil {
			intent.unknown = true
			oe.markExposureUnknownLocked(cid)
			if err := oe.saveJournalIntentLocked(intent); err != nil {
				oe.blockJournalFailureLocked()
				logger.ErrorCtx(oe.logCtx(), "persist conflicting cumulative fill failed: %v", err)
			}
			return false
		}
		observed = merged
		if err := oe.observeExposureLocked(cid, observed); err != nil {
			intent.unknown = true
			oe.markExposureUnknownLocked(cid)
		}
		if intent.order != nil && terminalOrderStatus(intent.order.Status) && !terminalOrderStatus(string(update.Status)) {
			return true
		}
		intent.order = observed
		if err := oe.saveJournalIntentLocked(intent); err != nil {
			intent.unknown = true
			oe.blockJournalFailureLocked()
			oe.markExposureUnknownLocked(cid)
			logger.ErrorCtx(oe.logCtx(), "persist observed order failed; execution blocked: %v", err)
			return false
		}
		oe.reconcileExposureLimitBlock()
		return true
	}
	return false
}
