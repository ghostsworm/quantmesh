package strategy

import (
	"strings"

	"quantmesh/order"
	"quantmesh/position"
)

func (mse *MultiStrategyExecutor) venueClientOrderID(cid string) string {
	if mse.executor == nil {
		return cid
	}
	return mse.executor.VenueClientOrderID(cid)
}

// registerCapitalIntentLocked must run before the physical call: Binance WS can
// deliver a broker-prefixed terminal report before REST returns an OrderID.
func (mse *MultiStrategyExecutor) registerCapitalIntentLocked(rec *orderCapital, cid string) {
	mse.bindOrderRouteLocked(0, cid, rec.strategy)
	mse.trackOrderLocked(rec, 0, cid)
	alias := mse.venueClientOrderID(cid)
	if alias != cid {
		mse.bindOrderRouteLocked(0, alias, rec.strategy)
		mse.trackOrderLocked(rec, 0, alias)
	}
}

func (mse *MultiStrategyExecutor) acknowledgeCapitalLocked(rec *orderCapital, ord *order.Order) {
	if rec == nil || rec.terminal {
		return // an earlier terminal WS report must not be resurrected by REST
	}
	mse.bindOrderRouteLocked(ord.OrderID, ord.ClientOrderID, rec.strategy)
	mse.trackOrderLocked(rec, ord.OrderID, ord.ClientOrderID)
	if ord.Quantity > 0 && ord.Quantity >= rec.filledQty {
		rec.quantity = ord.Quantity
	}
}

func (mse *MultiStrategyExecutor) applyCapitalAcknowledgement(ord *order.Order) {
	if ord == nil {
		return
	}
	if strings.EqualFold(ord.Status, orderStatusFilled) && ord.ExecutedQty <= 0 {
		// An acceptance response with no execution quantity is not a fill record.
		// Keep the reservation and wait for WS/query execution evidence.
		return
	}
	mse.OnOrderUpdate(&position.OrderUpdate{OrderID: ord.OrderID, ClientOrderID: ord.ClientOrderID,
		Symbol: ord.Symbol, Status: ord.Status, ExecutedQty: ord.ExecutedQty})
}
