package main

import (
	"context"
	"fmt"
	"math"
	"time"
)

type fundingCarryStartupPriceReader interface{ GetLastPrice() float64 }

const fundingCarryInitialPriceAttempts = 10
const fundingCarryInitialPriceInterval = 500 * time.Millisecond

func waitFundingCarryInitialPrice(ctx context.Context, reader fundingCarryStartupPriceReader, interval time.Duration) error {
	if ctx == nil || reader == nil {
		return fmt.Errorf("funding_carry initial price requires context and monitor")
	}
	if interval <= 0 {
		interval = fundingCarryInitialPriceInterval
	}
	for attempt := 0; attempt <= fundingCarryInitialPriceAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		price := reader.GetLastPrice()
		if math.IsNaN(price) || math.IsInf(price, 0) {
			return fmt.Errorf("funding_carry initial price is not finite")
		}
		if price > 0 {
			return ctx.Err()
		}
		if attempt == fundingCarryInitialPriceAttempts {
			break
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	return fmt.Errorf("無法獲取初始價格")
}
