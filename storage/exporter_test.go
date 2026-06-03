package storage

import (
	"archive/zip"
	"bytes"
	"encoding/csv"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

func TestExporterEncodeDataCoversJSONCSVAndFallback(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 3, 10, 11, 12, 0, time.UTC)
	closed := now.Add(time.Hour)
	exporter := NewExporter(nil)

	cases := []struct {
		name       string
		data       interface{}
		wantHeader []string
		wantCell   string
	}{
		{
			name: "trades",
			data: []*Trade{{
				BuyOrderID: 11, SellOrderID: 12, Exchange: "binance", Account: "acct",
				Symbol: "BTCUSDT", BuyPrice: 100, SellPrice: 110, Quantity: 0.5,
				PnL: 5, CreatedAt: now,
			}},
			wantHeader: []string{"buy_order_id", "sell_order_id", "exchange", "account", "symbol", "buy_price", "sell_price", "quantity", "pnl", "created_at"},
			wantCell:   "BTCUSDT",
		},
		{
			name: "orders",
			data: []*Order{{
				OrderID: 21, ClientOrderID: "client-21", Symbol: "ETHUSDT", Side: "BUY",
				Price: 2000, Quantity: 1.25, Status: "FILLED", CreatedAt: now, UpdatedAt: closed,
			}},
			wantHeader: []string{"order_id", "client_order_id", "symbol", "side", "price", "quantity", "status", "created_at", "updated_at"},
			wantCell:   "client-21",
		},
		{
			name: "positions",
			data: []*Position{{
				SlotPrice: 99, Symbol: "SOLUSDT", Size: 3, EntryPrice: 95,
				CurrentPrice: 101, PnL: 18, OpenedAt: now, ClosedAt: &closed,
			}},
			wantHeader: []string{"slot_price", "symbol", "size", "entry_price", "current_price", "pnl", "opened_at", "closed_at"},
			wantCell:   "SOLUSDT",
		},
		{
			name: "statistics",
			data: []*DailyStatisticsWithTradeCount{{
				Date: now, TotalTrades: 7, TotalVolume: 8, TotalPnL: 9,
				WinRate: 0.75, WinningTrades: 6, LosingTrades: 1,
			}},
			wantHeader: []string{"date", "total_trades", "total_volume", "total_pnl", "win_rate", "winning_trades", "losing_trades"},
			wantCell:   "7",
		},
		{
			name: "reconciliation",
			data: []*ReconciliationHistory{{
				Exchange: "gate", Symbol: "XRPUSDT", Account: "acct", ReconcileTime: now,
				LocalPosition: 10, ExchangePosition: 9, PositionDiff: 1,
				ActiveBuyOrders: 2, ActiveSellOrders: 3, EstimatedProfit: 4, ActualProfit: 5,
			}},
			wantHeader: []string{"exchange", "symbol", "account", "reconcile_time", "local_position", "exchange_position", "position_diff", "active_buy_orders", "active_sell_orders", "estimated_profit", "actual_profit"},
			wantCell:   "XRPUSDT",
		},
		{
			name: "system_metrics",
			data: []*SystemMetrics{{
				Timestamp: now, CPUPercent: 12.34, MemoryMB: 256.78,
				MemoryPercent: 45.6, ProcessID: 987,
			}},
			wantHeader: []string{"timestamp", "cpu_percent", "memory_mb", "memory_percent", "process_id"},
			wantCell:   "987",
		},
		{
			name: "risk_check_rows",
			data: []RiskCheckExportRow{{
				CheckTime: now, Symbol: "BNBUSDT", IsHealthy: true,
				PriceDeviation: 0.12345, VolumeRatio: 2.5, Reason: "ok",
			}},
			wantHeader: []string{"check_time", "symbol", "is_healthy", "price_deviation", "volume_ratio", "reason"},
			wantCell:   "true",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			data, contentType, err := exporter.encodeData(tc.data, ExportFormatCSV, tc.name)
			if err != nil {
				t.Fatalf("encodeData returned error: %v", err)
			}
			if contentType != "text/csv" {
				t.Fatalf("content type = %q, want text/csv", contentType)
			}

			rows, err := csv.NewReader(bytes.NewReader(data)).ReadAll()
			if err != nil {
				t.Fatalf("csv parse failed: %v", err)
			}
			if len(rows) != 2 {
				t.Fatalf("rows length = %d, want 2: %q", len(rows), data)
			}
			if strings.Join(rows[0], ",") != strings.Join(tc.wantHeader, ",") {
				t.Fatalf("header = %v, want %v", rows[0], tc.wantHeader)
			}
			if !rowContains(rows[1], tc.wantCell) {
				t.Fatalf("row %v does not contain %q", rows[1], tc.wantCell)
			}
		})
	}

	jsonData, contentType, err := exporter.encodeData(map[string]string{"kind": "fallback"}, ExportFormat("xml"), "unknown")
	if err != nil {
		t.Fatalf("fallback JSON returned error: %v", err)
	}
	if contentType != "application/json" {
		t.Fatalf("fallback content type = %q", contentType)
	}
	if !json.Valid(jsonData) || !strings.Contains(string(jsonData), "fallback") {
		t.Fatalf("fallback data is not expected JSON: %s", jsonData)
	}
}

