package storage

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

func TestMySQLOpeningPauseOwnersAreIndependentByInstance(t *testing.T) {
	dsn := os.Getenv("QUANTMESH_MYSQL_TEST_DSN")
	if dsn == "" {
		t.Skip("requires QUANTMESH_MYSQL_TEST_DSN pointing to a disposable MySQL schema")
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal("open isolated MySQL test database:", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		t.Fatal("connect to isolated MySQL test database:", err)
	}
	store := &SQLStorage{db: db, dbType: "mysql"}
	if err := store.MigrateOpeningPauseHolders(ctx); err != nil {
		t.Fatal("apply opening pause owner migrations:", err)
	}
	unique := fmt.Sprintf("mysql-pause-%d", time.Now().UTC().UnixNano())
	first := OpeningPauseHolder{OwnerID: unique + "-a", Source: "circuit_breaker", Reason: "owner a"}
	second := OpeningPauseHolder{OwnerID: unique + "-b", Source: "circuit_breaker", Reason: "owner b"}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM opening_pause_owners WHERE owner_id IN (?, ?)`, first.OwnerID, second.OwnerID)
	})
	for _, holder := range []OpeningPauseHolder{first, second} {
		if err := store.UpsertOpeningPauseHolder(ctx, holder); err != nil {
			t.Fatal("persist isolated MySQL pause owner:", err)
		}
	}
	if err := store.DeleteOpeningPauseHolder(ctx, first); err != nil {
		t.Fatal("delete first MySQL pause owner:", err)
	}
	got, err := store.LoadOpeningPauseHolders(ctx)
	if err != nil {
		t.Fatal("reload MySQL pause owners:", err)
	}
	var foundSecond bool
	for _, holder := range got {
		if holder.OwnerID == second.OwnerID && holder.Source == second.Source && holder.Reason == second.Reason {
			foundSecond = true
		}
		if holder.OwnerID == first.OwnerID && holder.Source == first.Source {
			t.Fatal("deleting one MySQL owner removed neither owner")
		}
	}
	if !foundSecond {
		t.Fatalf("deleting one MySQL owner removed the other: holders=%+v", got)
	}
	if err := store.MigrateOpeningPauseHolders(ctx); err != nil {
		t.Fatal("reapply idempotent opening pause migrations:", err)
	}
}

func TestOpeningPauseHoldersPersistUntilExplicitDelete(t *testing.T) {
	store, err := NewSQLStorage(t.TempDir() + "/opening-pause.db")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.MigrateOpeningPauseHolders(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	legacyHolders, err := store.LoadOpeningPauseHolders(ctx)
	if err != nil || len(legacyHolders) != 1 || legacyHolders[0].Source != "opening_pause_legacy_state_unverified" {
		t.Fatalf("first migration must fail closed for legacy pause state: holders=%+v err=%v", legacyHolders, err)
	}
	if err := store.DeleteOpeningPauseHolder(ctx, OpeningPauseHolder{Source: "opening_pause_legacy_state_unverified"}); err != nil {
		t.Fatal(err)
	}
	if err := store.MigrateOpeningPauseHolders(ctx); err != nil {
		t.Fatal(err)
	}
	legacyHolders, err = store.LoadOpeningPauseHolders(ctx)
	if err != nil || len(legacyHolders) != 0 {
		t.Fatalf("acknowledged legacy hold was reinstalled by idempotent migration: holders=%+v err=%v", legacyHolders, err)
	}
	if err := store.UpsertOpeningPauseHolder(ctx, OpeningPauseHolder{Source: "circuit_breaker", Reason: "daily loss"}); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertOpeningPauseHolder(ctx, OpeningPauseHolder{Source: "circuit_breaker", Reason: "manual review required"}); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertOpeningPauseHolder(ctx, OpeningPauseHolder{Source: "composite_risk", Reason: "stop trading"}); err != nil {
		t.Fatal(err)
	}
	got, err := store.LoadOpeningPauseHolders(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != (OpeningPauseHolder{Source: "circuit_breaker", Reason: "manual review required"}) || got[1].Source != "composite_risk" {
		t.Fatalf("holders = %+v, want the latest reason for each source", got)
	}
	if err := store.DeleteOpeningPauseHolder(ctx, OpeningPauseHolder{Source: "circuit_breaker"}); err != nil {
		t.Fatal(err)
	}
	got, err = store.LoadOpeningPauseHolders(ctx)
	if err != nil || len(got) != 1 || got[0].Source != "composite_risk" {
		t.Fatalf("holders after source release = %+v, err=%v", got, err)
	}
}

func TestOpeningPauseOwnersAreIndependentByInstance(t *testing.T) {
	store, err := NewSQLStorage(t.TempDir() + "/opening-pause-owners.db")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.MigrateOpeningPauseHolders(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := store.DeleteOpeningPauseHolder(ctx, OpeningPauseHolder{Source: "opening_pause_legacy_state_unverified"}); err != nil {
		t.Fatal(err)
	}
	for _, holder := range []OpeningPauseHolder{
		{OwnerID: "instance-a", Source: "circuit_breaker", Reason: "daily loss"},
		{OwnerID: "instance-b", Source: "circuit_breaker", Reason: "daily loss"},
	} {
		if err := store.UpsertOpeningPauseHolder(ctx, holder); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.DeleteOpeningPauseHolder(ctx, OpeningPauseHolder{OwnerID: "instance-a", Source: "circuit_breaker"}); err != nil {
		t.Fatal(err)
	}
	got, err := store.LoadOpeningPauseHolders(ctx)
	if err != nil || len(got) != 1 || got[0].OwnerID != "instance-b" {
		t.Fatalf("deleting one instance owner affected another: holders=%+v err=%v", got, err)
	}
}

func TestOpeningPauseOwnerDownMigrationPreservesEveryActiveGate(t *testing.T) {
	store, err := NewSQLStorage(t.TempDir() + "/opening-pause-owner-down.db")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	if err := store.MigrateOpeningPauseHolders(ctx); err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteOpeningPauseHolder(ctx, OpeningPauseHolder{Source: "opening_pause_legacy_state_unverified"}); err != nil {
		t.Fatal(err)
	}
	for _, owner := range []string{"instance-a", "instance-b"} {
		if err := store.UpsertOpeningPauseHolder(ctx, OpeningPauseHolder{OwnerID: owner, Source: "circuit_breaker", Reason: "daily loss"}); err != nil {
			t.Fatal(err)
		}
	}
	script, err := openingPauseMigrations.ReadFile("migrations/2026093005_opening_pause_holders_sqlite.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range strings.Split(string(script), ";") {
		statement = strings.TrimSpace(statement)
		if statement == "" {
			continue
		}
		if _, err := store.db.ExecContext(ctx, statement); err != nil {
			t.Fatalf("run owner down migration statement %q: %v", statement, err)
		}
	}
	rows, err := store.db.QueryContext(ctx, `SELECT source FROM opening_pause_holders WHERE source LIKE 'circuit_breaker:down:%' ORDER BY source`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var retained []string
	for rows.Next() {
		var source string
		if err := rows.Scan(&source); err != nil {
			t.Fatal(err)
		}
		retained = append(retained, source)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(retained) != 2 {
		t.Fatalf("down migration discarded active process gates: %v", retained)
	}
}

func TestOpeningPauseHolderRejectsInvalidSource(t *testing.T) {
	store, err := NewSQLStorage(t.TempDir() + "/opening-pause.db")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.MigrateOpeningPauseHolders(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertOpeningPauseHolder(context.Background(), OpeningPauseHolder{Reason: "missing source"}); err == nil {
		t.Fatal("empty source unexpectedly persisted")
	}
}
