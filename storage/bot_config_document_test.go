package storage

import (
	"context"
	"path/filepath"
	"testing"

	"quantmesh/config"
)

func TestSaveBotConfigSnapshotRoundTrip(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "t.db")
	st, err := NewSQLStorage(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	ctx := context.Background()
	bf := &config.BotConfigFile{
		BotID:        "bot-a",
		Name:         "Test",
		Exchange:     "binance",
		Symbol:       "BTCUSDT",
		MarketType:   "futures",
		StrategyMode: "single",
	}
	bf.Advanced.PositionSafetyCheck = 7

	rev, err := SaveBotConfigSnapshot(ctx, st, bf, "test", "unit")
	if err != nil {
		t.Fatal(err)
	}
	if rev < 1 {
		t.Fatalf("revision %d", rev)
	}

	doc, err := st.GetBotConfigDocument(ctx, "bot-a")
	if err != nil {
		t.Fatal(err)
	}
	if doc == nil || doc.Revision != rev {
		t.Fatalf("doc mismatch: %+v rev want %d", doc, rev)
	}

	if err := DeleteBotConfigSnapshot(ctx, st, "bot-a"); err != nil {
		t.Fatal(err)
	}
	doc2, err := st.GetBotConfigDocument(ctx, "bot-a")
	if err != nil {
		t.Fatal(err)
	}
	if doc2 != nil {
		t.Fatalf("expected nil after delete, got %+v", doc2)
	}
}
