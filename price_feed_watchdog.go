package main

import (
	"context"
	"time"
)

const (
	priceFeedStaleAfter  = 15 * time.Second
	priceFeedCheckEvery  = time.Second
	priceFeedBlockReason = "price_feed_stale"
)

// watchPriceFeedHealth turns silent WebSocket feeds into an independent
// fail-closed opening hold. Recovery only clears this watcher’s own hold.
func watchPriceFeedHealth(ctx context.Context, interval time.Duration, isStale func() bool, onTransition func(bool)) {
	if interval <= 0 || isStale == nil || onTransition == nil {
		return
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	wasStale := false
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			stale := isStale()
			if stale == wasStale {
				continue
			}
			wasStale = stale
			onTransition(stale)
		}
	}
}