func TestExporterNilStorageErrorsAndTradeFiltering(t *testing.T) {
	t.Parallel()

	exporter := NewExporter(nil)
	params := ExportParams{Format: ExportFormatJSON, Limit: 1}

	nilStorageCalls := []struct {
		name string
		call func() error
	}{
		{"trades", func() error { _, _, err := exporter.ExportTrades(params); return err }},
		{"orders", func() error { _, _, err := exporter.ExportOrders(params); return err }},
		{"positions", func() error { _, _, err := exporter.ExportPositions(params); return err }},
		{"statistics", func() error { _, _, err := exporter.ExportStatistics(params); return err }},
		{"reconciliation", func() error { _, _, err := exporter.ExportReconciliation(params); return err }},
		{"risk_checks", func() error { _, _, err := exporter.ExportRiskChecks(params); return err }},
		{"system_metrics", func() error { _, _, err := exporter.ExportSystemMetrics(params); return err }},
	}
	for _, tc := range nilStorageCalls {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if err := tc.call(); err == nil {
				t.Fatal("expected nil storage error")
			}
		})
	}

	trades := []*Trade{
		{Exchange: "binance", Symbol: "BTCUSDT", Account: "acct-a"},
		{Exchange: "gate", Symbol: "BTCUSDT", Account: "acct-a"},
		{Exchange: "binance", Symbol: "ETHUSDT", Account: "acct-b"},
	}
	if got := filterTrades(trades, "", "", ""); len(got) != len(trades) {
		t.Fatalf("unfiltered length = %d, want %d", len(got), len(trades))
	}
	got := filterTrades(trades, "binance", "BTCUSDT", "acct-a")
	if len(got) != 1 || got[0] != trades[0] {
		t.Fatalf("filtered trades = %#v, want first trade only", got)
	}
}

func TestCreateExportZipIncludesConfigLogsAndAuditFiles(t *testing.T) {
	t.Parallel()

	auditDir := t.TempDir()
	writeTestFile(t, auditDir+"/audit_trades_2026-06-02.csv", []byte("id,symbol\n1,BTCUSDT\n"))
	writeTestFile(t, auditDir+"/audit_trades_2026-05-01.csv", []byte("old\n"))
	writeTestFile(t, auditDir+"/audit_events_2026-06-02.jsonl", []byte(`{"event":"ok"}`+"\n"))
	writeTestFile(t, auditDir+"/skip.txt", []byte("skip"))

	exporter := NewExporter(nil)
	zipData, err := exporter.CreateExportZip(ExportParams{
		AuditDir:  auditDir,
		StartTime: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
		EndTime:   time.Date(2026, 6, 3, 0, 0, 0, 0, time.UTC),
	}, []map[string]string{{"level": "info"}}, []byte("bot: test\n"))
	if err != nil {
		t.Fatalf("CreateExportZip returned error: %v", err)
	}

	zr, err := zip.NewReader(bytes.NewReader(zipData), int64(len(zipData)))
	if err != nil {
		t.Fatalf("zip parse failed: %v", err)
	}
	names := make([]string, 0, len(zr.File))
	for _, f := range zr.File {
		names = append(names, f.Name)
	}

	assertZipContainsSuffix(t, names, "/config.yaml")
	assertZipContainsSuffix(t, names, "/logs.json")
	assertZipContainsSuffix(t, names, "/audit/audit_trades_2026-06-02.csv")
	assertZipContainsSuffix(t, names, "/audit/audit_events_2026-06-02.jsonl")
	assertZipOmitsSuffix(t, names, "/audit/audit_trades_2026-05-01.csv")
	assertZipOmitsSuffix(t, names, "/audit/skip.txt")
}

func rowContains(row []string, want string) bool {
	for _, cell := range row {
		if cell == want {
			return true
		}
	}
	return false
}

func writeTestFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := osWriteFile(path, data); err != nil {
		t.Fatalf("write test file %s: %v", path, err)
	}
}

func osWriteFile(path string, data []byte) error {
	return os.WriteFile(path, data, 0600)
}

func assertZipContainsSuffix(t *testing.T, names []string, suffix string) {
	t.Helper()
	for _, name := range names {
		if strings.HasSuffix(name, suffix) {
			return
		}
	}
	t.Fatalf("zip entries %v do not contain suffix %q", names, suffix)
}

func assertZipOmitsSuffix(t *testing.T, names []string, suffix string) {
	t.Helper()
	for _, name := range names {
		if strings.HasSuffix(name, suffix) {
			t.Fatalf("zip entries %v unexpectedly contain suffix %q", names, suffix)
		}
	}
}
