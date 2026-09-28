package strategy

import (
	"errors"
	"fmt"
	"strings"

	"quantmesh/execution"
	"quantmesh/position"
	"quantmesh/utils"
)

// submitSignalOrder runs with the strategy mutex held. Keep the CID and economic
// action even when REST has no result so a later WS fill can still find its owner.
func submitSignalOrder(executor position.OrderExecutorInterface, venue string, req *position.OrderRequest, action string,
	active **Order, pendingAction *string, holding **Position, entry *float64, stats *StrategyStatistics,
	exchange position.IExchange, persist func() error) error {
	if *active != nil {
		return execution.ErrIntentPending
	}
	tracked := &Order{ClientOrderID: req.ClientOrderID, Symbol: req.Symbol, Side: req.Side,
		Price: req.Price, Quantity: req.Quantity, Status: "PENDING",
		clientOrderAlias: utils.AddBrokerPrefix(strings.ToLower(venue), req.ClientOrderID)}
	*active, *pendingAction = tracked, action
	if persist != nil {
		if err := persist(); err != nil {
			*active, *pendingAction = nil, ""
			return err
		}
	}
	ord, err := executor.PlaceOrder(req)
	if err != nil && !errors.Is(err, execution.ErrOrderUnknown) {
		*active, *pendingAction = nil, ""
		if persist != nil {
			if persistErr := persist(); persistErr != nil {
				return errors.Join(err, persistErr)
			}
		}
		return err
	}
	if ord != nil {
		tracked.OrderID = ord.OrderID
		if ord.ClientOrderID != "" {
			tracked.ClientOrderID = ord.ClientOrderID
		}
		if ord.Quantity > 0 {
			tracked.Quantity = ord.Quantity
		}
		if ord.Price > 0 {
			tracked.Price = ord.Price
		}
	}
	persistAfterAcknowledgement := func() error {
		if persist == nil {
			return nil
		}
		persistErr := persist()
		if persistErr == nil {
			return nil
		}
		if tracker, ok := executor.(interface {
			MarkOrderReconciliationRequired(int64, string, string) error
		}); ok {
			if markErr := tracker.MarkOrderReconciliationRequired(tracked.OrderID, tracked.ClientOrderID,
				"signal order accepted/unknown but strategy state persistence failed"); markErr != nil {
				return errors.Join(persistErr, fmt.Errorf("mark order for reconciliation: %w", markErr))
			}
		}
		return persistErr
	}
	if err != nil || ord == nil {
		tracked.Status = position.OrderStatusUnknown
		if err == nil {
			err = fmt.Errorf("empty signal order acknowledgement: %w", execution.ErrOrderUnknown)
		}
		if persistErr := persistAfterAcknowledgement(); persistErr != nil {
			return errors.Join(err, persistErr)
		}
		return err
	}
	if ord.ExecutedQty > 0 || signalOrderStatusFilled(ord.Status) {
		// Placement acknowledgements do not carry per-fill fee currency/value.
		// Keep the order for the authoritative stream/poll response instead.
		tracked.Status = position.OrderStatusUnknown
		if persistErr := persistAfterAcknowledgement(); persistErr != nil {
			return persistErr
		}
		return nil
	}
	applySignalOrderUpdate(active, pendingAction, holding, entry, stats, exchange, executor, &position.OrderUpdate{
		OrderID: ord.OrderID, ClientOrderID: tracked.ClientOrderID, Symbol: req.Symbol, Side: req.Side,
		Status: ord.Status, ExecutedQty: ord.ExecutedQty, AvgPrice: ord.AvgPrice, Price: tracked.Price,
	})
	if persistErr := persistAfterAcknowledgement(); persistErr != nil {
		return persistErr
	}
	return nil
}
