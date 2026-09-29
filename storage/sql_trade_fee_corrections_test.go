package storage

import (
	"math"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestTradeFeeCorrectionPersistsIdempotentScopedOpeningHold(t *testing.T) {
	store, err := NewSQLStorage(filepath.Join(t.TempDir(), "fee-corrections.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	correction := &TradeFeeCorrection{
		CorrectionID: "correction-one", BotID: "bot-a", Exchange: "BINANCE", MarketType: "FUTURES",
		Symbol: "btcusdt", AccountScope: "scope-a", Account: "account-a", OrderID: 912,
		ClientOrderID: "client-912", Leg: "open", Side: "BUY", Fee: 0.25, FeeAsset: "usdt",
		ExecutedQty: 0.5, Reason: "late fee history",
	}
	if err := store.SaveTradeFeeCorrection(correction); err != nil {
		t.Fatalf("save fee correction: %v", err)
	}
	if err := store.SaveTradeFeeCorrection(correction); err != nil {
		t.Fatalf("idempotent fee correction replay: %v", err)
	}
	if err := store.SaveEvent("trade_fee_correction", map[string]interface{}{
		"correction_id": correction.CorrectionID, "bot_id": correction.BotID,
		"exchange": correction.Exchange, "symbol": correction.Symbol,
	}); err != nil {
		t.Fatalf("save linked audit event: %v", err)
	}
	conflict := *correction
	conflict.Fee = 0.3
	if err := store.SaveTradeFeeCorrection(&conflict); err == nil {
		t.Fatal("reused correction identity with changed economics was accepted")
	}

	count, err := store.CountPendingTradeFeeCorrections("binance", "futures", "BTCUSDT", "scope-a", "bot-a")
	if err != nil || count != 1 {
		t.Fatalf("pending scoped count=%d err=%v, want 1", count, err)
	}
	otherScope, err := store.CountPendingTradeFeeCorrections("binance", "futures", "BTCUSDT", "scope-b", "bot-a")
	if err != nil || otherScope != 0 {
		t.Fatalf("different credential scope count=%d err=%v, want 0", otherScope, err)
	}
	corrections, err := store.GetPendingTradeFeeCorrections("binance", "futures", "BTCUSDT", "scope-a", "bot-a")
	if err != nil || len(corrections) != 1 || corrections[0].FeeAsset != "USDT" || corrections[0].Status != "pending" {
		t.Fatalf("pending fee correction=%+v err=%v", corrections, err)
	}
	for _, trade := range []*Trade{
		{BuyOrderID: 912, SellOrderID: 1001, BotID: "bot-a", Exchange: "binance", MarketType: "futures", PnLAsset: "USDT", AccountScope: "scope-a", Account: "account-a", Symbol: "BTCUSDT", Quantity: 0.2, Fee: 0.01, FeeAsset: "USDT", CreatedAt: time.Now()},
		{BuyOrderID: 912, SellOrderID: 1002, BotID: "bot-a", Exchange: "binance", MarketType: "futures", PnLAsset: "USDT", AccountScope: "scope-a", Account: "account-a", Symbol: "BTCUSDT", Quantity: 0.3, Fee: 0.02, FeeAsset: "USDT", CreatedAt: time.Now()},
	} {
		if err := store.SaveTrade(trade); err != nil {
			t.Fatalf("save paired trade: %v", err)
		}
	}

	evidence := "exchange fill history verified ref-42"
	if err := store.ResolveTradeFeeCorrection("correction-one", "binance", "futures", "BTCUSDT", "scope-a", "bot-a", evidence, time.Now()); err != nil {
		t.Fatalf("resolve with evidence: %v", err)
	}
	if err := store.ResolveTradeFeeCorrection("correction-one", "binance", "futures", "BTCUSDT", "scope-a", "bot-a", evidence, time.Now()); err != nil {
		t.Fatalf("idempotent resolution retry: %v", err)
	}
	var totalFee float64
	if err := store.db.QueryRow(`SELECT SUM(fee) FROM trades WHERE buy_order_id = 912`).Scan(&totalFee); err != nil || math.Abs(totalFee-0.28) > 1e-8 {
		t.Fatalf("paired trades fee sum=%v err=%v, want 0.28", totalFee, err)
	}
	count, err = store.CountPendingTradeFeeCorrections("binance", "futures", "BTCUSDT", "scope-a", "bot-a")
	if err != nil || count != 0 {
		t.Fatalf("pending count after scoped resolution=%d err=%v, want 0", count, err)
	}
	resolved, err := store.getTradeFeeCorrection("correction-one")
	if err != nil || resolved.Status != "resolved" || resolved.Evidence != evidence {
		t.Fatalf("resolved correction evidence=%+v err=%v", resolved, err)
	}
	if err := store.ResolveTradeFeeCorrection("correction-one", "binance", "futures", "BTCUSDT", "scope-a", "bot-a", "different evidence ref", time.Now()); err == nil {
		t.Fatal("duplicate resolution was accepted")
	}
	if err := store.SaveTradeFeeCorrection(correction); err == nil {
		t.Fatal("replay after resolution was accepted without renewed reconciliation")
	}
}

func TestLegacyTradeFeeCorrectionEventRestoresConservativeHold(t *testing.T) {
	store, err := NewSQLStorage(filepath.Join(t.TempDir(), "legacy-fee-corrections.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.SaveEvent("trade_fee_correction", map[string]interface{}{
		"bot_id": "legacy-bot", "exchange": "binance", "symbol": "BTCUSDT", "order_id": 42,
	}); err != nil {
		t.Fatal(err)
	}
	count, err := store.CountPendingTradeFeeCorrections("binance", "futures", "BTCUSDT", "scope-a", "legacy-bot")
	if err != nil || count != 1 {
		t.Fatalf("legacy correction count=%d err=%v, want conservative pending hold", count, err)
	}
	legacy, err := store.GetPendingTradeFeeCorrections("binance", "futures", "BTCUSDT", "scope-a", "legacy-bot")
	if err != nil || len(legacy) != 1 || !legacy[0].LegacyEvent || legacy[0].CorrectionID != "legacy-event-1" || legacy[0].ExecutedQty != 0 {
		t.Fatalf("legacy correction not exposed as manual-only evidence: %+v err=%v", legacy, err)
	}
	otherBot, err := store.CountPendingTradeFeeCorrections("binance", "futures", "BTCUSDT", "scope-a", "different-bot")
	if err != nil || otherBot != 0 {
		t.Fatalf("unrelated bot correction count=%d err=%v, want 0", otherBot, err)
	}
}

func TestTradeFeeCorrectionRejectsMissingEvidenceAndOwner(t *testing.T) {
	store, err := NewSQLStorage(filepath.Join(t.TempDir(), "fee-corrections-invalid.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.SaveTradeFeeCorrection(&TradeFeeCorrection{CorrectionID: "x", OrderID: 10, Fee: 1, Reason: "missing owner"}); err == nil {
		t.Fatal("fee correction without owner scope was accepted")
	}
	if err := store.ResolveTradeFeeCorrection("x", "binance", "futures", "BTCUSDT", "scope", "bot", "", time.Now()); err == nil {
		t.Fatal("fee correction resolved without reconciliation evidence")
	}
}

func TestTradeFeeCorrectionSQLiteMigrationAddsExecutedQuantity(t *testing.T) {
	store, err := NewSQLStorage(filepath.Join(t.TempDir(), "fee-corrections-migration.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.db.Exec(`ALTER TABLE trade_fee_corrections DROP COLUMN executed_qty`); err != nil {
		t.Fatalf("prepare pre-migration schema: %v", err)
	}
	if err := migrateTradeFeeCorrectionsTableSQLite(store.db); err != nil {
		t.Fatalf("apply executed quantity migration: %v", err)
	}
	if err := store.db.QueryRow(`SELECT executed_qty FROM trade_fee_corrections LIMIT 0`).Scan(new(float64)); err == nil {
		t.Fatal("zero-row query unexpectedly scanned a value")
	} else if !strings.Contains(err.Error(), "no rows") {
		t.Fatalf("executed_qty column missing after migration: %v", err)
	}
}

func TestResolveTradeFeeCorrectionFailsClosedWithoutExactFeeCoverage(t *testing.T) {
	for _, test := range []struct {
		name       string
		tradeQty   float64
		baseFeeQty float64
		pnlAsset   string
		feeAsset   string
	}{
		{name: "partial quantity", tradeQty: 0.4, pnlAsset: "USDT", feeAsset: "USDT"},
		{name: "base asset fee changes inventory", tradeQty: 0.5, baseFeeQty: 0.001, pnlAsset: "USDT", feeAsset: "USDT"},
		{name: "denomination mismatch", tradeQty: 0.5, pnlAsset: "USDT", feeAsset: "BTC"},
	} {
		t.Run(test.name, func(t *testing.T) {
			store, err := NewSQLStorage(filepath.Join(t.TempDir(), "fee-corrections-reject.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			correction := &TradeFeeCorrection{CorrectionID: "reject-case", BotID: "bot", Exchange: "binance", MarketType: "futures",
				Symbol: "BTCUSDT", AccountScope: "scope", Account: "account", OrderID: 77, Leg: "close", Side: "SELL",
				Fee: 0.2, FeeAsset: "USDT", BaseFeeQty: test.baseFeeQty, ExecutedQty: 0.5, Reason: "late fee correction"}
			if err := store.SaveTradeFeeCorrection(correction); err != nil {
				t.Fatalf("save correction: %v", err)
			}
			if err := store.SaveTrade(&Trade{BuyOrderID: 70, SellOrderID: 77, BotID: "bot", Exchange: "binance", MarketType: "futures",
				PnLAsset: test.pnlAsset, AccountScope: "scope", Account: "account", Symbol: "BTCUSDT", Quantity: test.tradeQty,
				Fee: 0.03, FeeAsset: test.feeAsset, CreatedAt: time.Now()}); err != nil {
				t.Fatalf("save paired trade: %v", err)
			}
			if err := store.ResolveTradeFeeCorrection("reject-case", "binance", "futures", "BTCUSDT", "scope", "bot", "verified exchange ledger reference", time.Now()); err == nil {
				t.Fatal("unsafe fee correction was applied")
			}
			var fee float64
			if err := store.db.QueryRow(`SELECT fee FROM trades WHERE sell_order_id = 77`).Scan(&fee); err != nil || math.Abs(fee-0.03) > 1e-10 {
				t.Fatalf("trade fee changed after rejected correction: fee=%v err=%v", fee, err)
			}
			pending, err := store.CountPendingTradeFeeCorrections("binance", "futures", "BTCUSDT", "scope", "bot")
			if err != nil || pending != 1 {
				t.Fatalf("pending correction count=%d err=%v; failed apply must retain hold", pending, err)
			}
		})
	}
}
