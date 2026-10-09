package main

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
	"quantmesh/config"
	"quantmesh/storage"
)

type namespacedRiskCheckpointBackend struct {
	backend riskCheckpointBackend
	prefix  string
}

func (b namespacedRiskCheckpointBackend) LoadRiskCheckpoint(ctx context.Context, key string) ([]byte, int64, error) {
	return b.backend.LoadRiskCheckpoint(ctx, b.prefix+key)
}

func (b namespacedRiskCheckpointBackend) SaveRiskCheckpoint(ctx context.Context, key string, expected int64, payload []byte) error {
	return b.backend.SaveRiskCheckpoint(ctx, b.prefix+key, expected, payload)
}

func TestMySQLRetiredEquityArchiveEncryptsAndSurvivesRestart(t *testing.T) {
	dsn := os.Getenv("QUANTMESH_MYSQL_TEST_DSN")
	if dsn == "" {
		t.Skip("requires QUANTMESH_MYSQL_TEST_DSN pointing to disposable loopback MySQL")
	}
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		t.Fatal("invalid disposable MySQL fixture DSN")
	}
	host, _, err := net.SplitHostPort(cfg.Addr)
	if err != nil || cfg.Net != "tcp" || host != "127.0.0.1" || cfg.User != "root" || cfg.Passwd != "" ||
		(cfg.DBName != "quantmesh_test" && !strings.HasPrefix(cfg.DBName, "quantmesh_audit_")) {
		t.Fatal("retired-account archive test requires credential-free loopback disposable MySQL")
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.PingContext(t.Context()); err != nil {
		t.Fatal(err)
	}
	backend, err := storage.NewMySQLStorage(dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = backend.Close() })
	if err := backend.MigrateRiskCheckpoints(t.Context()); err != nil {
		t.Fatal(err)
	}
	prefix := fmt.Sprintf("retired-equity-mysql-test-%d-", time.Now().UnixNano())
	checkpointKey := prefix + retiredEquityAccountsCheckpointKey
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM risk_checkpoints WHERE checkpoint_key = ?`, checkpointKey) })
	namespaced := namespacedRiskCheckpointBackend{backend: backend, prefix: prefix}
	key := []byte("0123456789abcdef0123456789abcdef")
	oldCredentials := config.ExchangeConfig{APIKey: "mysql-retired-account-api-key", SecretKey: "mysql-retired-account-secret", Testnet: true}
	previous := buildEquityScopeSnapshot(&config.Config{Exchanges: map[string]config.ExchangeConfig{"binance": oldCredentials},
		Bots: []config.BotConfig{{Exchange: "binance", Symbol: "BTCUSDT", MarketType: "futures"}}})
	next := buildEquityScopeSnapshot(&config.Config{Exchanges: map[string]config.ExchangeConfig{}, Bots: []config.BotConfig{}})
	store := &retiredEquityAccountsStore{backend: namespaced, key: key}
	if err := store.archiveRemoved(t.Context(), previous, next, time.Date(2026, 10, 9, 1, 2, 3, 0, time.UTC)); err != nil {
		t.Fatalf("archive removed account: %v", err)
	}
	payload, revision, err := backend.LoadRiskCheckpoint(t.Context(), checkpointKey)
	if err != nil || revision != 1 || strings.Contains(string(payload), oldCredentials.APIKey) || strings.Contains(string(payload), oldCredentials.SecretKey) {
		t.Fatalf("archive must persist encrypted credentials only: revision=%d err=%v", revision, err)
	}
	if err := backend.Close(); err != nil {
		t.Fatal(err)
	}
	backend, err = storage.NewMySQLStorage(dsn)
	if err != nil {
		t.Fatal(err)
	}
	restarted := &retiredEquityAccountsStore{backend: namespacedRiskCheckpointBackend{backend: backend, prefix: prefix}, key: key}
	accounts, loadedRevision, err := restarted.load(t.Context())
	if err != nil {
		t.Fatalf("reload encrypted archive after restart: %v", err)
	}
	if loadedRevision != 1 || len(accounts) != 1 || accounts[0].Status != retiredEquityAccountPendingVerification ||
		accounts[0].credentials.APIKey != oldCredentials.APIKey || accounts[0].credentials.SecretKey != oldCredentials.SecretKey ||
		accounts[0].MarketType != "futures" || accounts[0].Scope != equityAccountScopeID("binance", oldCredentials) {
		t.Fatalf("reloaded retired-account mismatch: revision=%d account_count=%d", loadedRevision, len(accounts))
	}
}
