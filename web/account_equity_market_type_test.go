package web

import (
	"testing"
	"time"

	"quantmesh/storage"
)

type marketAccountEquityTestStore struct {
	storage.Storage
	requested string
	scope     string
}

func (s *marketAccountEquityTestStore) QueryAccountEquityRecordsByScope(_, marketType, scope string, start, _ time.Time) ([]*storage.AccountEquityRecord, error) {
	s.requested, s.scope = marketType, scope
	return []*storage.AccountEquityRecord{{MarketType: marketType, Timestamp: start, AccountEquity: map[string]float64{"spot": 100, "futures": 900}[marketType]}}, nil
}

func (*marketAccountEquityTestStore) QueryHourlyEquityRecordsByMarketType(string, string, string, string, time.Time, time.Time) ([]*storage.HourlyEquityRecord, error) {
	return nil, nil
}

func TestAccountEquityCurveQueriesByOpaqueAccountScopeAndMarketType(t *testing.T) {
	store := &marketAccountEquityTestStore{}
	start := time.Date(2026, 9, 27, 0, 0, 0, 0, time.UTC)
	for marketType, want := range map[string]float64{"spot": 100, "futures": 900} {
		got := lastAccountEquityPerDayFromHourly(store, "binance", marketType, "BTCUSDT", "short-prefix", "opaque-scope", start, start.Add(24*time.Hour))
		if got["2026-09-27"] != want || store.requested != marketType || store.scope != "opaque-scope" {
			t.Fatalf("market %s equity curve=%v queried=%s scope=%s want=%v", marketType, got, store.requested, store.scope, want)
		}
	}
}
