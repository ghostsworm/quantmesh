package storage

import (
	"context"
	"strings"
	"testing"
)

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
