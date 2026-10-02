package order

import (
	"context"
	"fmt"
	"time"

	"quantmesh/execution"
)

// Configure before publication. This supplements, not replaces, the ordinary
// admission/UI quote provider; capital proof never calls the synchronous one.
func (oe *ExchangeOrderExecutor) SetCapitalExposureQuoteProvider(provider func(context.Context) (float64, time.Time, error)) {
	oe.capitalExposureQuoteProvider = provider
}

func (oe *ExchangeOrderExecutor) CapitalReleaseExposureSnapshot(ctx context.Context) (*execution.ExposureSnapshot, error) {
	if oe == nil || ctx == nil || oe.exposureBook == nil {
		return nil, fmt.Errorf("capital release exposure evidence is unavailable")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var quote *execution.ExposureQuote
	if oe.capitalExposureQuoteProvider != nil {
		price, at, err := oe.capitalExposureQuoteProvider(ctx)
		if err != nil {
			return nil, err
		}
		quote = &execution.ExposureQuote{Price: price, At: at}
	} else if oe.exposureMarkProvider != nil {
		return nil, fmt.Errorf("capital release quote provider lacks cancellable evidence capability")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	snapshot, err := oe.exposureBook.CapitalReleaseSnapshot(ctx, time.Now(), quote)
	if err != nil {
		return nil, err
	}
	// Proof does not run reconcileExposureLimitBlock, which may launch trading
	// cancellations. Normal admission/monitor risk processing stays unchanged.
	return &snapshot, nil
}
