package execution

import (
	"fmt"
	"time"
)

// ObserveMark consumes evidence time, not the time at which an old cached price
// was read. A bad observation invalidates the preceding good mark immediately.
func (b *ExposureBook) ObserveMark(price float64, at, now time.Time) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.observeMarkLocked(price, at, now)
}

func (b *ExposureBook) observeMarkLocked(price float64, at, now time.Time) error {
	if !at.IsZero() && at.Before(b.markEvidenceAt) {
		return fmt.Errorf("exposure quote superseded by newer evidence")
	}
	_, err := exposureNumber(price)
	if err != nil || price <= 0 || at.IsZero() || at.After(now) || at.Before(b.markAt) || now.Sub(at) > b.maxMarkAge {
		b.markValid = false
		b.markEvidenceAt = at
		if at.IsZero() || at.After(now) {
			b.markEvidenceAt = now
		}
		return fmt.Errorf("invalid or stale exposure mark")
	}
	b.mark, b.markAt, b.markValid = price, at, true
	b.markEvidenceAt = at
	return nil
}

// RequireReconciliation cannot be cleared by prices or configuration updates.
// Used when an account-level operation escapes the owned execution journal.
func (b *ExposureBook) RequireReconciliation(reason string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	_ = b.failLocked(reason)
	b.ready = false
}
