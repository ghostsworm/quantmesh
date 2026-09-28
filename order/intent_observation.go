package order

import "fmt"

// REST acknowledgements and WS updates can arrive out of order. Never replace
// durable cumulative fill evidence with an older NEW/partial acknowledgement.
func mergeIntentObservation(previous, next *Order) (*Order, error) {
	copy := *next
	if previous == nil {
		return &copy, nil
	}
	prior := *previous
	if next.ExecutedQty < previous.ExecutedQty {
		if terminalOrderStatus(next.Status) {
			return &prior, fmt.Errorf("terminal cumulative fill regressed")
		}
		return &prior, nil
	}
	if terminalOrderStatus(previous.Status) && !terminalOrderStatus(next.Status) {
		if next.ExecutedQty > previous.ExecutedQty {
			return &prior, fmt.Errorf("new fill after terminal observation requires reconciliation")
		}
		return &prior, nil
	}
	if copy.Quantity == 0 {
		copy.Quantity = previous.Quantity
	}
	if copy.Price == 0 {
		copy.Price = previous.Price
	}
	if copy.AvgPrice == 0 && copy.ExecutedQty == previous.ExecutedQty {
		copy.AvgPrice = previous.AvgPrice
	}
	return &copy, nil
}
