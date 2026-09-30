package exchange

import "fmt"

// rejectUnsupportedReduceOnly prevents adapters that cannot represent the
// venue's reduce-only flag from silently submitting a close as an opening order.
func rejectUnsupportedReduceOnly(exchangeName string, req *OrderRequest) error {
	if req != nil && req.ReduceOnly {
		return fmt.Errorf("%s adapter does not support reduce-only orders", exchangeName)
	}
	return nil
}
