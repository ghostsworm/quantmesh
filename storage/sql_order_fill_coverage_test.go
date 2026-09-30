package storage

import (
	"testing"
	"time"
)

func TestOrderFillCoveragePersistsAcrossStorageRestartAndRejectsGaps(t *testing.T) {
	path := t.TempDir() + "/coverage.db"
	start := time.Date(2026, 9, 25, 0, 0, 0, 0, time.UTC)
	through := start.Add(12 * time.Hour)
	st, err := NewSQLStorage(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AdvanceOrderFillCoverage("binance", "spot", "BTCUSDT", "scope-a", start, through); err != nil {
		t.Fatal(err)
	}
	if err := st.AdvanceOrderFillCoverage("binance", "spot", "BTCUSDT", "scope-a", through.Add(2*time.Second), through.Add(24*time.Hour)); err == nil {
		t.Fatal("coverage update across an unqueried interval must be rejected")
	}
	if err := st.AdvanceOrderFillCoverage("binance", "spot", "BTCUSDT", "scope-a", start.Add(-2*time.Second), start.Add(-time.Second)); err == nil {
		t.Fatal("older disjoint coverage must not be merged across an unqueried interval")
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := NewSQLStorage(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	coverage, err := reopened.GetOrderFillCoverage("binance", "spot", "BTCUSDT", "scope-a")
	if err != nil {
		t.Fatal(err)
	}
	if coverage == nil || !coverage.CoveredFrom.Equal(start) || !coverage.CoveredThrough.Equal(through) {
		t.Fatalf("persisted coverage mismatch: %+v", coverage)
	}
	if err := reopened.AdvanceOrderFillCoverage("binance", "spot", "BTCUSDT", "scope-a", through.Add(-10*time.Minute), through.Add(24*time.Hour)); err != nil {
		t.Fatalf("overlapping history replay should extend coverage: %v", err)
	}
	coverage, err = reopened.GetOrderFillCoverage("binance", "spot", "BTCUSDT", "scope-a")
	if err != nil || coverage == nil || !coverage.CoveredFrom.Equal(start) || !coverage.CoveredThrough.Equal(through.Add(24*time.Hour)) {
		t.Fatalf("overlap merge failed: coverage=%+v err=%v", coverage, err)
	}
}

func TestOrderFillCoverageIsIsolatedByAccountAndMarket(t *testing.T) {
	st, err := NewSQLStorage(t.TempDir() + "/coverage-scope.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	now := time.Now().UTC()
	if err := st.AdvanceOrderFillCoverage("binance", "spot", "BTCUSDT", "scope-a", now.Add(-time.Hour), now); err != nil {
		t.Fatal(err)
	}
	for _, scope := range [][4]string{{"binance", "spot", "BTCUSDT", "scope-b"}, {"binance", "futures", "BTCUSDT", "scope-a"}, {"okx", "spot", "BTCUSDT", "scope-a"}} {
		got, err := st.GetOrderFillCoverage(scope[0], scope[1], scope[2], scope[3])
		if err != nil || got != nil {
			t.Fatalf("coverage leaked into scope %v: %+v err=%v", scope, got, err)
		}
	}
}
