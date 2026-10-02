package execution

import (
	"context"
	"fmt"
	"time"
)

type ExposureQuote struct {
	Price float64
	At    time.Time
}

const capitalExposureLockRetryInterval = 5 * time.Millisecond

// CapitalReleaseSnapshot shares the normal quote/readiness and totals rules.
// It never seeds, reconciles or clears inventory, intents or risk reasons.
// A nil quote reads existing evidence; an invalid newer quote invalidates it.
func (b *ExposureBook) CapitalReleaseSnapshot(ctx context.Context, now time.Time, quote *ExposureQuote) (ExposureSnapshot, error) {
	if b == nil || ctx == nil {
		return ExposureSnapshot{}, fmt.Errorf("capital exposure snapshot requires book and context")
	}
	if err := b.lockCapitalExposure(ctx); err != nil {
		return ExposureSnapshot{}, err
	}
	defer b.mu.Unlock()
	if quote != nil {
		// Like refreshExposureMark, a superseded observation does not discard
		// newer evidence. Invalid current evidence changes readiness below.
		_ = b.observeMarkLocked(quote.Price, quote.At, now)
	}
	totals, err := b.totalsLockedContext(ctx, nil)
	if err != nil {
		return ExposureSnapshot{}, err
	}
	snapshot := b.snapshotFromTotalsLocked(now, totals)
	if err := ctx.Err(); err != nil {
		return ExposureSnapshot{}, err
	}
	return snapshot, nil
}

func (b *ExposureBook) lockCapitalExposure(ctx context.Context) error {
	var ticker *time.Ticker
	defer func() {
		if ticker != nil {
			ticker.Stop()
		}
	}()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if b.mu.TryLock() {
			if err := ctx.Err(); err != nil {
				b.mu.Unlock()
				return err
			}
			return nil
		}
		if ticker == nil {
			ticker = time.NewTicker(capitalExposureLockRetryInterval)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}
