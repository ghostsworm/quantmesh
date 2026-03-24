package storage

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

func TestEnsureAppConfigDocumentTables_OnBareDB(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bare.db")
	db, err := sql.Open("sqlite3", path+"?_journal_mode=WAL")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS _placeholder(x INTEGER)`); err != nil {
		t.Fatal(err)
	}
	s := &SQLiteStorage{db: db, dbType: "sqlite"}
	if err := s.EnsureAppConfigDocumentTables(); err != nil {
		t.Fatal(err)
	}
	if err := s.EnsureAppConfigDocumentTables(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetAppConfigDocument(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestMigrateYAMLToAppConfigDB_Minimal(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "t.db")
	st, err := NewSQLiteStorage(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	yamlPath := filepath.Join(dir, "c.yaml")
	minimal := `app:
  current_exchange: binance
exchanges:
  binance:
    api_key: "k"
    secret_key: "s"
trading:
  symbols:
    - symbol: BTCUSDT
      exchange: binance
      price_interval: 100
      order_quantity: 10
      buy_window_size: 5
      sell_window_size: 5
`
	if err := os.WriteFile(yamlPath, []byte(minimal), 0644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("QUANTMESH_MIGRATE_APP_CONFIG_FORCE", "1")
	if _, err := MigrateYAMLToAppConfigDB(context.Background(), st, yamlPath, filepath.Join(dir, "nobots"), MigrateYAMLModeCLI); err != nil {
		t.Fatal(err)
	}

	doc, err := st.GetAppConfigDocument(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if doc == nil || doc.Revision < 1 || doc.Content == "" {
		t.Fatalf("expected app_config row, got %+v", doc)
	}
}

func TestLoadConfigFromAppConfigDBIfExists(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "boot.db")
	st, err := NewSQLiteStorage(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	yamlPath := filepath.Join(dir, "c.yaml")
	minimal := `app:
  current_exchange: binance
exchanges:
  binance:
    api_key: "k"
    secret_key: "s"
trading:
  symbols:
    - symbol: BTCUSDT
      exchange: binance
      price_interval: 100
      order_quantity: 10
      buy_window_size: 5
      sell_window_size: 5
`
	if err := os.WriteFile(yamlPath, []byte(minimal), 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("QUANTMESH_MIGRATE_APP_CONFIG_FORCE", "1")
	if _, err := MigrateYAMLToAppConfigDB(context.Background(), st, yamlPath, filepath.Join(dir, "nobots"), MigrateYAMLModeCLI); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfigFromAppConfigDBIfExists(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg == nil || cfg.App.CurrentExchange != "binance" {
		t.Fatalf("expected config from DB, got %+v", cfg)
	}
}
