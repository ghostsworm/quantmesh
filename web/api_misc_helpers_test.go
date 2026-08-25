package web

import (
	"path/filepath"
	"testing"
	"time"

	"quantmesh/storage"
)

func TestLogStorageAdapterAndSmallHelpers(t *testing.T) {
	ls, err := storage.NewLogStorage(filepath.Join(t.TempDir(), "logs.db"))
	if err != nil {
		t.Fatalf("NewLogStorage: %v", err)
	}
	t.Cleanup(func() { _ = ls.Close() })

	ls.WriteLog("INFO", "binance BTCUSDT started", "bot-a")
	ls.WriteLog("ERROR", "binance BTCUSDT failed", "bot-a")

	adapter := NewLogStorageAdapter(ls)
	SetLogStorageProvider(adapter)
	t.Cleanup(func() { SetLogStorageProvider(nil) })

	var logs []*LogRecordResponse
	var total int
	var queryErr error
	for i := 0; i < 30; i++ {
		logs, total, queryErr = adapter.GetLogs(storage.LogQueryParams{Limit: 10, Offset: 0, BotID: "bot-a"})
		if queryErr != nil {
			t.Fatalf("GetLogs: %v", queryErr)
		}
		if total == 2 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if total != 2 || len(logs) != 2 {
		t.Fatalf("unexpected logs total=%d len=%d logs=%+v", total, len(logs), logs)
	}
	if logs[0].BotID != "bot-a" || logs[0].Level == "" {
		t.Fatalf("unexpected converted log: %+v", logs[0])
	}

	stats, err := adapter.GetLogStats()
	if err != nil {
		t.Fatalf("GetLogStats: %v", err)
	}
	if len(stats) == 0 {
		t.Fatal("expected non-empty log stats")
	}
	if err := adapter.Vacuum(); err != nil {
		t.Fatalf("Vacuum: %v", err)
	}
	if cleaned, err := adapter.CleanOldLogsByLevel(0, []string{"INFO", "ERROR"}); err != nil {
		t.Fatalf("CleanOldLogsByLevel: %v", err)
	} else if cleaned == 0 {
		t.Fatalf("expected at least one cleaned row")
	}

	if got := trimClearable("  __clear__  "); got != "" {
		t.Fatalf("trimClearable clear marker = %q", got)
	}
	if got := trimClearable("  keep  "); got != "keep" {
		t.Fatalf("trimClearable keep = %q", got)
	}
	if got := maskDSN("short@example"); got != "*****@***" {
		t.Fatalf("maskDSN short = %q", got)
	}
	if got := maskDSN("abcdefghijklmnopqr@example"); got != "abcdefghijkl...opqr@***" {
		t.Fatalf("maskDSN long = %q", got)
	}
	if got := maskDSN("plain-secret"); got == "plain-secret" || got == "" {
		t.Fatalf("maskDSN no-at did not mask: %q", got)
	}

	if limit, err := parseLimitParam("25"); err != nil || limit != 25 {
		t.Fatalf("parseLimitParam valid = %d/%v", limit, err)
	}
	if _, err := parseLimitParam("bad"); err == nil {
		t.Fatal("parseLimitParam invalid should fail")
	}
}
