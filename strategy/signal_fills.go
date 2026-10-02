package strategy

import (
	"math"

	"quantmesh/logger"
	"quantmesh/position"
)

// applySignalOrderUpdate is called with the strategy mutex held. Executions are
// accounted for before terminal status cleanup, including cancel-with-fill.
func applySignalOrderUpdate(active **Order, action *string, holding **Position, entry *float64, stats *StrategyStatistics,
	exchange position.IExchange, executor position.OrderExecutorInterface, update *position.OrderUpdate) {
	order := *active
	if !signalOrderMatches(order, update) {
		return
	}
	if order.OrderID == 0 && update.OrderID > 0 {
		order.OrderID = update.OrderID
	}
	quantity := update.ExecutedQty
	if order.Quantity <= 0 || math.IsNaN(order.Quantity) || math.IsInf(order.Quantity, 0) || math.IsNaN(quantity) || math.IsInf(quantity, 0) || quantity < 0 || quantity > order.Quantity+entryQtyEpsilon {
		retainSignalOrderForReconciliation(order, executor, update, "signal order cumulative fill exceeds submitted quantity")
		return
	}
	if signalOrderStatusFilled(update.Status) && quantity <= 0 {
		// FILLED without a cumulative quantity is not evidence that the requested
		// amount executed. Retain the order/action for explicit reconciliation.
		order.Status = position.OrderStatusUnknown
		return
	}
	if quantity < order.FillProgress.Quantity {
		if signalOrderStatusTerminal(update.Status) {
			retainSignalOrderForReconciliation(order, executor, update, "terminal signal order fill regressed")
		}
		return // stale non-terminal acknowledgements must not roll status backward
	}
	if quantity > order.FillProgress.Quantity && update.AvgPrice <= 0 {
		order.Status = position.OrderStatusUnknown
		return
	}
	nextProgress := order.FillProgress
	delta, price := nextProgress.Advance(quantity, update.AvgPrice, 0)
	if math.IsNaN(quantity) || math.IsInf(quantity, 0) || quantity < 0 || (quantity > order.FillProgress.Quantity && delta == 0) {
		// An invalid new fill cannot make its ownership disappear just because
		// the same report claims to be terminal. Keep it for reconciliation.
		order.Status = position.OrderStatusUnknown
		return
	}
	if delta > 0 && *action == signalActionCloseLong && (*holding == nil || delta > (*holding).Size+entryQtyEpsilon) {
		retainSignalOrderForReconciliation(order, executor, update, "signal close fill exceeds locally owned inventory")
		return
	}
	if delta > 0 {
		if !update.CommissionKnown {
			retainSignalOrderForReconciliation(order, executor, update, "signal order fill fee evidence is not authoritative")
			return
		}
		fee, feeKnown := commissionInQuote(exchange, update.Commission, update.CommissionAsset, price)
		if !feeKnown {
			retainSignalOrderForReconciliation(order, executor, update, "signal order fee cannot be valued in quote asset")
			return
		}
		order.FillProgress = nextProgress
		order.FeeVerifiedQty += delta
		order.FeeProgress += fee
		applySignalFill(order, *action, holding, entry, stats, delta, price, fee)
	}
	if signalOrderStatusFilled(update.Status) && quantity < order.Quantity-entryQtyEpsilon {
		// Account the verified cumulative fill above, but do not let an
		// inconsistent FILLED label erase the strategy's owned order identity.
		// A later authoritative query may confirm the full quantity. Conflicting
		// venue terminal evidence remains held by the durable executor journal.
		retainSignalOrderForReconciliation(order, executor, update, "filled signal order quantity is below submitted quantity")
		return
	}
	order.Status = update.Status
	if signalOrderStatusFilled(update.Status) || signalOrderStatusTerminal(update.Status) {
		if *action == signalActionCloseLong && order.FillProgress.Quantity > 0 {
			stats.TotalTrades++
		}
		*active, *action = nil, ""
	}
}

func retainSignalOrderForReconciliation(order *Order, executor position.OrderExecutorInterface, update *position.OrderUpdate, reason string) {
	order.Status = position.OrderStatusUnknown
	if tracker, ok := executor.(interface {
		MarkOrderReconciliationRequired(int64, string, string) error
	}); ok {
		if err := tracker.MarkOrderReconciliationRequired(update.OrderID, update.ClientOrderID, reason); err != nil {
			logger.Error("[%s] %s; durable reconciliation lock failed: %v", order.Symbol, reason, err)
		}
	} else if executor != nil {
		logger.Error("[%s] %s; executor lacks durable reconciliation lock", order.Symbol, reason)
	}
}

func applySignalFill(order *Order, action string, holding **Position, entry *float64, stats *StrategyStatistics, quantity, price, fee float64) {
	switch action {
	case signalActionOpenLong:
		if *holding == nil {
			*holding = &Position{Symbol: order.Symbol, EntryOrderID: order.OrderID, EntryClientOrderID: order.ClientOrderID}
		}
		p := *holding
		cost := p.Size*p.EntryPrice + quantity*price
		p.Size += quantity
		p.EntryPrice = cost / p.Size
		p.OpeningFee += fee
		p.CurrentPrice = price
		p.PnL = (price-p.EntryPrice)*p.Size - p.OpeningFee
		*entry = p.EntryPrice
	case signalActionCloseLong:
		if *holding == nil {
			return
		}
		p := *holding
		quantity = math.Min(quantity, p.Size)
		openingFee := p.OpeningFee * quantity / p.Size
		stats.TotalPnL += (price-p.EntryPrice)*quantity - openingFee - fee
		p.OpeningFee -= openingFee
		p.Size -= quantity
		p.CurrentPrice = price
		p.PnL = (price-p.EntryPrice)*p.Size - p.OpeningFee
		if p.Size <= entryQtyEpsilon {
			*holding, *entry = nil, 0
		}
	}
	stats.TotalVolume += quantity * price
}
