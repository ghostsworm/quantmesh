package backtest

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"quantmesh/exchange"
)

func TestDataFetcherCacheCSVHelpers(t *testing.T) {
	for interval, duration := range map[string]time.Duration{
		"1m": time.Minute, "3m": 3 * time.Minute, "5m": 5 * time.Minute, "15m": 15 * time.Minute,
		"30m": 30 * time.Minute, "1h": time.Hour, "2h": 2 * time.Hour, "4h": 4 * time.Hour,
		"6h": 6 * time.Hour, "8h": 8 * time.Hour, "12h": 12 * time.Hour, "1d": 24 * time.Hour,
		"3d": 3 * 24 * time.Hour, "1w": 7 * 24 * time.Hour, "1M": 30 * 24 * time.Hour,
		"bad": time.Hour,
	} {
		if got := calculateBatchDuration(interval, 2); got != 2*duration {
			t.Fatalf("duration %s=%s want %s", interval, got, 2*duration)
		}
	}
	if _, err := parseCSVRecord([]string{"1"}); err == nil {
		t.Fatalf("bad field count should fail")
	}
	for idx := range []string{"bad", "1", "2", "3", "4", "5"} {
		record := []string{"1700000000000", "1", "2", "0.5", "1.5", "100", "BTCUSDT"}
		record[idx] = "bad"
		if _, err := parseCSVRecord(record); err == nil {
			t.Fatalf("bad csv field %d should fail", idx)
		}
	}
	candle, err := parseCSVRecord([]string{"1700000000000", "1", "2", "0.5", "1.5", "100", "BTCUSDT"})
	if err != nil || candle.Symbol != "BTCUSDT" || !candle.IsClosed {
		t.Fatalf("candle=%#v err=%v", candle, err)
	}

	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.MkdirAll(filepath.Join("data", "kline"), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	csvPath := filepath.Join("data", "kline", "cache-key.csv")
	content := "timestamp,open,high,low,close,volume,symbol\n1700000000000,1,2,0.5,1.5,100,BTCUSDT\n"
	if err := os.WriteFile(csvPath, []byte(content), 0644); err != nil {
		t.Fatalf("write csv: %v", err)
	}
	loaded, err := LoadFromCache("cache-key")
	if err != nil || len(loaded) != 1 {
		t.Fatalf("load cache len=%d err=%v", len(loaded), err)
	}
	if _, err := loadCandlesFromFile(filepath.Join("data", "kline", "missing.csv")); err == nil {
		t.Fatalf("missing file should fail")
	}
	badHeader := filepath.Join("data", "kline", "bad-header.csv")
	if err := os.WriteFile(badHeader, []byte(""), 0644); err != nil {
		t.Fatalf("write bad header: %v", err)
	}
	if _, err := loadCandlesFromFile(badHeader); err == nil {
		t.Fatalf("bad header should fail")
	}
	if err := SaveToCache("empty", nil); err != nil {
		t.Fatalf("empty save should be no-op: %v", err)
	}
	if err := os.MkdirAll(filepath.Join("backtest", "cache"), 0755); err != nil {
		t.Fatalf("mkdir legacy: %v", err)
	}
	if err := SaveToCache("binance_BTCUSDT_1m_2023-01-01_2023-01-02", []*exchange.Candle{candle}); err != nil {
		t.Fatalf("save cache: %v", err)
	}
	if _, err := os.Stat(filepath.Join("data", "kline", "binance_BTCUSDT_1m_2023-01-01_2023-01-02.csv")); err != nil {
		t.Fatalf("saved csv missing: %v", err)
	}
}
