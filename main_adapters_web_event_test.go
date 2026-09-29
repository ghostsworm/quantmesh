package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"quantmesh/config"
	"quantmesh/execution"
	"quantmesh/position"
	"quantmesh/storage"
)

func TestTradeStorageAdapterSaveEvent_Unavailable(t *testing.T) {
	a := &tradeStorageAdapter{botID: "bot-1"}
	if err := a.SaveEvent("trade_fee_correction", map[string]interface{}{"fee": 1.0}); err == nil {
		t.Fatal("nil storage service must return an error so the correction is not silently dropped")
	}

	disabled := &config.Config{}
	ss, err := storage.NewStorageService(disabled, context.Background())
	if err != nil {
		t.Fatalf("NewStorageService(disabled): %v", err)
	}
	a.storageService = ss
	if err := a.SaveEvent("trade_fee_correction", map[string]interface{}{"fee": 1.0}); err == nil {
		t.Fatal("disabled storage must return an error")
	}
	if err := a.SaveTrade(1, 2, "binance", "BTCUSDT", 100, 101, 1, 1, 0, "USDT", time.Now(), "bot-1"); err == nil {
		t.Fatal("legacy SaveTrade must not report success without storage")
	}
	if err := a.SaveTradeWithExchangePnL(1, 2, "binance", "BTCUSDT", 100, 101, 1, 1, 1, 0, "USDT", 0, 0, time.Now(), "bot-1"); err == nil {
		t.Fatal("SaveTradeWithExchangePnL must not report success without storage")
	}
}

func TestTradeStorageAdapterPersistsOwnerScopedFeeCorrection(t *testing.T) {
	cfg := &config.Config{}
	cfg.Storage.Enabled = true
	cfg.Storage.Type = "sqlite"
	cfg.Storage.Path = filepath.Join(t.TempDir(), "adapter-fee-corrections.db")
	cfg.Storage.BufferSize = 1
	cfg.Storage.BatchSize = 1
	ss, err := storage.NewStorageService(cfg, context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer ss.GetStorage().Close()

	adapter := &tradeStorageAdapter{storageService: ss, accountID: "acct", accountScope: "credential-a", botID: "bot-a", marketType: "futures"}
	correction := &storage.TradeFeeCorrection{Exchange: "BINANCE", Symbol: "btcusdt", OrderID: 919, Leg: "close", Side: "SELL", Fee: 0.5, FeeAsset: "USDT", Reason: "late REST supplement"}
	firstID, err := adapter.SaveTradeFeeCorrection(correction)
	if err != nil {
		t.Fatalf("persist scoped fee correction: %v", err)
	}
	secondID, err := adapter.SaveTradeFeeCorrection(correction)
	if err != nil || secondID != firstID {
		t.Fatalf("same correction replay id=%q err=%v, want stable id %q", secondID, err, firstID)
	}
	otherOwner := *adapter
	otherOwner.accountScope = "credential-b"
	otherID, err := otherOwner.SaveTradeFeeCorrection(correction)
	if err != nil || otherID == firstID {
		t.Fatalf("different credential scope id=%q err=%v, want distinct identity", otherID, err)
	}
	count, err := adapter.CountPendingTradeFeeCorrections("binance", "BTCUSDT")
	if err != nil || count != 1 {
		t.Fatalf("adapter pending count=%d err=%v, want one row in exact owner scope", count, err)
	}
}

func TestRestorePendingTradeFeeCorrectionHoldFailsClosed(t *testing.T) {
	for _, test := range []struct {
		name      string
		pending   int
		lookupErr error
		wantBlock bool
	}{
		{name: "no corrections", wantBlock: false},
		{name: "pending correction", pending: 1, wantBlock: true},
		{name: "lookup failure", lookupErr: context.DeadlineExceeded, wantBlock: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			gate := &execution.OpeningGate{}
			if got := restorePendingTradeFeeCorrectionHold(gate, test.pending, test.lookupErr); got != test.wantBlock {
				t.Fatalf("restore hold returned %v, want %v", got, test.wantBlock)
			}
			if gate.HasBlock(tradeFeeCorrectionOpeningBlock) != test.wantBlock {
				t.Fatalf("hold state=%v, want %v", gate.HasBlock(tradeFeeCorrectionOpeningBlock), test.wantBlock)
			}
		})
	}
}

func TestTradeStorageAdapterSaveEvent_PersistsWithBotID(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "events.db")
	cfg := &config.Config{}
	cfg.Storage.Enabled = true
	cfg.Storage.Type = "sqlite"
	cfg.Storage.Path = dbPath
	cfg.Storage.BufferSize = 10
	cfg.Storage.BatchSize = 10

	ss, err := storage.NewStorageService(cfg, context.Background())
	if err != nil {
		t.Fatalf("NewStorageService: %v", err)
	}
	t.Cleanup(func() { _ = ss.GetStorage().Close() })

	a := &tradeStorageAdapter{storageService: ss, botID: "bot-42"}
	input := map[string]interface{}{"order_id": float64(7), "fee": 0.12}
	if err := a.SaveEvent("trade_fee_correction", input); err != nil {
		t.Fatalf("SaveEvent: %v", err)
	}
	if _, ok := input["bot_id"]; ok {
		t.Fatal("SaveEvent must not mutate the caller's map")
	}

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	var raw string
	if err := db.QueryRow(`SELECT data FROM events WHERE event_type = ?`, "trade_fee_correction").Scan(&raw); err != nil {
		t.Fatalf("query event: %v", err)
	}
	var got map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("unmarshal event: %v", err)
	}
	if got["bot_id"] != "bot-42" || got["fee"] != 0.12 {
		t.Fatalf("unexpected persisted event: %v", got)
	}
}

