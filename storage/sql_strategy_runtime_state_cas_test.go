package storage

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestStrategyRuntimeStateConditionalWritePreservesNewEvidence(t *testing.T) {
	store, err := NewSQLStorage(filepath.Join(t.TempDir(), "cas.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	verifyRuntimeStateConditionalWrite(t, store, "bot-a")
}

func verifyRuntimeStateConditionalWrite(t *testing.T, store *SQLStorage, botID string) {
	t.Helper()
	seed := &StrategyRuntimeState{BotID: botID, StrategyName: "funding_carry", SchemaVersion: 7, Payload: `{"id":42}`}
	if err := store.SetStrategyRuntimeState(seed); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, bot, payload string
		version            int
	}{
		{"wrong_owner", botID + "-other", seed.Payload, 7},
		{"wrong_schema", botID, seed.Payload, 6},
		{"wrong_payload", botID, `{"id":43}`, 7},
		{"byte_exact", botID, `{"ID":42}`, 7},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ok, err := store.CompareAndSwapStrategyRuntimeState(t.Context(), &StrategyRuntimeState{BotID: tc.bot, StrategyName: seed.StrategyName, SchemaVersion: 7, Payload: `{"debt":0.4}`}, tc.version, tc.payload)
			if err != nil || ok {
				t.Fatalf("invalid provenance accepted: %v %v", ok, err)
			}
		})
	}
	next := &StrategyRuntimeState{BotID: seed.BotID, StrategyName: seed.StrategyName, SchemaVersion: 7, Payload: `{"debt":0.4}`}
	ok, err := store.CompareAndSwapStrategyRuntimeState(t.Context(), next, 7, seed.Payload)
	if err != nil || !ok {
		t.Fatalf("valid write failed: %v %v", ok, err)
	}
	ok, err = store.CompareAndSwapStrategyRuntimeState(t.Context(), seed, 7, seed.Payload)
	if err != nil || ok {
		t.Fatal("stale writer overwrote new evidence")
	}
	loaded, err := store.GetStrategyRuntimeState(seed.BotID, seed.StrategyName)
	if err != nil || loaded == nil || loaded.Payload != next.Payload {
		t.Fatalf("new evidence lost: %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if ok, err := store.CompareAndSwapStrategyRuntimeState(ctx, seed, 7, next.Payload); err == nil || ok {
		t.Fatal("cancelled write accepted")
	}
}

func TestMySQLStrategyRuntimeStateConditionalWritePreservesNewEvidence(t *testing.T) {
	dsn := os.Getenv("QUANTMESH_MYSQL_TEST_DSN")
	if dsn == "" {
		t.Skip("requires QUANTMESH_MYSQL_TEST_DSN pointing to a disposable MySQL schema")
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.PingContext(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := migrateStrategyRuntimeStateTableMySQL(db); err != nil {
		t.Fatal(err)
	}
	store := &SQLStorage{db: db, dbType: "mysql"}
	botID := fmt.Sprintf("mysql-runtime-cas-%d", time.Now().UnixNano())
	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM strategy_runtime_states WHERE bot_id IN (?, ?)`, botID, botID+"-other")
	})
	verifyRuntimeStateConditionalWrite(t, store, botID)
	seed := &StrategyRuntimeState{BotID: botID, StrategyName: "funding_carry", SchemaVersion: 7, Payload: `{"id":42}`}
	if err := store.SetStrategyRuntimeState(seed); err != nil {
		t.Fatal(err)
	}
	var collationEqual bool
	if err := db.QueryRowContext(t.Context(), `SELECT payload = ? FROM strategy_runtime_states WHERE bot_id = ? AND strategy_name = ?`, `{"ID":42}`, botID, seed.StrategyName).Scan(&collationEqual); err != nil || !collationEqual {
		t.Fatalf("fixture did not exercise case-insensitive MySQL collation: equal=%v err=%v", collationEqual, err)
	}
	const writers = 8
	results := make(chan bool, writers)
	errors := make(chan error, writers)
	var workers sync.WaitGroup
	for i := 0; i < writers; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			next := &StrategyRuntimeState{BotID: botID, StrategyName: seed.StrategyName, SchemaVersion: 7, Payload: `{"id":43}`}
			saved, err := store.CompareAndSwapStrategyRuntimeState(t.Context(), next, 7, seed.Payload)
			results <- saved
			errors <- err
		}()
	}
	workers.Wait()
	close(results)
	close(errors)
	winners := 0
	for saved := range results {
		if saved {
			winners++
		}
	}
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	if winners != 1 {
		t.Fatalf("concurrent conditional writers=%d, want exactly one", winners)
	}
	loaded, err := store.GetStrategyRuntimeStateContext(t.Context(), botID, seed.StrategyName)
	if err != nil || loaded == nil || loaded.Payload != `{"id":43}` {
		t.Fatalf("winning checkpoint missing: %+v err=%v", loaded, err)
	}
	t.Run("identical_snapshot_noop", func(t *testing.T) { verifyMySQLRuntimeStateNoopCAS(t, dsn) })
}
