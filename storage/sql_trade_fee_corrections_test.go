package storage

import (
	"path/filepath"
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
		BaseFeeQty: 0.001, Reason: "late fee history",
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

	if err := store.ResolveTradeFeeCorrection("correction-one", "binance", "futures", "BTCUSDT", "scope-a", "bot-a", "external ledger row #42 corrected", time.Now()); err != nil {
		t.Fatalf("resolve with evidence: %v", err)
	}
	count, err = store.CountPendingTradeFeeCorrections("binance", "futures", "BTCUSDT", "scope-a", "bot-a")
	if err != nil || count != 0 {
		t.Fatalf("pending count after scoped resolution=%d err=%v, want 0", count, err)
	}
	resolved, err := store.getTradeFeeCorrection("correction-one")
	if err != nil || resolved.Status != "resolved" || resolved.Evidence != "external ledger row #42 corrected" {
		t.Fatalf("resolved correction evidence=%+v err=%v", resolved, err)
	}
	if err := store.ResolveTradeFeeCorrection("correction-one", "binance", "futures", "BTCUSDT", "scope-a", "bot-a", "duplicate", time.Now()); err == nil {
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
