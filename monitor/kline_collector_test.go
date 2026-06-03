package monitor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"quantmesh/exchange"
)

func TestKlineCollectorFilenameParsingAndListing(t *testing.T) {
	dir := t.TempDir()
	kc := NewKlineCollector(nil, nil, nil)
	kc.dataDir = dir

	if kc.GetDataDir() != dir {
		t.Fatalf("GetDataDir = %s, want %s", kc.GetDataDir(), dir)
	}
	if !kc.isKlineFile("tick_binance_BTCUSDT_20260603.csv") {
		t.Fatal("tick csv should be recognized")
	}
	if !isKlineFilename("1m_okx_ETHUSDT_20260603.csv") || !isKlineFilename("1h_gate_SOLUSDT_20260603.csv") {
		t.Fatal("1m/1h csv should be recognized")
	}
	if isKlineFilename("daily_binance_BTCUSDT_20260603.csv") || isKlineFilename("tick_binance.txt") {
		t.Fatal("non-kline files should not be recognized")
	}

	parts := splitFilename("tick_binance_BTCUSDT_20260603.csv")
	if strings.Join(parts, "|") != "tick|binance|BTCUSDT|20260603" {
		t.Fatalf("split parts = %#v", parts)
	}
	info := kc.parseFilename("1m_binance_BTCUSDT_20260603.csv")
	if info.Interval != "1m" || info.Exchange != "binance" || info.Symbol != "BTCUSDT" || !info.HasDepth {
		t.Fatalf("parseFilename = %#v", info)
	}
	tickInfo := parseKlineFilename("tick_okx_ETHUSDT_20260603.csv")
	if tickInfo.HasDepth || tickInfo.Interval != "tick" {
		t.Fatalf("tick parse = %#v", tickInfo)
	}

	if err := os.WriteFile(filepath.Join(dir, "tick_binance_BTCUSDT_20260603.csv"), []byte("header\nrow\n"), 0644); err != nil {
		t.Fatalf("write kline file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "note.txt"), []byte("ignore"), 0644); err != nil {
		t.Fatalf("write ignored file: %v", err)
	}
	if err := os.Mkdir(filepath.Join(dir, "nested"), 0755); err != nil {
		t.Fatalf("mkdir nested: %v", err)
	}

	files, err := kc.ListFiles()
	if err != nil {
		t.Fatalf("ListFiles returned error: %v", err)
	}
	if len(files) != 1 || files[0].Filename != "tick_binance_BTCUSDT_20260603.csv" {
		t.Fatalf("ListFiles = %#v", files)
	}
	fromDir, err := ListKlineFilesFromDir(dir, nil)
	if err != nil {
		t.Fatalf("ListKlineFilesFromDir returned error: %v", err)
	}
	if len(fromDir) != 1 {
		t.Fatalf("ListKlineFilesFromDir length = %d, want 1", len(fromDir))
	}
	missing, err := ListKlineFilesFromDir(filepath.Join(dir, "missing"), nil)
	if err != nil || len(missing) != 0 {
		t.Fatalf("missing dir result = %#v err=%v", missing, err)
	}
}

func TestKlineCollectorCSVWritersAndCleanup(t *testing.T) {
	dir := t.TempDir()
	kc := &KlineCollector{dataDir: dir}
	klines := []*exchange.Candle{
		{Timestamp: 1, Open: 10, High: 11, Low: 9, Close: 10.5, Volume: 100},
		{Timestamp: 2, Open: 11, High: 12, Low: 10, Close: 11.5, Volume: 200},
	}

	plainPath := filepath.Join(dir, "tick_binance_BTCUSDT_20260603.csv")
	if err := kc.saveKlinesToCSV(plainPath, klines, false); err != nil {
		t.Fatalf("saveKlinesToCSV create: %v", err)
	}
	if err := kc.saveKlinesToCSV(plainPath, klines[:1], true); err != nil {
		t.Fatalf("saveKlinesToCSV append: %v", err)
	}
	plain, err := os.ReadFile(plainPath)
	if err != nil {
		t.Fatalf("read plain csv: %v", err)
	}
	if got := strings.Count(string(plain), "\n"); got != 4 {
		t.Fatalf("plain csv newline count = %d, want 4\n%s", got, string(plain))
	}

	depthPath := filepath.Join(dir, "1m_binance_BTCUSDT_20260603.csv")
	orderbook := &exchange.OrderBook{
		Bids: []exchange.OrderBookLevel{{Price: 10, Quantity: 1}, {Price: 9, Quantity: 2}},
		Asks: []exchange.OrderBookLevel{{Price: 11, Quantity: 1}},
	}
	if err := kc.saveKlineWithDepthToCSV(depthPath, klines[0], orderbook); err != nil {
		t.Fatalf("saveKlineWithDepthToCSV create: %v", err)
	}
	if err := kc.saveKlineWithDepthToCSV(depthPath, klines[1], nil); err != nil {
		t.Fatalf("saveKlineWithDepthToCSV append nil depth: %v", err)
	}
	depth, err := os.ReadFile(depthPath)
	if err != nil {
		t.Fatalf("read depth csv: %v", err)
	}
	if !strings.Contains(string(depth), "bid_price_5") || strings.Count(string(depth), "\n") != 3 {
		t.Fatalf("unexpected depth csv:\n%s", string(depth))
	}

	oldPath := filepath.Join(dir, "1h_gate_SOLUSDT_20200101.csv")
	if err := os.WriteFile(oldPath, []byte("old"), 0644); err != nil {
		t.Fatalf("write old file: %v", err)
	}
	oldTime := time.Now().AddDate(0, 0, -10)
	if err := os.Chtimes(oldPath, oldTime, oldTime); err != nil {
		t.Fatalf("chtimes old file: %v", err)
	}
	kc.cleanupOldFilesOnce()
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Fatalf("old file should be removed, stat err=%v", err)
	}
	if _, err := os.Stat(plainPath); err != nil {
		t.Fatalf("recent file should remain: %v", err)
	}
}

func TestKlineCollectorDatabaseSyncSkipsAndEstimates(t *testing.T) {
	dir := t.TempDir()
	kc := &KlineCollector{dataDir: dir}

	if err := kc.syncKlineFileToDatabase("bad.csv", false, true); err != nil {
		t.Fatalf("nil storage should skip bad filename, got %v", err)
	}
	if err := kc.updateCompletedKlineFiles(); err != nil {
		t.Fatalf("nil storage update should skip: %v", err)
	}
	if got := estimateCandleCount(0, false); got != 0 {
		t.Fatalf("zero estimate = %d, want 0", got)
	}
	if got := estimateCandleCount(400, false); got != 4 {
		t.Fatalf("plain estimate = %d, want 4", got)
	}
	if got := estimateCandleCount(1000, true); got != 4 {
		t.Fatalf("depth estimate = %d, want 4", got)
	}
	if protected, err := kc.getProtectedFiles(); err != nil || len(protected) != 0 {
		t.Fatalf("nil storage protected files = %#v err=%v", protected, err)
	}
}
