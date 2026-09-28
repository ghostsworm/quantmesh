package storage

import (
	"os"
	"testing"
	"time"
)

func TestSaveOrderFillIsIdempotentAndRejectsConflictingReplay(t *testing.T) {
	path := t.TempDir() + "/fills.db"
	st, err := NewSQLStorage(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	defer os.Remove(path)

	pnl := 0.25
	fill := &OrderFill{Exchange: "binance", MarketType: "futures", AccountScope: "scope-a", Account: "acct", Symbol: "BTCUSDT", TradeID: "trade-1", OrderID: 42, Side: "BUY", Price: 100, Quantity: 0.5, Commission: 0.01, CommissionAsset: "USDT", RealizedPnL: &pnl, TradeTime: time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)}
	if err := st.SaveOrderFill(fill); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveOrderFill(fill); err != nil {
		t.Fatalf("identical replay should be idempotent: %v", err)
	}
	enriched := *fill
	enriched.QuoteQuantity = 50
	if err := st.SaveOrderFill(&enriched); err != nil {
		t.Fatalf("replay with authoritative quote amount should enrich legacy row: %v", err)
	}
	var storedQuoteQuantity float64
	if err := st.db.QueryRow(`SELECT quote_quantity FROM order_fills WHERE account_scope = ?`, "scope-a").Scan(&storedQuoteQuantity); err != nil {
		t.Fatal(err)
	}
	if storedQuoteQuantity != 50 {
		t.Fatalf("expected authoritative quote amount to enrich legacy row, got %v", storedQuoteQuantity)
	}
	conflict := *fill
	conflict.Quantity = 0.6
	if err := st.SaveOrderFill(&conflict); err == nil {
		t.Fatal("conflicting economic replay must fail")
	}
	var count int
	if err := st.db.QueryRow(`SELECT COUNT(*) FROM order_fills WHERE account_scope = ?`, "scope-a").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("expected exactly one persisted execution, got %d", count)
	}
}

