package exchange

import (
	"testing"
	"time"
)

func TestEstimateNextFundingUTC8h(t *testing.T) {
	now := time.Date(2026, 4, 8, 3, 0, 0, 0, time.UTC)
	next := EstimateNextFundingUTC8h(now)
	if next.Hour() != 8 || next.Day() != 8 {
		t.Fatalf("want 08:00 same day, got %v", next)
	}
	now = time.Date(2026, 4, 8, 10, 0, 0, 0, time.UTC)
	next = EstimateNextFundingUTC8h(now)
	if next.Hour() != 16 {
		t.Fatalf("want 16:00, got %v", next)
	}
}
