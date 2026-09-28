package storage

import (
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"quantmesh/execution"
)

func TestExecutionIntentJournalDurableCASAndMigration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "intents.db")
	s, err := NewSQLStorage(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	key, _ := (execution.IntentScope{Account: "account-a", Exchange: "fake", Market: "futures", Symbol: "BTCUSDT", Bot: "a"}).Key()
	if _, err := s.LoadExecutionIntents(ctx, key, 0, 500); err == nil {
		t.Fatal("journal auto-migrated at startup")
	}
	if err := s.MigrateExecutionIntents(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveExecutionIntent(ctx, key, "cid", 0, []byte(`{"phase":"prepared"}`)); err != nil {
		t.Fatal(err)
	}
	var synchronous int
	if err := s.db.QueryRow(`PRAGMA synchronous`).Scan(&synchronous); err != nil || synchronous != 2 {
		t.Fatalf("durability mode=%d err=%v", synchronous, err)
	}
	if err := s.SaveExecutionIntent(ctx, key, "cid", 0, []byte(`{}`)); !errors.Is(err, execution.ErrIntentJournalConflict) {
		t.Fatalf("duplicate: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = NewSQLStorage(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	page, err := s.LoadExecutionIntents(ctx, key, 0, 500)
	if err != nil || len(page) != 1 || page[0].Revision != 1 || string(page[0].Payload) != `{"phase":"prepared"}` {
		t.Fatalf("restart page=%v err=%v", page, err)
	}
	var accepted atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := s.SaveExecutionIntent(ctx, key, "cid", 1, []byte(`{"phase":"unknown"}`))
			if err == nil {
				accepted.Add(1)
			} else if !errors.Is(err, execution.ErrIntentJournalConflict) {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if accepted.Load() != 1 {
		t.Fatalf("CAS accepted %d writers", accepted.Load())
	}
	if err := s.MigrateExecutionIntents(ctx); err != nil {
		t.Fatal(err)
	}
	page, err = s.LoadExecutionIntents(ctx, key, 0, 500)
	if err != nil || len(page) != 1 || page[0].Revision != 2 {
		t.Fatal("idempotent migration changed journal")
	}
	down, err := executionIntentMigrations.ReadFile("migrations/2026092402_execution_intents_sqlite.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.ExecContext(ctx, string(down)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.LoadExecutionIntents(ctx, key, 0, 500); err == nil {
		t.Fatal("missing table treated as empty")
	}
	if err := s.MigrateExecutionIntents(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestExecutionIntentJournalPaginatesAndIsolatesOwners(t *testing.T) {
	s, err := NewSQLStorage(filepath.Join(t.TempDir(), "pages.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := t.Context()
	if err := s.MigrateExecutionIntents(ctx); err != nil {
		t.Fatal(err)
	}
	a := execution.IntentScope{Account: "account-a", Exchange: "fake", Market: "futures", Symbol: "BTCUSDT", Bot: "a"}
	b := a
	b.Bot = "b"
	c := a
	c.Account = "account-b"
	key, _ := a.Key()
	for _, scope := range []execution.IntentScope{a, b, c} {
		k, _ := scope.Key()
		n := 1
		if k == key {
			n = 2101
		}
		for i := 0; i < n; i++ {
			if err := s.SaveExecutionIntent(ctx, k, fmt.Sprintf("cid-%d", i), 0, []byte(`{"phase":"prepared"}`)); err != nil {
				t.Fatal(err)
			}
		}
	}
	var after int64
	count := 0
	for {
		page, err := s.LoadExecutionIntents(ctx, key, after, 500)
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range page {
			if r.ID <= after {
				t.Fatal("non-increasing cursor")
			}
			after = r.ID
			count++
		}
		if len(page) < 500 {
			break
		}
	}
	if count != 2101 {
		t.Fatalf("lost/foreign records: %d", count)
	}
}
