package storage

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestMySQLRiskCheckpointDurableCAS(t *testing.T) {
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
	store := &SQLStorage{db: db, dbType: "mysql"}
	ctx := t.Context()
	if err := store.MigrateRiskCheckpoints(ctx); err != nil {
		t.Fatal(err)
	}
	key := fmt.Sprintf("risk-checkpoint-mysql-test-%d", time.Now().UnixNano())
	t.Cleanup(func() { _, _ = db.ExecContext(ctx, `DELETE FROM risk_checkpoints WHERE checkpoint_key = ?`, key) })
	if payload, revision, err := store.LoadRiskCheckpoint(ctx, key); err != nil || payload != nil || revision != 0 {
		t.Fatalf("initial state: payload=%s revision=%d err=%v", payload, revision, err)
	}
	if err := store.SaveRiskCheckpoint(ctx, key, 0, []byte(`{"high_water":1200}`)); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveRiskCheckpoint(ctx, key, 0, []byte(`{"high_water":1}`)); !errors.Is(err, ErrRiskCheckpointConflict) {
		t.Fatalf("duplicate initialization did not conflict: %v", err)
	}
	const writers = 12
	var accepted atomic.Int32
	var workers sync.WaitGroup
	for i := 0; i < writers; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			err := store.SaveRiskCheckpoint(ctx, key, 1, []byte(`{"high_water":1300}`))
			if err == nil {
				accepted.Add(1)
			} else if !errors.Is(err, ErrRiskCheckpointConflict) {
				t.Errorf("unexpected CAS error: %v", err)
			}
		}()
	}
	workers.Wait()
	if accepted.Load() != 1 {
		t.Fatalf("CAS accepted %d concurrent writers, want 1", accepted.Load())
	}
	payload, revision, err := store.LoadRiskCheckpoint(ctx, key)
	if err != nil || revision != 2 || string(payload) != `{"high_water":1300}` {
		t.Fatalf("reloaded state: payload=%s revision=%d err=%v", payload, revision, err)
	}
	if err := store.SaveRiskCheckpoint(ctx, key, 2, []byte(`not-json`)); err == nil {
		t.Fatal("invalid JSON accepted")
	}
}
