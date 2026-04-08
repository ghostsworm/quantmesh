package web

import (
	"testing"

	"quantmesh/storage"
)

func TestCollectDailyStatDateKeysInRange_IncludesFundingOnly(t *testing.T) {
	start, end := "2026-04-01", "2026-04-30"
	funding := map[string]float64{"2026-04-07": -1.25}
	keys := collectDailyStatDateKeysInRange(start, end, nil, nil, funding, nil, nil)
	if !keys["2026-04-07"] {
		t.Fatal("expected 2026-04-07 when only funding map has the date")
	}
}

func TestCollectDailyStatDateKeysInRange_IncludesExchangePnLOnly(t *testing.T) {
	start, end := "2026-04-01", "2026-04-30"
	ex := map[string]float64{"2026-04-07": 0.5}
	keys := collectDailyStatDateKeysInRange(start, end, nil, nil, nil, ex, nil)
	if !keys["2026-04-07"] {
		t.Fatal("expected 2026-04-07 when only exchange PnL map has the date")
	}
}

func TestCollectDailyStatDateKeysInRange_IncludesSnapshotOnly(t *testing.T) {
	start, end := "2026-04-01", "2026-04-30"
	snap := map[string]*storage.DailySnapshot{"2026-04-07": {}}
	keys := collectDailyStatDateKeysInRange(start, end, nil, nil, nil, nil, snap)
	if !keys["2026-04-07"] {
		t.Fatal("expected 2026-04-07 when only snapshot map has the date")
	}
}
