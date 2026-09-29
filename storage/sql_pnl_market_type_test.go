package storage

import (
	"database/sql"
	"errors"
	"os"
	"testing"
	"time"
)

func TestGetPnLByTimeRangeSeparatesMarketsAndKeepsLegacyUnknown(t *testing.T) {
	path := t.TempDir() + "/pnl-market.db"
	st, err := NewSQLStorage(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	defer os.Remove(path)

	now := time.Now().UTC()
	if err := st.SaveTradeWithExchangePnLAndMarketType(0, 1, "binance", "spot", "BTCUSDT", 100, 110, 1, 10, 10, 0, "USDT", 0, 0, now, "spot-bot"); err != nil {
		t.Fatal(err)
	}
	for _, trade := range []Trade{
		{Exchange: "binance", MarketType: "futures", Symbol: "BTCUSDT", Quantity: 2, PnL: 20, CreatedAt: now},
		{Exchange: "binance", Symbol: "BTCUSDT", Quantity: 3, PnL: 30, CreatedAt: now},
	} {
		if err := st.SaveTrade(&trade); err != nil {
			t.Fatal(err)
		}
	}

	rows, err := st.GetPnLByTimeRange("", now.Add(-time.Minute), now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 {
		t.Fatalf("want separate spot, futures and unknown rows, got %+v", rows)
	}
	got := make(map[string]float64, len(rows))
	for _, row := range rows {
		got[row.MarketType] = row.TotalPnL
	}
	if got["spot"] != 10 || got["futures"] != 20 || got["unknown"] != 30 {
		t.Fatalf("unexpected market PnL grouping: %#v", got)
	}
}

func TestGetPnLBySymbolRequiresExchangeMarketScopeWhenAmbiguous(t *testing.T) {
	st := newSQLStorageForTest(t)
	now := time.Now().UTC()
	for _, trade := range []Trade{
		{Account: "acct", MarketType: "spot", Symbol: "BTCUSDT", Quantity: 1, PnL: 10, Fee: 1, CreatedAt: now},
		{Account: "acct", MarketType: "futures", Symbol: "BTCUSDT", Quantity: 2, PnL: 20, Fee: 2, CreatedAt: now},
		{Account: "acct", Exchange: "okx", MarketType: "spot", Symbol: "BTCUSDT", Quantity: 3, PnL: 30, Fee: 3, CreatedAt: now},
		{Account: "other", MarketType: "margin", Symbol: "BTCUSDT", Quantity: 3, PnL: 30, Fee: 3, CreatedAt: now},
	} {
		if err := st.SaveTrade(&trade); err != nil {
			t.Fatal(err)
		}
	}
	start, end := now.Add(-time.Minute), now.Add(time.Minute)
	if _, err := st.GetPnLBySymbol("BTCUSDT", "acct", start, end); !errors.Is(err, ErrPnLScopeRequired) {
		t.Fatalf("ambiguous symbol PnL error=%v, want explicit exchange/market scope", err)
	}
	spot, err := st.GetPnLBySymbolScope("BTCUSDT", "acct", "binance", "spot", start, end)
	if err != nil {
		t.Fatal(err)
	}
	if spot.Exchange != "binance" || spot.MarketType != "spot" || spot.TotalPnL != 9 || spot.TotalTrades != 1 {
		t.Fatalf("spot PnL crossed market/account boundary: %+v", spot)
	}
	futures, err := st.GetPnLBySymbolScope("BTCUSDT", "acct", "binance", "futures", start, end)
	if err != nil {
		t.Fatal(err)
	}
	if futures.TotalPnL != 18 || futures.TotalTrades != 1 {
		t.Fatalf("futures PnL mismatch: %+v", futures)
	}
	okxSpot, err := st.GetPnLBySymbolScope("BTCUSDT", "acct", "okx", "spot", start, end)
	if err != nil {
		t.Fatal(err)
	}
	if okxSpot.TotalPnL != 27 || okxSpot.TotalTrades != 1 {
		t.Fatalf("other exchange spot PnL mismatch: %+v", okxSpot)
	}
}

func TestGetPnLBySymbolAccountScopeReadsLegacyAccountLabelWithoutCrossingScopes(t *testing.T) {
	st := newSQLStorageForTest(t)
	now := time.Now().UTC()
	for _, trade := range []Trade{
		{Account: "old-key-prefix", AccountScope: "scope-a", Exchange: "binance", MarketType: "futures", Symbol: "BTCUSDT", Quantity: 2, PnL: 20, Fee: 2, CreatedAt: now},
		{Account: "opaque-new-id", AccountScope: "scope-a", Exchange: "binance", MarketType: "futures", Symbol: "BTCUSDT", Quantity: 1, PnL: 10, Fee: 1, CreatedAt: now},
		{Account: "old-key-prefix", AccountScope: "scope-b", Exchange: "binance", MarketType: "futures", Symbol: "BTCUSDT", Quantity: 100, PnL: 1000, Fee: 100, CreatedAt: now},
	} {
		if err := st.SaveTrade(&trade); err != nil {
			t.Fatal(err)
		}
	}
	summary, err := st.GetPnLBySymbolAccountScope("BTCUSDT", "scope-a", "binance", "futures", now.Add(-time.Minute), now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if summary.TotalTrades != 2 || summary.TotalPnL != 27 || summary.TotalVolume != 3 {
		t.Fatalf("scope-filtered historical PnL = %+v, want two scoped rows totalling 27", summary)
	}
}

func TestGetPnLByAccountScopeAndAssetSeparatesAssetsAndCredentialScopes(t *testing.T) {
	st := newSQLStorageForTest(t)
	now := time.Now().UTC()
	trades := []Trade{
		{BuyOrderID: 1, SellOrderID: 101, Account: "old-label", AccountScope: "scope-a", Exchange: "binance", MarketType: "futures", PnLAsset: "USDT", FeeAsset: "USDT", Symbol: "BTCUSDT", Quantity: 1, PnL: 12, Fee: 2, ExchangePnL: 10, CreatedAt: now},
		{BuyOrderID: 2, SellOrderID: 102, Account: "opaque-label", AccountScope: "scope-a", Exchange: "binance", MarketType: "futures", PnLAsset: "USDT", FeeAsset: "USDT", Symbol: "ETHUSDT", Quantity: 2, PnL: 8, Fee: 1, ExchangePnL: 7, CreatedAt: now},
		{BuyOrderID: 3, SellOrderID: 103, Account: "opaque-label", AccountScope: "scope-a", Exchange: "binance", MarketType: "futures", PnLAsset: "USDC", FeeAsset: "USDC", Symbol: "BTCUSDC", Quantity: 3, PnL: 9, Fee: 1, ExchangePnL: 8, CreatedAt: now},
		{BuyOrderID: 4, SellOrderID: 104, Account: "foreign", AccountScope: "scope-b", Exchange: "binance", MarketType: "futures", PnLAsset: "USDT", FeeAsset: "USDT", Symbol: "BTCUSDT", Quantity: 100, PnL: 1000, Fee: 0, CreatedAt: now},
	}
	for i := range trades {
		if err := st.SaveTrade(&trades[i]); err != nil {
			t.Fatal(err)
		}
	}
	rows, err := st.GetPnLByAccountScopeAndAsset("BINANCE", "scope-a", "usdt", now.Add(-time.Minute), now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("USDT query returned wrong grouped rows: %+v", rows)
	}
	got := make(map[string]float64, len(rows))
	for _, row := range rows {
		if row.PnLAsset != "USDT" || row.Exchange != "binance" || row.MarketType != "futures" {
			t.Fatalf("scope/asset metadata mismatch: %+v", row)
		}
		got[row.Symbol] = row.TotalPnL
	}
	if got["BTCUSDT"] != 10 || got["ETHUSDT"] != 7 {
		t.Fatalf("unexpected scoped USDT PnL: %#v", got)
	}
	if _, err := st.GetPnLByAccountScopeAndAsset("binance", "scope-a", "BTC", now.Add(-time.Minute), now.Add(time.Minute)); err != nil {
		t.Fatalf("known non-requested asset rows should be safely excluded: %v", err)
	}
}

func TestGetPnLByAccountScopeAndAssetRejectsUnknownOwnershipAndFeeAsset(t *testing.T) {
	now := time.Now().UTC()
	tests := []struct {
		name   string
		trades []Trade
	}{
		{name: "unscoped exchange trade", trades: []Trade{{BuyOrderID: 1, SellOrderID: 11, Exchange: "binance", MarketType: "futures", PnLAsset: "USDT", FeeAsset: "USDT", Symbol: "BTCUSDT", PnL: 1, CreatedAt: now}}},
		{name: "unknown fee denomination", trades: []Trade{{BuyOrderID: 2, SellOrderID: 12, Exchange: "binance", AccountScope: "scope-a", MarketType: "futures", PnLAsset: "USDT", FeeAsset: "BNB", Symbol: "BTCUSDT", PnL: 1, Fee: 0.1, CreatedAt: now}}},
		{name: "unknown PnL denomination", trades: []Trade{{BuyOrderID: 3, SellOrderID: 13, Exchange: "binance", AccountScope: "scope-a", MarketType: "futures", FeeAsset: "USDT", Symbol: "BTCUSDT", PnL: 1, CreatedAt: now}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := newSQLStorageForTest(t)
			for i := range tt.trades {
				if err := st.SaveTrade(&tt.trades[i]); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := st.GetPnLByAccountScopeAndAsset("binance", "scope-a", "USDT", now.Add(-time.Minute), now.Add(time.Minute)); err == nil {
				t.Fatal("PnL query accepted incomplete ownership or fee denomination evidence")
			}
		})
	}
}

func TestQueryTradesByAccountScopeAndAssetFailsClosedOnIncompleteEvidence(t *testing.T) {
	now := time.Now().UTC()
	tests := []struct {
		name  string
		trade Trade
	}{
		{
			name:  "unattributed exchange trade",
			trade: Trade{Exchange: "binance", MarketType: "futures", PnLAsset: "USDT", FeeAsset: "USDT", Symbol: "BTCUSDT", CreatedAt: now},
		},
		{
			name:  "mismatched fee denomination",
			trade: Trade{Exchange: "binance", AccountScope: "scope-a", MarketType: "futures", PnLAsset: "USDT", FeeAsset: "BNB", Fee: 0.01, Symbol: "BTCUSDT", CreatedAt: now},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := newSQLStorageForTest(t)
			if err := st.SaveTrade(&tt.trade); err != nil {
				t.Fatal(err)
			}
			if _, err := st.QueryTradesByAccountScopeAndAsset("binance", "scope-a", "futures", "USDT", now.Add(-time.Minute), now.Add(time.Minute), 100); err == nil {
				t.Fatal("scoped diagnostic query accepted incomplete ownership/asset evidence")
			}
		})
	}
}

func TestMigrateTradesMarketTypePreservesLegacyRowsAndIsIdempotent(t *testing.T) {
	path := t.TempDir() + "/legacy-trades.db"
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE trades (id INTEGER PRIMARY KEY, symbol TEXT); INSERT INTO trades(symbol) VALUES ('BTCUSDT')`); err != nil {
		t.Fatal(err)
	}
	if err := migrateTradesMarketType(db); err != nil {
		t.Fatal(err)
	}
	if err := migrateTradesAccountScope(db); err != nil {
		t.Fatal(err)
	}
	if err := migrateTradesMarketType(db); err != nil {
		t.Fatalf("second migration should be idempotent: %v", err)
	}
	if err := migrateTradesAccountScope(db); err != nil {
		t.Fatalf("second account scope migration should be idempotent: %v", err)
	}
	var marketType string
	if err := db.QueryRow(`SELECT market_type FROM trades WHERE symbol = 'BTCUSDT'`).Scan(&marketType); err != nil {
		t.Fatal(err)
	}
	if marketType != "" {
		t.Fatalf("legacy market type must remain unclassified, got %q", marketType)
	}
	var accountScope string
	if err := db.QueryRow(`SELECT account_scope FROM trades WHERE symbol = 'BTCUSDT'`).Scan(&accountScope); err != nil {
		t.Fatal(err)
	}
	if accountScope != "" {
		t.Fatalf("legacy account scope must remain unattributed, got %q", accountScope)
	}
}

func TestReconciliationTradeQueriesAreMarketAndExchangeScoped(t *testing.T) {
	st, err := NewSQLStorage(t.TempDir() + "/reconciliation-market.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	now := time.Now().UTC()
	trades := []Trade{
		{Exchange: "binance", MarketType: "spot", Account: "acct", AccountScope: "scope-a", BotID: "bot", Symbol: "BTCUSDT", Quantity: 1, PnL: 10, Fee: 1, CreatedAt: now},
		{Exchange: "binance", MarketType: "futures", Account: "acct", AccountScope: "scope-a", BotID: "bot", Symbol: "BTCUSDT", Quantity: 2, PnL: 20, Fee: 2, CreatedAt: now},
		{Exchange: "okx", MarketType: "spot", Account: "acct", AccountScope: "scope-a", BotID: "bot", Symbol: "BTCUSDT", Quantity: 4, PnL: 40, Fee: 4, CreatedAt: now},
		{Exchange: "binance", MarketType: "spot", Account: "acct", AccountScope: "scope-b", BotID: "bot", Symbol: "BTCUSDT", Quantity: 100, PnL: 1000, Fee: 100, CreatedAt: now},
		{Exchange: "binance", MarketType: "", Account: "acct", AccountScope: "scope-a", BotID: "bot", Symbol: "BTCUSDT", Quantity: 8, PnL: 80, Fee: 8, CreatedAt: now},
		{Exchange: "binance", MarketType: "spot", Account: "acct", AccountScope: "scope-a", Symbol: "BTCUSDT", Quantity: 16, PnL: 160, Fee: 16, CreatedAt: now},
	}
	for i := range trades {
		if err := st.SaveTrade(&trades[i]); err != nil {
			t.Fatal(err)
		}
	}
	buy, sell, err := st.GetTotalBuySellQtyByMarketScope("binance", "spot", "BTCUSDT", "acct", "scope-a", "bot")
	if err != nil || buy != 1 || sell != 1 {
		t.Fatalf("spot quantities=(%v,%v), err=%v", buy, sell, err)
	}
	buy, sell, err = st.GetTotalBuySellQtyByMarketScope("binance", "spot", "BTCUSDT", "acct", "scope-b", "bot")
	if err != nil || buy != 100 || sell != 100 {
		t.Fatalf("other account-scope quantities=(%v,%v), err=%v", buy, sell, err)
	}
	profit, err := st.GetActualProfitBySymbolMarketScope("binance", "spot", "BTCUSDT", "acct", "scope-a", now.Add(time.Second), "bot")
	if err != nil || profit != 9 {
		t.Fatalf("spot actual profit=%v err=%v", profit, err)
	}
	profit, err = st.GetActualProfitBySymbolMarketScope("binance", "futures", "BTCUSDT", "acct", "scope-a", now.Add(time.Second), "bot")
	if err != nil || profit != 18 {
		t.Fatalf("futures actual profit=%v err=%v", profit, err)
	}
	legacy, err := st.HasUnclassifiedMarketTrades("binance", "BTCUSDT", "acct", "scope-a", "bot")
	if err != nil || !legacy {
		t.Fatalf("unclassified legacy row not detected: found=%v err=%v", legacy, err)
	}
	legacy, err = st.HasUnclassifiedMarketTrades("okx", "BTCUSDT", "acct", "scope-a", "bot")
	if err != nil || legacy {
		t.Fatalf("other exchange legacy attribution leaked: found=%v err=%v", legacy, err)
	}
	legacy, err = st.HasUnclassifiedMarketTrades("binance", "BTCUSDT", "acct", "scope-b", "bot")
	if err != nil || legacy {
		t.Fatalf("other account-scope legacy attribution leaked: found=%v err=%v", legacy, err)
	}
	legacy, err = st.HasUnclassifiedMarketTrades("binance", "BTCUSDT", "acct", "scope-a", "bot-with-no-legacy-id")
	if err != nil || !legacy {
		t.Fatalf("legacy row without bot identity was not detected: found=%v err=%v", legacy, err)
	}
}

func TestReconciliationHistoryPersistsAndQueriesExactScope(t *testing.T) {
	st, err := NewSQLStorage(t.TempDir() + "/reconciliation-history-scope.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	now := time.Now().UTC()
	for _, row := range []ReconciliationHistory{
		{Exchange: "binance", Symbol: "BTCUSDT", Account: "acct", AccountScope: "scope-a", MarketType: "spot", BotID: "bot-a", ReconcileTime: now, CreatedAt: now},
		{Exchange: "binance", Symbol: "BTCUSDT", Account: "acct", AccountScope: "scope-a", MarketType: "futures", BotID: "bot-a", ReconcileTime: now, CreatedAt: now},
		{Exchange: "binance", Symbol: "BTCUSDT", Account: "acct", AccountScope: "scope-b", MarketType: "spot", BotID: "bot-a", ReconcileTime: now, CreatedAt: now},
		{Exchange: "binance", Symbol: "BTCUSDT", Account: "acct", AccountScope: "scope-a", MarketType: "spot", BotID: "bot-b", ReconcileTime: now, CreatedAt: now},
		{Exchange: "binance", Symbol: "BTCUSDT", Account: "acct", ReconcileTime: now, CreatedAt: now}, // legacy: intentionally unclassified
	} {
		copy := row
		if err := st.SaveReconciliationHistory(&copy); err != nil {
			t.Fatal(err)
		}
	}
	got, err := st.QueryReconciliationHistoryByScope("binance", "BTCUSDT", "acct", "spot", "scope-a", "bot-a", now.Add(-time.Second), now.Add(time.Second), 20, 0)
	if err != nil || len(got) != 1 {
		t.Fatalf("scoped history rows=%d err=%v", len(got), err)
	}
	if got[0].MarketType != "spot" || got[0].AccountScope != "scope-a" || got[0].BotID != "bot-a" {
		t.Fatalf("scoped identity was not persisted: %+v", got[0])
	}
	count, err := st.GetReconciliationCountByScope("binance", "BTCUSDT", "acct", "spot", "scope-a", "bot-a")
	if err != nil || count != 1 {
		t.Fatalf("scoped reconciliation count=%d err=%v", count, err)
	}
	unscoped, err := st.HasUnscopedReconciliationHistory("binance", "BTCUSDT", "acct")
	if err != nil || !unscoped {
		t.Fatalf("legacy history not detected: found=%v err=%v", unscoped, err)
	}
	if err := migrateReconciliationHistory(st.db); err != nil {
		t.Fatalf("scope migration must be idempotent: %v", err)
	}
}

func TestQueryDailyPnLTradesAppliesAllDimensionsBeforeAggregatingAndRanking(t *testing.T) {
	path := t.TempDir() + "/daily-pnl-scope.db"
	st, err := NewSQLStorage(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	defer os.Remove(path)

	day := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	trades := []Trade{
		{Exchange: "binance", MarketType: "spot", Symbol: "BTCUSDT", Account: "acct", AccountScope: "scope-a", BotID: "bot-a", PnL: 10, Fee: 1, CreatedAt: day},
		{Exchange: "binance", MarketType: "spot", Symbol: "BTCUSDT", Account: "acct", AccountScope: "scope-a", BotID: "bot-a", PnL: -3, Fee: 0.5, CreatedAt: day},
		{Exchange: "binance", MarketType: "futures", Symbol: "BTCUSDT", Account: "acct", AccountScope: "scope-a", BotID: "bot-a", PnL: 100, Fee: 10, CreatedAt: day},
		{Exchange: "binance", MarketType: "spot", Symbol: "ETHUSDT", Account: "acct", AccountScope: "scope-a", BotID: "bot-a", PnL: 200, Fee: 20, CreatedAt: day},
		{Exchange: "binance", MarketType: "spot", Symbol: "BTCUSDT", Account: "acct", AccountScope: "scope-b", BotID: "bot-a", PnL: 300, Fee: 30, CreatedAt: day},
		{Exchange: "binance", MarketType: "spot", Symbol: "BTCUSDT", Account: "acct", AccountScope: "scope-a", BotID: "bot-b", PnL: 400, Fee: 40, CreatedAt: day},
		{Exchange: "binance", Symbol: "BTCUSDT", Account: "acct", BotID: "bot-a", PnL: 500, Fee: 50, CreatedAt: day},
	}
	for i := range trades {
		if err := st.SaveTrade(&trades[i]); err != nil {
			t.Fatal(err)
		}
	}

	count, pnl, fees, winners, losers, err := st.QueryDailyPnLTrades("binance", "spot", "BTCUSDT", "acct", "scope-a", "bot-a", day.Truncate(24*time.Hour), day.Truncate(24*time.Hour).Add(24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 || pnl != 7 || fees != 1.5 {
		t.Fatalf("unexpected scoped summary: count=%d pnl=%v fees=%v", count, pnl, fees)
	}
	if len(winners) != 1 || winners[0].PnL != 10 || len(losers) != 1 || losers[0].PnL != -3 {
		t.Fatalf("unexpected scoped extremes: winners=%+v losers=%+v", winners, losers)
	}
}
