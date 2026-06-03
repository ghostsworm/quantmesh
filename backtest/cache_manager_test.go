package backtest

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func withTempWorkingDir(t *testing.T) string {
	t.Helper()
	original, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd failed: %v", err)
	}
	dir := t.TempDir()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir failed: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(original); err != nil {
			t.Fatalf("restore Chdir failed: %v", err)
		}
	})
	return dir
}

func writeCacheIndex(t *testing.T, entries map[string]CacheIndexEntry) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join("backtest", "cache"), 0755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	data, err := json.Marshal(entries)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join("backtest", "cache", "cache_index.json"), data, 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}
}

func TestListCacheSelfHealsIndexDataFromFilesAndNames(t *testing.T) {
	withTempWorkingDir(t)
	created := time.Now().Add(-48 * time.Hour)
	writeCacheIndex(t, map[string]CacheIndexEntry{
		"binance_BTCUSDT_1m_2026-01-01_2026-01-02": {Created: created},
		"ETHUSDT_5m_2026-02-01_2026-02-03": {
			Symbol: "ETHUSDT", Interval: "5m", Start: time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
			End: time.Date(2026, 2, 3, 0, 0, 0, 0, time.UTC), Candles: 7, SizeMB: 1.5, Created: created,
		},
	})
	csvPath := filepath.Join("backtest", "cache", "binance_BTCUSDT_1m_2026-01-01_2026-01-02.csv")
	if err := os.WriteFile(csvPath, []byte("ts,open\n1,100\n2,101\n"), 0644); err != nil {
		t.Fatalf("WriteFile csv failed: %v", err)
	}

	caches, err := ListCache()
	if err != nil {
		t.Fatalf("ListCache failed: %v", err)
	}
	if len(caches) != 2 {
		t.Fatalf("cache count = %d", len(caches))
	}
	var healed CacheInfo
	for _, cache := range caches {
		if cache.Name == "binance_BTCUSDT_1m_2026-01-01_2026-01-02" {
			healed = cache
		}
	}
	if healed.Symbol != "BTCUSDT" || healed.Interval != "1m" || healed.Candles != 2 || healed.SizeMB <= 0 {
		t.Fatalf("unexpected healed cache: %#v", healed)
	}
	if healed.Start.Format("2006-01-02") != "2026-01-01" || healed.End.Format("2006-01-02") != "2026-01-02" {
		t.Fatalf("unexpected parsed dates: %#v", healed)
	}
}

func TestCacheManagerStatsDeleteClearAndCleanOld(t *testing.T) {
	withTempWorkingDir(t)
	oldCreated := time.Now().AddDate(0, 0, -10)
	newCreated := time.Now()
	writeCacheIndex(t, map[string]CacheIndexEntry{
		"old_BTCUSDT_1m_2026-01-01_2026-01-02": {Symbol: "BTCUSDT", Interval: "1m", Created: oldCreated},
		"new_ETHUSDT_1m_2026-01-01_2026-01-02": {Symbol: "ETHUSDT", Interval: "1m", Created: newCreated},
	})
	for _, name := range []string{
		"old_BTCUSDT_1m_2026-01-01_2026-01-02.csv",
		"new_ETHUSDT_1m_2026-01-01_2026-01-02.csv",
	} {
		if err := os.WriteFile(filepath.Join("backtest", "cache", name), []byte("ts,open\n1,100\n"), 0644); err != nil {
			t.Fatalf("WriteFile failed: %v", err)
		}
	}

	stats, err := GetCacheStats()
	if err != nil {
		t.Fatalf("GetCacheStats failed: %v", err)
	}
	if stats.FileCount != 2 || stats.TotalSize == 0 || stats.SizeMB <= 0 {
		t.Fatalf("unexpected stats: %#v", stats)
	}

	if err := DeleteCache("new_ETHUSDT_1m_2026-01-01_2026-01-02"); err != nil {
		t.Fatalf("DeleteCache failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join("backtest", "cache", "new_ETHUSDT_1m_2026-01-01_2026-01-02.csv")); !os.IsNotExist(err) {
		t.Fatalf("expected deleted csv, stat err=%v", err)
	}

	if err := CleanOldCache(7); err != nil {
		t.Fatalf("CleanOldCache failed: %v", err)
	}
	caches, err := ListCache()
	if err != nil {
		t.Fatalf("ListCache after clean failed: %v", err)
	}
	if len(caches) != 0 {
		t.Fatalf("expected all indexed caches removed, got %#v", caches)
	}

	if err := ClearCache(); err != nil {
		t.Fatalf("ClearCache failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join("backtest", "cache")); !os.IsNotExist(err) {
		t.Fatalf("cache dir should be removed, err=%v", err)
	}
}

func TestCacheManagerMissingAndInvalidIndexPaths(t *testing.T) {
	withTempWorkingDir(t)
	caches, err := ListCache()
	if err != nil || len(caches) != 0 {
		t.Fatalf("missing index ListCache = %#v, %v", caches, err)
	}
	if err := DeleteCache("missing"); err != nil {
		t.Fatalf("DeleteCache missing index should be ignored: %v", err)
	}
	if lines, size := countCsvLinesAndSize(filepath.Join("missing", "file.csv")); lines != -1 || size != 0 {
		t.Fatalf("missing csv count = %d, %d", lines, size)
	}

	if err := os.MkdirAll(filepath.Join("backtest", "cache"), 0755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join("backtest", "cache", "cache_index.json"), []byte("{"), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}
	if _, err := ListCache(); err == nil {
		t.Fatal("expected invalid index error")
	}
	if err := DeleteCache("bad"); err == nil {
		t.Fatal("expected invalid index delete error")
	}
}
