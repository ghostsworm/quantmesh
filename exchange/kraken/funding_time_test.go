package kraken

import (
	"testing"
	"time"
)

func TestNextKrakenFundingUTC(t *testing.T) {
	now := time.Date(2026, 4, 8, 10, 30, 45, 0, time.UTC)
	next := nextKrakenFundingUTC(now)
	if next.Hour() != 11 || next.Minute() != 0 || next.Second() != 0 {
		t.Fatalf("want 11:00:00 UTC, got %v", next)
	}
}
