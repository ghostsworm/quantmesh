package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"testing"

	_ "github.com/mattn/go-sqlite3"

	"quantmesh/config"
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
