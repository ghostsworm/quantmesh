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

func TestListOrderFillStreamsFindsRawStreamsWithoutPairedTrades(t *testing.T) {
	st, err := NewSQLStorage(t.TempDir() + "/raw-streams.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	for _, fill := range []*OrderFill{
		{Exchange: "binance", MarketType: "spot", AccountScope: "scope-a", Symbol: "ETHUSDT", TradeID: "raw-only", OrderID: 17, Side: "BUY", Price: 100, Quantity: 1, TradeTime: time.Now().UTC()},
		{Exchange: "binance", MarketType: "futures", AccountScope: "scope-a", Symbol: "BTCUSDT", TradeID: "configured-away", OrderID: 18, Side: "SELL", Price: 100, Quantity: 1, TradeTime: time.Now().UTC()},
	} {
		if err := st.SaveOrderFill(fill); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := st.db.Exec(`INSERT INTO order_fills (exchange, market_type, account_scope, symbol, trade_id, order_id, side, price, quantity, trade_time) VALUES ('binance', 'spot', 'scope-a', '', 'invalid-symbol', 19, 'BUY', 100, 1, CURRENT_TIMESTAMP)`); err != nil {
		t.Fatal(err)
	}
	streams, invalid, err := st.ListOrderFillStreamsByAccountScope("binance", "scope-a")
	if err != nil {
		t.Fatal(err)
	}
	if invalid != 1 || len(streams) != 2 {
		t.Fatalf("raw stream inventory missed an orphan or invalid identity: streams=%+v invalid=%d", streams, invalid)
	}
	if streams[0].Exchange != "binance" || streams[0].MarketType != "futures" || streams[0].Symbol != "BTCUSDT" {
		t.Fatalf("raw stream identity was not normalized: %+v", streams[0])
	}
}

func TestListAccountExecutionScopesFindsUnarchivedAndUnattributedHistory(t *testing.T) {
	st, err := NewSQLStorage(t.TempDir() + "/execution-scopes.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	now := time.Now().UTC()
	if err := st.SaveOrderFill(&OrderFill{Exchange: "binance", MarketType: "spot", AccountScope: "current-scope", Symbol: "BTCUSDT", TradeID: "current", OrderID: 11, Side: "BUY", Price: 100, Quantity: 1, TradeTime: now}); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveTrade(&Trade{BuyOrderID: 21, SellOrderID: 22, Exchange: "binance", MarketType: "futures", PnLAsset: "USDT", AccountScope: "unarchived-old-scope", Symbol: "ETHUSDT", Quantity: 1, PnL: 1, CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.Exec(`INSERT INTO order_fills (exchange, market_type, account_scope, symbol, trade_id, order_id, side, price, quantity, trade_time) VALUES ('binance', 'spot', '', 'SOLUSDT', 'legacy-unattributed', 31, 'BUY', 100, 1, ?)`, now); err != nil {
		t.Fatal(err)
	}
	scopes, invalid, err := st.ListAccountExecutionScopesByExchange("binance")
	if err != nil || invalid != 1 {
		t.Fatalf("execution scope inventory failed to flag legacy attribution: scopes=%+v invalid=%d err=%v", scopes, invalid, err)
	}
	found := map[string]bool{}
	for _, scope := range scopes {
		found[scope.Exchange+"/"+scope.Scope] = true
	}
	if !found["binance/current-scope"] || !found["binance/unarchived-old-scope"] {
		t.Fatalf("execution scope inventory omitted durable history: %+v", scopes)
	}
}

func TestOrderFillLedgerIntegrityRequiresPairingValuationAndFeeAgreement(t *testing.T) {
	st, err := NewSQLStorage(t.TempDir() + "/fill-integrity.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	now := time.Now().UTC()
	for _, fill := range []*OrderFill{
		{Exchange: "binance", MarketType: "futures", AccountScope: "scope-a", Symbol: "BTCUSDT", TradeID: "buy-fill", OrderID: 101, Side: "BUY", Price: 100, Quantity: 1, Commission: 0.05, CommissionAsset: "USDT", TradeTime: now},
		{Exchange: "binance", MarketType: "futures", AccountScope: "scope-a", Symbol: "BTCUSDT", TradeID: "sell-fill", OrderID: 102, Side: "SELL", Price: 101, Quantity: 1, Commission: 0.05, CommissionAsset: "USDT", TradeTime: now},
	} {
		if err := st.SaveOrderFill(fill); err != nil {
			t.Fatal(err)
		}
	}
	trade := &Trade{BuyOrderID: 101, SellOrderID: 102, Exchange: "binance", MarketType: "futures", PnLAsset: "USDT", AccountScope: "scope-a", Symbol: "BTCUSDT", Quantity: 1, PnL: 1, ExchangePnL: 1, Fee: 0.1, FeeAsset: "USDT", CreatedAt: now}
	if err := st.SaveTrade(trade); err != nil {
		t.Fatal(err)
	}
	got, err := st.GetOrderFillLedgerIntegrity("binance", "futures", "BTCUSDT", "scope-a", "USDT")
	if err != nil || got.UnpairedOrderCount != 0 || got.UnvaluedFeeCount != 0 || !got.FeeTotalsMatch {
		t.Fatalf("consistent raw and paired fee ledgers rejected: %+v err=%v", got, err)
	}
	if got.ExecutionMismatchOrderCount != 0 {
		t.Fatalf("matching side and quantity should pass execution reconciliation: %+v", got)
	}
	if _, err := st.db.Exec(`UPDATE order_fills SET quantity = 0.5 WHERE trade_id = 'buy-fill'`); err != nil {
		t.Fatal(err)
	}
	got, err = st.GetOrderFillLedgerIntegrity("binance", "futures", "BTCUSDT", "scope-a", "USDT")
	if err != nil || got.ExecutionMismatchOrderCount == 0 {
		t.Fatalf("partial raw quantity must not match a full trade leg: %+v err=%v", got, err)
	}
	if _, err := st.db.Exec(`UPDATE order_fills SET quantity = 1, side = 'SELL' WHERE trade_id = 'buy-fill'`); err != nil {
		t.Fatal(err)
	}
	got, err = st.GetOrderFillLedgerIntegrity("binance", "futures", "BTCUSDT", "scope-a", "USDT")
	if err != nil || got.ExecutionMismatchOrderCount == 0 {
		t.Fatalf("side mismatch must not pass execution reconciliation: %+v err=%v", got, err)
	}
	if _, err := st.db.Exec(`UPDATE order_fills SET side = 'BUY' WHERE trade_id = 'buy-fill'`); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveTrade(&Trade{BuyOrderID: 103, SellOrderID: 104, Exchange: "binance", MarketType: "futures", PnLAsset: "USDT", AccountScope: "scope-a", Symbol: "BTCUSDT", Quantity: 1, PnL: 0, ExchangePnL: 0, Fee: 0, FeeAsset: "USDT", CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	got, err = st.GetOrderFillLedgerIntegrity("binance", "futures", "BTCUSDT", "scope-a", "USDT")
	if err != nil || got.MissingFillOrderCount != 2 {
		t.Fatalf("paired trades missing raw fills were not detected: %+v err=%v", got, err)
	}
	if _, err := st.db.Exec(`DELETE FROM trades WHERE buy_order_id = 103`); err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.Exec(`UPDATE trades SET fee = 0.11 WHERE account_scope = 'scope-a'`); err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.Exec(`UPDATE order_fills SET commission_asset = 'BNB' WHERE trade_id = 'buy-fill'`); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveOrderFill(&OrderFill{Exchange: "binance", MarketType: "futures", AccountScope: "scope-a", Symbol: "BTCUSDT", TradeID: "open-fill", OrderID: 103, Side: "BUY", Price: 100, Quantity: 0.1, TradeTime: now}); err != nil {
		t.Fatal(err)
	}
	got, err = st.GetOrderFillLedgerIntegrity("binance", "futures", "BTCUSDT", "scope-a", "USDT")
	if err != nil || got.UnpairedOrderCount != 1 || got.UnvaluedFeeCount != 1 || got.FeeTotalsMatch {
		t.Fatalf("unpaired, unvalued or mismatched ledger was not rejected: %+v err=%v", got, err)
	}
	if err := st.SaveTrade(&Trade{Exchange: "binance", MarketType: "futures", PnLAsset: "USDT", AccountScope: "scope-a", Symbol: "BTCUSDT", Quantity: 1, PnL: 1, CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	got, err = st.GetOrderFillLedgerIntegrity("binance", "futures", "BTCUSDT", "scope-a", "USDT")
	if err != nil || got.InvalidTradeOrderCount != 1 {
		t.Fatalf("trade without stable buy/sell order identities must be rejected: %+v err=%v", got, err)
	}
}

func TestSaveOrderFillsAtomicRollsBackEarlierRowsOnConflict(t *testing.T) {
	st, err := NewSQLStorage(t.TempDir() + "/atomic-fills.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	tradeTime := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	existing := &OrderFill{Exchange: "binance", MarketType: "futures", AccountScope: "atomic-scope", Symbol: "BTCUSDT", TradeID: "existing", OrderID: 42, Side: "BUY", Price: 100, Quantity: 1, TradeTime: tradeTime}
	if err := st.SaveOrderFill(existing); err != nil {
		t.Fatal(err)
	}
	first := *existing
	first.TradeID, first.OrderID = "new-first", 43
	conflict := *existing
	conflict.Quantity = 2
	if err := st.SaveOrderFillsAtomic([]*OrderFill{&first, &conflict}); err == nil {
		t.Fatal("atomic batch accepted a conflicting existing execution")
	}
	var firstCount, totalCount int
	if err := st.db.QueryRow(`SELECT COUNT(*) FROM order_fills WHERE account_scope = ? AND trade_id = ?`, "atomic-scope", first.TradeID).Scan(&firstCount); err != nil {
		t.Fatal(err)
	}
	if err := st.db.QueryRow(`SELECT COUNT(*) FROM order_fills WHERE account_scope = ?`, "atomic-scope").Scan(&totalCount); err != nil {
		t.Fatal(err)
	}
	if firstCount != 0 || totalCount != 1 {
		t.Fatalf("partial batch escaped rollback: first=%d total=%d", firstCount, totalCount)
	}
}

func TestMigrateOrderFillsAddsPnLAssetWithoutGuessingLegacyRows(t *testing.T) {
	st, err := NewSQLStorage(t.TempDir() + "/legacy-pnl-asset.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	pnl := 4.0
	fill := &OrderFill{Exchange: "binance", MarketType: "futures", AccountScope: "legacy-scope", Symbol: "BTCUSDT", TradeID: "legacy-exec", OrderID: 77, Side: "SELL", Price: 100, Quantity: 1, RealizedPnL: &pnl, RealizedPnLAsset: "USDT", TradeTime: time.Now().UTC()}
	if err := st.SaveOrderFill(fill); err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.Exec(`ALTER TABLE order_fills DROP COLUMN realized_pnl_asset`); err != nil {
		t.Fatalf("prepare pre-rc473 schema: %v", err)
	}
	if err := migrateOrderFillsTable(st.db, false); err != nil {
		t.Fatalf("migrate legacy executions: %v", err)
	}
	var storedPnL float64
	var storedAsset string
	if err := st.db.QueryRow(`SELECT realized_pnl, realized_pnl_asset FROM order_fills WHERE trade_id = 'legacy-exec'`).Scan(&storedPnL, &storedAsset); err != nil {
		t.Fatal(err)
	}
	if storedPnL != pnl || storedAsset != "" {
		t.Fatalf("legacy execution migration guessed or lost data: pnl=%v asset=%q", storedPnL, storedAsset)
	}
}

func TestSaveOrderFillRequiresCompleteIdentityAndFiniteEconomics(t *testing.T) {
	st, err := NewSQLStorage(t.TempDir() + "/fills.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	base := &OrderFill{Exchange: "binance", MarketType: "futures", AccountScope: "scope", Symbol: "BTCUSDT", TradeID: "1", OrderID: 1, Side: "BUY", Price: 1, Quantity: 1, TradeTime: time.Now()}
	for _, mutate := range []func(*OrderFill){func(f *OrderFill) { f.AccountScope = "" }, func(f *OrderFill) { f.TradeID = "" }, func(f *OrderFill) { f.Side = "BID" }, func(f *OrderFill) { f.Side = "buy" }, func(f *OrderFill) { f.Price = 0 }, func(f *OrderFill) { f.Quantity = -1 }, func(f *OrderFill) { f.TradeTime = time.Time{} }} {
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
		{Exchange: "binance", MarketType: "futures", AccountScope: "scope-a", Account: "acct", Symbol: "BTCUSDT", TradeID: "sell-1", OrderID: 11, Side: "SELL", Price: 110, Quantity: 0.3, Commission: 0.02, CommissionAsset: "usdt", RealizedPnL: &realized, RealizedPnLAsset: "usdt", TradeTime: day.Add(2 * time.Hour)},
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
	if got.RealizedPnLByAsset["USDT"] != realized || got.RealizedPnLUnknownAssetCount != 0 {
		t.Fatalf("realized pnl denomination should remain explicit: %+v", got)
	}
	winners, losers, err := st.QueryTopDailyRealizedFills("acct", "binance", "futures", "BTCUSDT", "scope-a", day, day.Add(24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(winners) != 1 || len(losers) != 0 || winners[0].OrderID != 11 || winners[0].FilledQty != 0.3 || winners[0].Price != 110 || winners[0].RealizedPnL == nil || *winners[0].RealizedPnL != realized {
		t.Fatalf("realized order ranking must aggregate individual fills: winners=%+v losers=%+v", winners, losers)
	}
}

func TestQueryDailyOrderFillsByScopeTracksUnknownRealizedPnLAsset(t *testing.T) {
	st, err := NewSQLStorage(t.TempDir() + "/daily-fill-pnl-asset.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	day := time.Date(2026, 9, 26, 0, 0, 0, 0, time.UTC)
	unknownPnL, knownPnL := 1.25, 2.5
	for _, fill := range []*OrderFill{
		{Exchange: "binance", MarketType: "futures", AccountScope: "scope-a", Account: "acct", Symbol: "BTCUSDT", TradeID: "unknown-asset", OrderID: 20, Side: "SELL", Price: 100, Quantity: 1, RealizedPnL: &unknownPnL, TradeTime: day.Add(time.Hour)},
		{Exchange: "binance", MarketType: "futures", AccountScope: "scope-a", Account: "acct", Symbol: "BTCUSDT", TradeID: "known-other-asset", OrderID: 21, Side: "SELL", Price: 100, Quantity: 1, RealizedPnL: &knownPnL, RealizedPnLAsset: "USDC", TradeTime: day.Add(2 * time.Hour)},
	} {
		if err := st.SaveOrderFill(fill); err != nil {
			t.Fatal(err)
		}
	}
	got, err := st.QueryDailyOrderFillsByScope("acct", "binance", "futures", "BTCUSDT", "scope-a", day, day.Add(24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if got.RealizedPnLUnknownAssetCount != 1 || got.RealizedPnLByAsset["USDC"] != knownPnL || got.RealizedPnL != unknownPnL+knownPnL {
		t.Fatalf("daily realized pnl must retain unknown and mixed denomination evidence: %+v", got)
	}
}
