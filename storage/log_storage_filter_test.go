package storage

import (
	"path/filepath"
	"testing"
	"time"
)

func TestLogStorage_GetLogs_BotIDColumn(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "logfilter.db")
	ls, err := NewLogStorage(path)
	if err != nil {
		t.Fatal(err)
	}
	defer ls.Close()

	ls.WriteLog("INFO", "noise", "")
	ls.WriteLog("INFO", "target row", "bot-xyz")
	time.Sleep(2 * time.Second)

	logs, total, err := ls.GetLogs(LogQueryParams{
		BotID: "bot-xyz",
		Limit: 50,
	})
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(logs) != 1 {
		t.Fatalf("expected 1 row, got total=%d len=%d", total, len(logs))
	}
	if logs[0].BotID != "bot-xyz" || logs[0].Message != "target row" {
		t.Fatalf("unexpected record: %+v", logs[0])
	}
}

func TestLogStorage_GetLogs_OptionalMessageFilters(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "logfilter2.db")
	ls, err := NewLogStorage(path)
	if err != nil {
		t.Fatal(err)
	}
	defer ls.Close()

	ls.WriteLog("INFO", "noise binance ETHUSDT futures", "")
	ls.WriteLog("INFO", "okx line", "bot-xyz")
	time.Sleep(2 * time.Second)

	logs, total, err := ls.GetLogs(LogQueryParams{
		Keyword:    "ETHUSDT",
		Exchange:   "okx",
		MarketType: "futures",
		BotID:      "bot-xyz",
		Limit:      50,
	})
	if err != nil {
		t.Fatal(err)
	}
	if total != 0 || len(logs) != 0 {
		t.Fatalf("AND of column bot_id + message filters should exclude mismatch, got total=%d", total)
	}
}
