package mcp

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"quantmesh/storage"
	"quantmesh/utils"
)

type scopedPnLStorageFixture struct {
	storage.Storage
	exchange string
	scope    string
	asset    string
	start    time.Time
	end      time.Time
	rows     []*storage.PnLBySymbol
}

func (f *scopedPnLStorageFixture) GetPnLByAccountScopeAndAsset(exchange, scope, asset string, start, end time.Time) ([]*storage.PnLBySymbol, error) {
	f.exchange, f.scope, f.asset, f.start, f.end = exchange, scope, asset, start, end
	return f.rows, nil
}

func TestPnLToolsRequireAndUseExactScopeAndAsset(t *testing.T) {
	store := &scopedPnLStorageFixture{rows: []*storage.PnLBySymbol{
		{Exchange: "binance", MarketType: "futures", Symbol: "BTCUSDT", PnLAsset: "USDT", TotalTrades: 2, TotalPnL: 10},
		{Exchange: "binance", MarketType: "futures", Symbol: "ETHUSDT", PnLAsset: "USDT", TotalTrades: 1, TotalPnL: -3},
	}}
	server := NewServer("test", nil)
	RegisterPnLTools(server, Providers{Storage: store})

	today, ok := findToolEntryForTest(t, server, "qm_pnl_today")
	if !ok {
		t.Fatal("qm_pnl_today was not registered")
	}
	result, err := today.Handler(context.Background(), json.RawMessage(`{"exchange":" BINANCE ","account_scope":"scope-a","pnl_asset":"usdt"}`))
	if err != nil {
		t.Fatalf("qm_pnl_today: %v", err)
	}
	todayResult := result.(map[string]any)
	if todayResult["net_pnl"] != float64(7) || todayResult["trades_count"] != 3 || store.exchange != "binance" || store.scope != "scope-a" || store.asset != "USDT" {
		t.Fatalf("today result/query scope mismatch: result=%#v store=%#v", todayResult, store)
	}
	now := utils.NowConfiguredTimezone()
	if !store.start.Equal(time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).UTC()) || store.end.Before(store.start) {
		t.Fatalf("today query interval is not local midnight-to-now: start=%s end=%s", store.start, store.end)
	}

	rangeTool, ok := findToolEntryForTest(t, server, "qm_pnl_range")
	if !ok {
		t.Fatal("qm_pnl_range was not registered")
	}
	if _, err := rangeTool.Handler(context.Background(), json.RawMessage(`{"exchange":"binance","account_scope":" ","pnl_asset":"USDT"}`)); err == nil {
		t.Fatal("range query without exact account scope must fail")
	}
	if _, err := rangeTool.Handler(context.Background(), json.RawMessage(`{"exchange":"binance","account_scope":"scope-a","pnl_asset":"USDT","days":30}`)); err != nil {
		t.Fatalf("scoped range query: %v", err)
	}
	if store.exchange != "binance" || store.scope != "scope-a" || store.asset != "USDT" || store.end.Sub(store.start) < 29*24*time.Hour {
		t.Fatalf("range query did not preserve exact dimensions: %#v", store)
	}
}