func TestSaveOrderFillRequiresCompleteIdentityAndFiniteEconomics(t *testing.T) {
	st, err := NewSQLStorage(t.TempDir() + "/fills.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	base := &OrderFill{Exchange: "binance", MarketType: "futures", AccountScope: "scope", Symbol: "BTCUSDT", TradeID: "1", OrderID: 1, Side: "BUY", Price: 1, Quantity: 1, TradeTime: time.Now()}
	for _, mutate := range []func(*OrderFill){func(f *OrderFill) { f.AccountScope = "" }, func(f *OrderFill) { f.TradeID = "" }, func(f *OrderFill) { f.Price = 0 }, func(f *OrderFill) { f.Quantity = -1 }, func(f *OrderFill) { f.TradeTime = time.Time{} }} {
		candidate := *base
		mutate(&candidate)
		if err := st.SaveOrderFill(&candidate); err == nil {
			t.Fatalf("invalid fill accepted: %+v", candidate)
		}
	}
}

func TestSaveOrderFillAcceptsFiniteNegativeCommissionRebate(t *testing.T) {
	st, err := NewSQLStorage(t.TempDir() + "/commission-rebate.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	fill := &OrderFill{Exchange: "bybit", MarketType: "spot", AccountScope: "scope", Symbol: "BTCUSDT", TradeID: "rebate-1", OrderID: 7, Side: "BUY", Price: 100, Quantity: 1, Commission: -0.002, CommissionAsset: "USDT", TradeTime: time.Now()}
	if err := st.SaveOrderFill(fill); err != nil {
		t.Fatalf("finite negative commission is a valid exchange rebate: %v", err)
	}
	var fee float64
	if err := st.db.QueryRow(`SELECT commission FROM order_fills WHERE trade_id = ?`, fill.TradeID).Scan(&fee); err != nil {
		t.Fatal(err)
	}
	if fee != -0.002 {
		t.Fatalf("rebate sign must be preserved, got %v", fee)
	}
}

func TestSaveOrderFillCanEnrichButNeverEraseAttribution(t *testing.T) {
	st, err := NewSQLStorage(t.TempDir() + "/fills-attribution.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	fill := &OrderFill{Exchange: "binance", MarketType: "futures", AccountScope: "scope", Symbol: "BTCUSDT", TradeID: "same", OrderID: 9, Side: "BUY", Price: 2, Quantity: 3, TradeTime: time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)}
	if err := st.SaveOrderFill(fill); err != nil {
		t.Fatal(err)
	}
	pnl := 0.0
	botFill := *fill
	botFill.Account, botFill.BotID, botFill.RealizedPnL = "acct", "bot-1", &pnl
	if err := st.SaveOrderFill(&botFill); err != nil {
		t.Fatalf("authoritative Bot attribution should enrich an earlier account-level fill: %v", err)
	}
	if err := st.SaveOrderFill(fill); err != nil {
		t.Fatalf("an account-level replay must not erase stronger attribution: %v", err)
	}
	var account, bot string
	var storedPnL float64
	if err := st.db.QueryRow(`SELECT account, bot_id, realized_pnl FROM order_fills WHERE trade_id = 'same'`).Scan(&account, &bot, &storedPnL); err != nil {
		t.Fatal(err)
	}
	if account != "acct" || bot != "bot-1" || storedPnL != 0 {
		t.Fatalf("attribution enrichment was not retained: account=%q bot=%q pnl=%v", account, bot, storedPnL)
	}
}

func TestQueryDailyOrderFillsByScopeAggregatesExecutionsAndFeeAssets(t *testing.T) {
	st, err := NewSQLStorage(t.TempDir() + "/daily-fill-summary.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	day := time.Date(2026, 9, 26, 0, 0, 0, 0, time.UTC)
	realized := 4.25
	fills := []*OrderFill{
		{Exchange: "binance", MarketType: "futures", AccountScope: "scope-a", Account: "acct", Symbol: "BTCUSDT", TradeID: "buy-1", OrderID: 10, Side: "BUY", Price: 100, Quantity: 0.4, QuoteQuantity: 40.00000001, Commission: 0.04, CommissionAsset: "USDT", TradeTime: day.Add(time.Hour)},
		{Exchange: "binance", MarketType: "futures", AccountScope: "scope-a", Account: "acct", Symbol: "BTCUSDT", TradeID: "sell-1", OrderID: 11, Side: "SELL", Price: 110, Quantity: 0.3, Commission: 0.02, CommissionAsset: "usdt", RealizedPnL: &realized, TradeTime: day.Add(2 * time.Hour)},
		{Exchange: "binance", MarketType: "futures", AccountScope: "scope-a", Account: "acct", Symbol: "BTCUSDT", TradeID: "buy-2", OrderID: 12, Side: "BUY", Price: 99, Quantity: 1, Commission: 0.001, CommissionAsset: "BNB", TradeTime: day.Add(3 * time.Hour)},
		{Exchange: "binance", MarketType: "futures", AccountScope: "scope-b", Account: "acct", Symbol: "BTCUSDT", TradeID: "other-scope", OrderID: 13, Side: "SELL", Price: 1000, Quantity: 2, Commission: 9, CommissionAsset: "USDT", TradeTime: day.Add(time.Hour)},
		{Exchange: "binance", MarketType: "futures", AccountScope: "scope-a", Account: "other-account", Symbol: "BTCUSDT", TradeID: "other-account", OrderID: 14, Side: "SELL", Price: 1000, Quantity: 2, Commission: 9, CommissionAsset: "USDT", TradeTime: day.Add(time.Hour)},
	}
	for _, fill := range fills {
		if err := st.SaveOrderFill(fill); err != nil {
			t.Fatal(err)
		}
	}
	got, err := st.QueryDailyOrderFillsByScope("acct", "binance", "futures", "BTCUSDT", "scope-a", day, day.Add(24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if got.BuyOrders != 2 || got.SellOrders != 1 || got.BuyQty != 1.4 || got.SellQty != 0.3 || got.BuyValue != 139.00000001 || got.SellValue != 33 || got.RealizedPnL != realized || got.FillCount != 3 {
		t.Fatalf("unexpected execution aggregate: %+v", got)
	}
	if got.FeesByAsset["USDT"] != 0.06 || got.FeesByAsset["BNB"] != 0.001 {
		t.Fatalf("fees must remain separated by currency and scoped account: %+v", got.FeesByAsset)
	}
	if got.FeeQuoteValueByAsset["BNB"] != 0.099 {
		t.Fatalf("execution-price fee valuation should be retained per asset: %+v", got.FeeQuoteValueByAsset)
	}
	winners, losers, err := st.QueryTopDailyRealizedFills("acct", "binance", "futures", "BTCUSDT", "scope-a", day, day.Add(24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(winners) != 1 || len(losers) != 0 || winners[0].OrderID != 11 || winners[0].FilledQty != 0.3 || winners[0].Price != 110 || winners[0].RealizedPnL == nil || *winners[0].RealizedPnL != realized {
		t.Fatalf("realized order ranking must aggregate individual fills: winners=%+v losers=%+v", winners, losers)
	}
}
