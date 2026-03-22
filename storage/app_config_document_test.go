package storage

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

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
	if err := MigrateYAMLToAppConfigDB(context.Background(), st, yamlPath, filepath.Join(dir, "nobots")); err != nil {
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
