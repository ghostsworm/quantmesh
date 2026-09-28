package storage

import (
	"errors"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
)

func TestRiskCheckpointDurableCASAndMigrationRollback(t *testing.T) {
	path := filepath.Join(t.TempDir(), "risk.db")
	store, err := NewSQLStorage(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	if _, _, err := store.LoadRiskCheckpoint(ctx, "equity"); err == nil {
		t.Fatal("startup auto-created risk state schema")
	}
	if err := store.MigrateRiskCheckpoints(ctx); err != nil {
		t.Fatal(err)
	}
	if payload, revision, err := store.LoadRiskCheckpoint(ctx, "equity"); err != nil || payload != nil || revision != 0 {
		t.Fatalf("missing: %s %d %v", payload, revision, err)
	}
	if err := store.SaveRiskCheckpoint(ctx, "equity", 0, []byte(`{"high_water":1200}`)); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveRiskCheckpoint(ctx, "equity", 0, []byte(`{"high_water":1}`)); !errors.Is(err, ErrRiskCheckpointConflict) {
		t.Fatalf("duplicate initialization: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = NewSQLStorage(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	payload, revision, err := store.LoadRiskCheckpoint(ctx, "equity")
	if err != nil || revision != 1 || string(payload) != `{"high_water":1200}` {
		t.Fatalf("restart: %s %d %v", payload, revision, err)
	}
	var accepted atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := store.SaveRiskCheckpoint(ctx, "equity", 1, []byte(`{"high_water":1300}`))
			if err == nil {
				accepted.Add(1)
			} else if !errors.Is(err, ErrRiskCheckpointConflict) {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if accepted.Load() != 1 {
		t.Fatalf("CAS accepted %d concurrent writers", accepted.Load())
	}
	if err := migrateRiskCheckpoints(store.db, "sqlite"); err != nil {
		t.Fatal(err)
	}
	_, revision, err = store.LoadRiskCheckpoint(ctx, "equity")
	if err != nil || revision != 2 {
		t.Fatalf("idempotent migration lost state: %d %v", revision, err)
	}
	for _, bad := range [][]byte{nil, []byte(`not json`)} {
		if err := store.SaveRiskCheckpoint(ctx, "equity", 2, bad); err == nil {
			t.Fatal("invalid JSON accepted")
		}
	}
	// Exercise down/up ONLY on this disposable test database.
	down, err := riskCheckpointMigrations.ReadFile("migrations/2026092401_risk_checkpoints_sqlite.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(ctx, string(down)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.LoadRiskCheckpoint(ctx, "equity"); err == nil {
		t.Fatal("missing table treated as absent record")
	}
	if err := migrateRiskCheckpoints(store.db, "sqlite"); err != nil {
		t.Fatal(err)
	}
}
