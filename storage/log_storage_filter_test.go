package storage

import (
	"path/filepath"
	"testing"
	"time"
)

func TestLogStorage_GetLogs_OptionalMessageFilters(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "logfilter.db")
	ls, err := NewLogStorage(path)
	if err != nil {
		t.Fatal(err)
	}
	defer ls.Close()

	ls.WriteLog("INFO", "noise binance ETHUSDT futures")
	ls.WriteLog("INFO", "[bot=bot-xyz] okx BTCUSDT futures tick")
	time.Sleep(2 * time.Second)

	logs, total, err := ls.GetLogs(LogQueryParams{
		Keyword:    "BTCUSDT",
		Exchange:   "okx",
		MarketType: "futures",
		BotID:      "bot-xyz",
		Limit:      50,
	})
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(logs) != 1 {
		t.Fatalf("expected 1 row, got total=%d len=%d", total, len(logs))
	}
}
