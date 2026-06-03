package storage

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"quantmesh/config"
)

func TestAppConfigDocumentSnapshotsAndBotSync(t *testing.T) {
	ctx := context.Background()
	store, err := NewSQLStorage(filepath.Join(t.TempDir(), "app_config.db"))
	if err != nil {
		t.Fatalf("new sql storage: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	if sha256Hex("abc") == "" || !isAppConfigTableMissing(assertStorageErr("no such table: app_config")) ||
		!isAppConfigTableMissing(assertStorageErr("Error 1146: Table 'db.app_config' doesn't exist")) ||
		isAppConfigTableMissing(nil) {
		t.Fatalf("app config helper mismatch")
	}
	if !isBotConfigsTableMissing(assertStorageErr("no such table: bot_configs")) || isBotConfigsTableMissing(assertStorageErr("no such table: other")) {
		t.Fatalf("bot config missing helper mismatch")
	}
	if nullStr("") != nil || nullStr("operator").(string) != "operator" {
		t.Fatalf("nullStr mismatch")
	}
	if err := (&SQLStorage{}).EnsureAppConfigDocumentTables(); err == nil {
		t.Fatalf("nil db ensure should fail")
	}

	if doc, err := store.GetAppConfigDocument(ctx); err != nil || doc != nil {
		t.Fatalf("empty app doc=%#v err=%v", doc, err)
	}
	if _, err := SaveAppConfigSnapshotFromJSON(ctx, nil, []byte(`{}`), "op", "src"); err == nil {
		t.Fatalf("nil storage save json should fail")
	}
	if _, err := SaveAppConfigSnapshotFromJSON(ctx, store, []byte(` `), "op", "src"); err == nil {
		t.Fatalf("empty json should fail")
	}

	rev, err := SaveAppConfigSnapshotFromJSON(ctx, store, []byte(`{"app":{"name":"qm"}}`), "op", "src")
	if err != nil || rev != 1 {
		t.Fatalf("save json rev=%d err=%v", rev, err)
	}
	doc, err := store.GetAppConfigDocument(ctx)
	if err != nil || doc == nil || doc.Revision != 1 || !strings.Contains(doc.Content, "qm") || doc.ContentHash == "" {
		t.Fatalf("app doc=%#v err=%v", doc, err)
	}
	rev, err = SaveAppConfigSnapshotFromJSON(ctx, store, []byte(`{"app":{"name":"qm2"}}`), "", "")
	if err != nil || rev != 2 {
		t.Fatalf("second save json rev=%d err=%v", rev, err)
	}

	cfg := config.CreateMinimalConfig()
	cfg.App.CurrentExchange = "binance"
	cfg.Bots = []config.BotConfig{{
		ID:            "bot-1",
		Exchange:      "binance",
		Symbol:        "BTCUSDT",
		MarketType:    "futures",
		CreatedAt:     "2026-06-01T00:00:00Z",
		OrderQuantity: 100,
	}}
	rev, err = SaveAppConfigSnapshotWithBotSource(ctx, store, cfg, "tester", "app_src", "bot_src")
	if err != nil || rev != 3 {
		t.Fatalf("save config rev=%d err=%v", rev, err)
	}
	botDoc, err := store.GetBotConfigDocument(ctx, "bot-1")
	if err != nil || botDoc == nil || botDoc.Revision != 1 || botDoc.ContentHash == "" {
		t.Fatalf("bot doc=%#v err=%v", botDoc, err)
	}
	if empty, err := store.GetBotConfigDocument(ctx, ""); err != nil || empty != nil {
		t.Fatalf("empty bot id doc=%#v err=%v", empty, err)
	}
	list, err := store.ListBotConfigDocuments(ctx)
	if err != nil || len(list) != 1 {
		t.Fatalf("bot list=%#v err=%v", list, err)
	}

	bf := config.ConvertFromBotConfig(cfg.Bots[0])
	bf.BotID = "bot-1"
	bf.Symbol = "ETHUSDT"
	rev, err = SaveBotConfigSnapshot(ctx, store, bf, "tester", "manual")
	if err != nil || rev != 2 {
		t.Fatalf("save bot rev=%d err=%v", rev, err)
	}
	if _, err := SaveBotConfigSnapshot(ctx, nil, bf, "", ""); err == nil {
		t.Fatalf("nil storage bot save should fail")
	}
	if _, err := SaveBotConfigSnapshot(ctx, store, nil, "", ""); err == nil {
		t.Fatalf("nil bot config should fail")
	}
	bf.BotID = ""
	if _, err := SaveBotConfigSnapshot(ctx, store, bf, "", ""); err == nil {
		t.Fatalf("empty bot id should fail")
	}

	if err := SyncBotConfigSnapshotsFromMainConfig(ctx, store, cfg, "tester", "sync"); err != nil {
		t.Fatalf("sync bot snapshots: %v", err)
	}
	list, err = store.ListBotConfigDocuments(ctx)
	if err != nil || len(list) < 1 {
		t.Fatalf("synced bot list=%#v err=%v", list, err)
	}
	if err := SyncBotConfigSnapshotsFromMainConfig(ctx, nil, cfg, "", ""); err != nil {
		t.Fatalf("nil storage sync should no-op: %v", err)
	}
	if err := SyncBotConfigSnapshotsFromMainConfig(ctx, store, nil, "", ""); err != nil {
		t.Fatalf("nil config sync should no-op: %v", err)
	}

	if err := DeleteBotConfigSnapshot(ctx, store, "bot-1"); err != nil {
		t.Fatalf("delete bot: %v", err)
	}
	if deleted, err := store.GetBotConfigDocument(ctx, "bot-1"); err != nil || deleted != nil {
		t.Fatalf("deleted bot=%#v err=%v", deleted, err)
	}
	if err := DeleteBotConfigSnapshot(ctx, nil, "bot-1"); err == nil {
		t.Fatalf("nil storage delete should fail")
	}
	if err := DeleteBotConfigSnapshot(ctx, store, ""); err == nil {
		t.Fatalf("empty bot delete should fail")
	}
}

type assertStorageErr string

func (e assertStorageErr) Error() string {
	return string(e)
}
