package order

import (
	"strings"
	"time"

	"quantmesh/exchange"
	"quantmesh/utils"
)

// VenueClientOrderID exposes the exact alias used on the wire so callers can
// register both identities before submitting, not only after a REST response.
func (oe *ExchangeOrderExecutor) VenueClientOrderID(clientOrderID string) string {
	return utils.AddBrokerPrefix(strings.ToLower(oe.exchange.GetName()), clientOrderID)
}

// Map acknowledgements and recovered orders identically. In particular, a lost
// acknowledgement followed by a FILLED lookup must not invent execution at the
// originally requested price/quantity (venues can quantize/upsize orders).
func (oe *ExchangeOrderExecutor) mapVenueOrder(req *OrderRequest, fallbackPrice float64, raw *exchange.Order) *Order {
	if raw == nil || raw.OrderID <= 0 || (raw.Symbol != "" && raw.Symbol != req.Symbol) {
		return nil
	}
	cid := raw.ClientOrderID
	if cid == "" {
		cid = req.ClientOrderID
	} else if cid != req.ClientOrderID && cid != utils.AddBrokerPrefix(strings.ToLower(oe.exchange.GetName()), req.ClientOrderID) {
		return nil
	}
	price, qty := fallbackPrice, req.Quantity
	if raw.Price > 0 {
		price = raw.Price
	}
	if raw.Quantity > 0 {
		qty = raw.Quantity
	}
	if raw.Status == exchange.OrderStatusFilled {
		// Original order quantity and cumulative fills are independent evidence.
		// Copying fills into Quantity would manufacture a consistent FILLED report
		// and let a truncated/malformed response release the remaining reservation.
		if raw.AvgPrice > 0 {
			price = raw.AvgPrice
		}
	}
	return &Order{OrderID: raw.OrderID, ClientOrderID: cid, Symbol: req.Symbol, Side: req.Side,
		Price: price, Quantity: qty, Status: string(raw.Status), CreatedAt: time.Now(),
		ExecutedQty: raw.ExecutedQty, AvgPrice: raw.AvgPrice}
}
