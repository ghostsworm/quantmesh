package storage

import (
	"context"
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
	if err := store.DeleteOpeningPauseHolder(ctx, "opening_pause_legacy_state_unverified"); err != nil {
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
	if err := store.DeleteOpeningPauseHolder(ctx, "circuit_breaker"); err != nil {
		t.Fatal(err)
	}
	got, err = store.LoadOpeningPauseHolders(ctx)
	if err != nil || len(got) != 1 || got[0].Source != "composite_risk" {
		t.Fatalf("holders after source release = %+v, err=%v", got, err)
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
