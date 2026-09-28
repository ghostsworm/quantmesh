package web

import (
	"errors"
	"testing"
	"time"
)

type fundingTotalsStub struct {
	calls    int
	failCall int
}

func (s *fundingTotalsStub) GetFundingPaymentsSum(_, _ string, _, _ time.Time) (float64, error) {
	s.calls++
	if s.calls == s.failCall {
		return 0, errors.New("database unavailable")
	}
	return float64(s.calls), nil
}

func TestReadFundingProfitTotalsRejectsAnyFailedWindow(t *testing.T) {
	for failCall := 1; failCall <= 4; failCall++ {
		t.Run(string(rune('0'+failCall)), func(t *testing.T) {
			reader := &fundingTotalsStub{failCall: failCall}
			now := time.Now()
			_, err := readFundingProfitTotals(reader, "account", "binance", now.AddDate(-6, 0, 0), now.Add(-time.Hour), now.Add(-24*time.Hour), now.Add(-30*24*time.Hour), now)
			if err == nil {
				t.Fatalf("expected error for failed funding query %d", failCall)
			}
			if reader.calls != failCall {
				t.Fatalf("query calls=%d, want stop at failed query %d", reader.calls, failCall)
			}
		})
	}
}

func TestReadFundingProfitTotalsReturnsAllWindows(t *testing.T) {
	reader := &fundingTotalsStub{}
	now := time.Now()
	got, err := readFundingProfitTotals(reader, "account", "binance", now.AddDate(-6, 0, 0), now.Add(-time.Hour), now.Add(-24*time.Hour), now.Add(-30*24*time.Hour), now)
	if err != nil {
		t.Fatal(err)
	}
	if got.Total != 1 || got.Today != 2 || got.Week != 3 || got.Month != 4 {
		t.Fatalf("funding totals=%+v", got)
	}
}
