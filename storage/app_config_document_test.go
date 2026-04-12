package storage

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"quantmesh/config"

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
	s := &SQLStorage{db: db, dbType: "sqlite"}
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
	st, err := NewSQLStorage(dbPath)
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
	st, err := NewSQLStorage(dbPath)
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

func TestRefreshTradingConfigFromPrimarySource_DBOverwritesYAML(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "t.db")
	st, err := NewSQLStorage(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	yamlPath := filepath.Join(dir, "base.yaml")
	base := `app:
  current_exchange: binance
exchanges:
  binance:
    api_key: "k"
    secret_key: "s"
    fee_rate: 0.001
trading:
  symbols:
    - symbol: BTCUSDT
      exchange: binance
      price_interval: 100
      order_quantity: 10
      buy_window_size: 5
      sell_window_size: 5
`
	if err := os.WriteFile(yamlPath, []byte(base), 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("QUANTMESH_MIGRATE_APP_CONFIG_FORCE", "1")
	if _, err := MigrateYAMLToAppConfigDB(context.Background(), st, yamlPath, filepath.Join(dir, "nobots"), MigrateYAMLModeCLI); err != nil {
		t.Fatal(err)
	}

	cfgFromDB, err := loadConfigFromAppConfigDocument(st)
	if err != nil || cfgFromDB == nil {
		t.Fatalf("load from db: %v cfg=%v", err, cfgFromDB)
	}
	ex := cfgFromDB.Exchanges["binance"]
	ex.FeeRate = 0.0005
	cfgFromDB.Exchanges["binance"] = ex
	if _, err := SaveAppConfigSnapshot(context.Background(), st, cfgFromDB, "test", "t"); err != nil {
		t.Fatal(err)
	}

	cfgPtr, err := config.LoadConfig(yamlPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfgPtr.Exchanges["binance"].FeeRate != 0.001 {
		t.Fatalf("yaml fee should be 0.001")
	}
	if err := RefreshTradingConfigFromPrimarySource(yamlPath, st, &cfgPtr); err != nil {
		t.Fatal(err)
	}
	if cfgPtr.Exchanges["binance"].FeeRate != 0.0005 {
		t.Fatalf("expected DB fee 0.0005, got %v", cfgPtr.Exchanges["binance"].FeeRate)
	}
}
