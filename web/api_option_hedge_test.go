package web

import (
	"context"
	"path/filepath"
	"testing"

	"quantmesh/config"
	"quantmesh/storage"
)

func TestResolveBotConfigForOptionHedge_FromBotConfigsTable(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "t.db")
	st, err := storage.NewSQLStorage(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.EnsureAppConfigDocumentTables(); err != nil {
		t.Fatal(err)
	}
	SetPrimaryStorageForAppConfig(st)
	t.Cleanup(func() { SetPrimaryStorageForAppConfig(nil) })

	bf := &config.BotConfigFile{
		BotID:        "e1e001e8-6ba9-4851-ae61-9197a9a0cb1e",
		Name:         "Test",
		Exchange:     "binance",
		Symbol:       "BTCUSDT",
		MarketType:   "futures",
		StrategyMode: "single",
	}
	ctx := context.Background()
	if _, err := storage.SaveBotConfigSnapshot(ctx, st, bf, "test", "unit"); err != nil {
		t.Fatal(err)
	}

	got, err := resolveBotConfigForOptionHedge(bf.BotID)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.BotID != bf.BotID || got.Exchange != "binance" {
		t.Fatalf("unexpected: %+v", got)
	}
}
