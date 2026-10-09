package storage

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
)

// Independent schema; the clock-freezing trigger must never touch another
// fixture's table. It models equal DATETIME(3) timestamps deterministically.
func verifyMySQLRuntimeStateNoopCAS(t *testing.T, dsn string) {
	t.Helper()
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		t.Fatal("invalid disposable MySQL fixture DSN")
	}
	host, _, err := net.SplitHostPort(cfg.Addr)
	if err != nil || cfg.Net != "tcp" || host != "127.0.0.1" || cfg.User != "root" || cfg.Passwd != "" ||
		(cfg.DBName != "quantmesh_test" && !strings.HasPrefix(cfg.DBName, "quantmesh_audit_")) || os.Getenv("QUANTMESH_MYSQL_TEST_ALLOW_DESTRUCTIVE_SCHEMA") != "1" {
		t.Fatal("no-op fixture requires credential-free loopback MySQL and explicit disposable schema opt-in")
	}
	admin, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = admin.Close() })
	schema := fmt.Sprintf("quantmesh_cas_noop_%d", time.Now().UnixNano())
	if _, err := admin.ExecContext(t.Context(), "CREATE DATABASE `"+schema+"` CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := admin.ExecContext(ctx, "DROP DATABASE `"+schema+"`"); err != nil {
			t.Error(err)
		}
	})
	cfg.DBName = schema
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := migrateStrategyRuntimeStateTableMySQL(db); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(t.Context(), `CREATE TRIGGER freeze_diagnostic_clock BEFORE UPDATE ON strategy_runtime_states FOR EACH ROW SET NEW.updated_at = OLD.updated_at`); err != nil {
		t.Fatal(err)
	}
	store := &SQLStorage{db: db, dbType: "mysql"}
	seed := &StrategyRuntimeState{BotID: "noop-owner", StrategyName: "funding_carry", SchemaVersion: 7, Payload: `{"clean":true}`}
	if err := store.SetStrategyRuntimeState(seed); err != nil {
		t.Fatal(err)
	}
	if saved, err := store.CompareAndSwapStrategyRuntimeState(t.Context(), seed, 7, seed.Payload); err != nil || !saved {
		t.Fatalf("exact no-op source was treated as conflict: saved=%v err=%v", saved, err)
	}
	for _, payload := range []string{`{"clean":false}`, `{"CLEAN":true}`} {
		next := *seed
		next.Payload = payload
		if saved, err := store.CompareAndSwapStrategyRuntimeState(t.Context(), &next, 7, payload); err != nil || saved {
			t.Fatal("no-op fallback accepted stale or case-insensitive provenance", err)
		}
	}
	missing := *seed
	missing.BotID = "missing-owner"
	if saved, err := store.CompareAndSwapStrategyRuntimeState(t.Context(), &missing, 7, missing.Payload); err != nil || saved {
		t.Fatal("no-op fallback created missing checkpoint", err)
	}
	wrongSchema := *seed
	wrongSchema.SchemaVersion = 6
	if saved, err := store.CompareAndSwapStrategyRuntimeState(t.Context(), &wrongSchema, 6, wrongSchema.Payload); err != nil || saved {
		t.Fatal("no-op fallback accepted wrong schema", err)
	}
	loaded, err := store.GetStrategyRuntimeState(seed.BotID, seed.StrategyName)
	if err != nil || loaded == nil || loaded.Payload != seed.Payload || loaded.SchemaVersion != seed.SchemaVersion {
		t.Fatal("no-op proof altered canonical checkpoint", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if saved, err := store.CompareAndSwapStrategyRuntimeState(ctx, seed, 7, seed.Payload); err == nil || saved {
		t.Fatal("cancelled no-op proof accepted")
	}
}
