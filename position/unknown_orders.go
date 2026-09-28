package position

// retainUnknownOrders runs after a batch result, before releasing failed slots.
// It does not overwrite a newer WS acknowledgement or an already handled fill.
func (spm *SuperPositionManager) retainUnknownOrders(requests []*OrderRequest, unknown map[string]bool) {
	if len(unknown) == 0 {
		return
	}
	spm.openingGate.Block("unknown_orders")
	for _, req := range requests {
		if req == nil || !unknown[req.ClientOrderID] {
			continue
		}
		price, side, valid := spm.parseClientOrderID(req.ClientOrderID)
		if !valid {
			continue
		}
		slot := spm.getOrCreateSlot(price)
		slot.mu.Lock()
		if slot.SlotStatus == SlotStatusPending {
			slot.OrderID = 0
			slot.ClientOID = req.ClientOrderID
			slot.OrderSide = side
			slot.OrderPrice = req.Price
			slot.OrderStatus = OrderStatusUnknown
			slot.OrderCreatedAt = spm.now()
			slot.SlotStatus = SlotStatusLocked
		}
		slot.mu.Unlock()
	}
}