func TestTradeStorageAdapterPersistsMarketType(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "market-trades.db")
	cfg := &config.Config{}
	cfg.Storage.Enabled = true
	cfg.Storage.Type = "sqlite"
	cfg.Storage.Path = dbPath
	cfg.Storage.BufferSize = 10
	cfg.Storage.BatchSize = 10
	ss, err := storage.NewStorageService(cfg, context.Background())
	if err != nil {
		t.Fatalf("NewStorageService: %v", err)
	}
	t.Cleanup(func() { _ = ss.GetStorage().Close() })
	a := &tradeStorageAdapter{storageService: ss, botID: "bot-spot", accountID: "acct-1", accountScope: "scope-immutable", pnlAsset: "USDT", marketType: "spot"}
	trade := &storage.Trade{ExecutionKey: "adapter-execution-1", BuyOrderID: 1, SellOrderID: 2, Exchange: "binance", MarketType: "SPOT", PnLAsset: "USDT", Symbol: "BTCUSDT", BuyPrice: 100, SellPrice: 110, Quantity: 1, PnL: 10, ExchangePnL: 10, FeeAsset: "USDT", CreatedAt: time.Now()}
	if err := a.SaveTradeIdempotent(trade); err != nil {
		t.Fatalf("SaveTradeIdempotent: %v", err)
	}
	if err := a.SaveTradeIdempotent(trade); err != nil {
		t.Fatalf("SaveTradeWithExchangePnLAndMarketType: %v", err)
	}

	var marketType, botID, account, accountScope, pnlAsset string
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.QueryRow(`SELECT market_type, bot_id, account, account_scope, pnl_asset FROM trades WHERE sell_order_id = 2`).Scan(&marketType, &botID, &account, &accountScope, &pnlAsset); err != nil {
		t.Fatal(err)
	}
	if marketType != "spot" || botID != "bot-spot" || account != "acct-1" || accountScope != "scope-immutable" || pnlAsset != "USDT" {
		t.Fatalf("trade identity or PnL denomination was not preserved: market=%q bot=%q account=%q scope=%q pnl_asset=%q", marketType, botID, account, accountScope, pnlAsset)
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM trades WHERE execution_key = ?`, trade.ExecutionKey).Scan(&count); err != nil || count != 1 {
		t.Fatalf("adapter retry duplicated trade: count=%d err=%v", count, err)
	}
}

func TestTradeStorageAdapterReplaysScopedGridLedgerPayloadIdempotently(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "trade-ledger-replay.db")
	cfg := &config.Config{}
	cfg.Storage.Enabled = true
	cfg.Storage.Type = "sqlite"
	cfg.Storage.Path = dbPath
	cfg.Storage.BufferSize = 10
	cfg.Storage.BatchSize = 10
	ss, err := storage.NewStorageService(cfg, context.Background())
	if err != nil {
		t.Fatalf("NewStorageService: %v", err)
	}
	t.Cleanup(func() { _ = ss.GetStorage().Close() })

	scope := execution.IntentScope{Account: "scope-1", Exchange: "binance", Market: "futures", Symbol: "BTCUSDT", Bot: "bot-replay"}
	const orderID int64 = 502
	const cumulativeQty = 0.25
	trade := storage.Trade{
		ExecutionKey: position.GridTradeExecutionKey(scope.Bot, scope.Exchange, scope.Market, orderID, scope.Symbol, cumulativeQty),
		SellOrderID:  orderID, BotID: scope.Bot, Exchange: scope.Exchange, MarketType: scope.Market, Symbol: scope.Symbol,
		BuyPrice: 100, SellPrice: 110, Quantity: cumulativeQty, PnL: 2.5, ExchangePnL: 2.5, Fee: 0.01, FeeAsset: "USDT", CreatedAt: time.Now().UTC(),
	}
	payload, err := json.Marshal(trade)
	if err != nil {
		t.Fatal(err)
	}
	adapter := &tradeStorageAdapter{storageService: ss, accountID: "acct-1", accountScope: "scope-1", botID: scope.Bot}
	if err := adapter.ReplayPendingGridTrade(context.Background(), scope, orderID, cumulativeQty, payload); err != nil {
		t.Fatalf("replay pending trade: %v", err)
	}
	if err := adapter.ReplayPendingGridTrade(context.Background(), scope, orderID, cumulativeQty, payload); err != nil {
		t.Fatalf("idempotent replay: %v", err)
	}
	if err := adapter.ReplayPendingGridTrade(context.Background(), scope, orderID+1, cumulativeQty, payload); err == nil {
		t.Fatal("trade payload bound to another order must be rejected")
	}
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var count int
	var account, accountScope string
	if err := db.QueryRow(`SELECT COUNT(*), MAX(account), MAX(account_scope) FROM trades WHERE execution_key = ?`, trade.ExecutionKey).Scan(&count, &account, &accountScope); err != nil {
		t.Fatal(err)
	}
	if count != 1 || account != "acct-1" || accountScope != "scope-1" {
		t.Fatalf("replay rows=%d account=%q scope=%q; want one row with exact owner", count, account, accountScope)
	}
}
