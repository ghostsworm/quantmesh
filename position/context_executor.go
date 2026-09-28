package position

import "context"

// ContextOrderExecutor is required for protective market submission. A venue
// adapter must not bypass intent ownership just because its caller is stopping.
type ContextOrderExecutor interface {
	PlaceOrderContext(context.Context, *OrderRequest) (*Order, error)
}

// ContextBatchOrderExecutor keeps protective limit submission inside the same
// cancellation boundary as preflight and settlement.
type ContextBatchOrderExecutor interface {
	BatchPlaceOrdersWithDetailsContext(context.Context, []*OrderRequest) *BatchPlaceOrdersResult
}
