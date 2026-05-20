package storage

import (
	"context"
	"path/filepath"
	"testing"
)

func TestGormConfigStorageSQLiteRoundTrip(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "config.db")
	st, err := NewGormConfigStorage("sqlite", dbPath)
	if err != nil {
		t.Fatalf("NewGormConfigStorage: %v", err)
	}

	ctx := context.Background()
	entry := &ConfigEntry{
		Key:         "database.dsn",
		Scope:       ScopeGlobal,
		ScopeID:     "",
		Type:        TypeString,
		Value:       "postgresql://user:pass@db.example.com:5432/quantmesh?sslmode=require",
		Category:    "database",
		DisplayName: "Database DSN",
		Editable:    true,
	}
	if err := st.SetConfig(ctx, entry, "test"); err != nil {
		t.Fatalf("SetConfig insert: %v", err)
	}

	got, err := st.GetConfig(ctx, ScopeGlobal, "", "database.dsn")
	if err != nil {
		t.Fatalf("GetConfig: %v", err)
	}
	if got == nil || got.Value != entry.Value {
		t.Fatalf("unexpected config: %+v", got)
	}

	entry.Value = "postgresql://user:pass@project.supabase.co:5432/postgres?sslmode=require"
	if err := st.SetConfig(ctx, entry, "test2"); err != nil {
		t.Fatalf("SetConfig update: %v", err)
	}
	got, err = st.GetConfig(ctx, ScopeGlobal, "", "database.dsn")
	if err != nil {
		t.Fatalf("GetConfig updated: %v", err)
	}
	if got.Version != 1 {
		t.Fatalf("expected version 1 after update, got %d", got.Version)
	}
	history, err := st.GetConfigHistory(ctx, got.ID, 10)
	if err != nil {
		t.Fatalf("GetConfigHistory: %v", err)
	}
	if len(history) != 2 {
		t.Fatalf("expected 2 history rows, got %d", len(history))
	}
}
