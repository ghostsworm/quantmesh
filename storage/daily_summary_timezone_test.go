package storage

import (
	"path/filepath"
	"testing"
	"time"

	"quantmesh/utils"

	_ "github.com/mattn/go-sqlite3"
)

// TestGetDailyTradesSummary_TimezoneBoundary 時區邊界回歸測試。
//
// GetDailyTradesSummary 按「配置時區」對 created_at 分桶。若調用方用 UTC
// 或本機時區來構造日期串，在兩者跨日的窗口裡就會查空。
//
// 本測試不依賴「跑測試的當下是幾點」——它顯式構造一個落在邊界上的時間戳，
// 因此任何時候跑都能穩定復現，不像原先那樣只在 UTC 16:00~24:00 才暴露。
func TestGetDailyTradesSummary_TimezoneBoundary(t *testing.T) {
	if err := utils.SetLocation("Asia/Shanghai"); err != nil {
		t.Fatalf("設定時區失敗: %v", err)
	}

	st, err := NewSQLStorage(filepath.Join(t.TempDir(), "tz.db"))
	if err != nil {
		t.Fatalf("建立存儲失敗: %v", err)
	}
	defer st.Close()

	// 2026-08-25 16:30 UTC == 2026-08-26 00:30 Asia/Shanghai
	// 兩個時區分屬不同日期，正是先前踩坑的窗口。
	utcTime := time.Date(2026, 8, 25, 16, 30, 0, 0, time.UTC)
	utcDate := utcTime.Format("2006-01-02")                            // "2026-08-25"
	tzDate := utils.ToConfiguredTimezone(utcTime).Format("2006-01-02") // "2026-08-26"

	if utcDate == tzDate {
		t.Fatalf("測試前提失效：UTC 日期與配置時區日期應當不同，實得 %s", utcDate)
	}

	if err := st.SaveTrade(&Trade{
		Exchange:  "binance",
		Symbol:    "BTCUSDT",
		Account:   "acct-tz",
		BotID:     "bot-tz",
		BuyPrice:  100,
		SellPrice: 112.5,
		Quantity:  1,
		PnL:       12.5,
		Fee:       0.5,
		CreatedAt: utcTime,
	}); err != nil {
		t.Fatalf("寫入成交失敗: %v", err)
	}

	// 用配置時區的日期查詢：應當命中
	count, gross, fee, err := st.GetDailyTradesSummary("binance", "acct-tz", tzDate, "bot-tz")
	if err != nil {
		t.Fatalf("GetDailyTradesSummary(%s): %v", tzDate, err)
	}
	if count != 1 || gross != 12.5 || fee != 0.5 {
		t.Errorf("按配置時區日期 %s 查詢應命中，實得 count=%d gross=%v fee=%v", tzDate, count, gross, fee)
	}

	// 用 UTC 日期查詢：應當落空——這正是先前 mcp/tools_pnl.go 的行為，
	// 也是原測試每天固定失敗 8 小時的根因。
	count, _, _, err = st.GetDailyTradesSummary("binance", "acct-tz", utcDate, "bot-tz")
	if err != nil {
		t.Fatalf("GetDailyTradesSummary(%s): %v", utcDate, err)
	}
	if count != 0 {
		t.Errorf("按 UTC 日期 %s 查詢應落空（證明分桶確實按配置時區），實得 count=%d", utcDate, count)
	}
}
