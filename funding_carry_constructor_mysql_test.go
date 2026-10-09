package main

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
	"quantmesh/config"
	"quantmesh/storage"
)

func TestMySQLFundingCarryFullConstructorBorrowReceiptRetainsCapitalAndOwnership(t *testing.T) {
	constructorMySQLConfig(t)
	testFundingCarryConstructorReceiptWithStorage(t, false, newConstructorMySQLStorage)
}

func TestMySQLFundingCarryFullConstructorRepaymentReceiptRetainsCapitalAndOwnership(t *testing.T) {
	constructorMySQLConfig(t)
	testFundingCarryConstructorReceiptWithStorage(t, true, newConstructorMySQLStorage)
}

func TestMySQLFundingCarryFullConstructorFinalVerificationReleasesOnlyAfterReadonlyRetry(t *testing.T) {
	constructorMySQLConfig(t) // Missing DSN skips the parent, not hidden children.
	testFundingCarryConstructorFinalVerificationWithStorage(t, newConstructorMySQLStorage)
}

func TestMySQLFundingCarryFullConstructorCommittedCASFailureRecoversWithoutFinancialReplay(t *testing.T) {
	constructorMySQLConfig(t)
	testFundingCarryConstructorFinalVerificationWithFaults(t, newConstructorMySQLStorage, []string{"ack_error", "cancelled_commit"})
}

func TestMySQLFundingCarryFullConstructorCleanupFailureRetainsCapital(t *testing.T) {
	constructorMySQLConfig(t)
	for _, all := range []bool{false, true} {
		t.Run(map[bool]string{false: "StopBot", true: "StopAll"}[all], func(t *testing.T) { testFundingCarryConstructorCleanupWithStorage(t, newConstructorMySQLStorage, all) })
	}
}

func TestMySQLFundingCarryFullConstructorStreamCleanupAndFinalVerificationRecoverWithoutFinancialReplay(t *testing.T) {
	constructorMySQLConfig(t)
	testFundingCarryConstructorFinalVerificationWithFaults(t, newConstructorMySQLStorage, []string{"cleanup"})
}

func freezeConstructorMySQLDiagnosticClock(t *testing.T, bm *BotManager) {
	t.Helper()
	constructorMySQLConfig(t)
	cfg, err := mysql.ParseDSN(bm.cfg.Storage.Path)
	if err != nil || !strings.HasPrefix(cfg.DBName, "quantmesh_ctor_") {
		t.Fatal("diagnostic trigger requires isolated constructor schema")
	}
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	if _, err := db.ExecContext(t.Context(), `CREATE TRIGGER freeze_diagnostic_clock BEFORE UPDATE ON strategy_runtime_states FOR EACH ROW SET NEW.updated_at = OLD.updated_at`); err != nil {
		t.Fatal(err)
	}
}

// Each case gets a fresh schema: retained claims must not collide with other
// cases, and the assertions must inspect all three claims without filtering.
func constructorMySQLConfig(t *testing.T) *mysql.Config {
	t.Helper()
	dsn := os.Getenv("QUANTMESH_MYSQL_TEST_DSN")
	if dsn == "" || os.Getenv("QUANTMESH_MYSQL_TEST_ALLOW_DESTRUCTIVE_SCHEMA") != "1" {
		t.Skip("requires disposable MySQL DSN and explicit schema creation/deletion opt-in")
	}
	dbConfig, err := mysql.ParseDSN(dsn)
	if err != nil {
		t.Fatal("invalid disposable MySQL fixture DSN")
	}
	host, _, err := net.SplitHostPort(dbConfig.Addr)
	if err != nil || dbConfig.Net != "tcp" || host != "127.0.0.1" || dbConfig.User != "root" || dbConfig.Passwd != "" ||
		(dbConfig.DBName != "quantmesh_test" && !strings.HasPrefix(dbConfig.DBName, "quantmesh_audit_")) {
		t.Fatal("constructor fixture requires credential-free loopback disposable MySQL")
	}
	dbConfig.Timeout, dbConfig.ReadTimeout, dbConfig.WriteTimeout = 5*time.Second, 15*time.Second, 15*time.Second
	return dbConfig
}

func newConstructorMySQLStorage(t *testing.T) *BotManager {
	t.Helper()
	dbConfig := constructorMySQLConfig(t)
	admin, err := sql.Open("mysql", dbConfig.FormatDSN())
	if err != nil {
		t.Fatal("open disposable MySQL admin connection failed")
	}
	t.Cleanup(func() {
		if err := admin.Close(); err != nil {
			t.Error(err)
		}
	})
	schema := fmt.Sprintf("quantmesh_ctor_%d", time.Now().UnixNano())
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	if _, err := admin.ExecContext(ctx, "CREATE DATABASE `"+schema+"` CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci"); err != nil {
		t.Fatalf("create isolated constructor schema: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, stop := context.WithTimeout(context.Background(), 20*time.Second)
		defer stop()
		if _, err := admin.ExecContext(cleanupCtx, "DROP DATABASE `"+schema+"`"); err != nil {
			t.Errorf("remove isolated constructor schema: %v", err)
		}
	})
	dbConfig.DBName = schema
	cfg := &config.Config{}
	cfg.Storage.Enabled, cfg.Storage.Type, cfg.Storage.Path = true, "mysql", dbConfig.FormatDSN()
	cfg.Storage.BufferSize, cfg.Storage.BatchSize = 1, 1
	service, err := storage.NewStorageService(cfg, t.Context())
	if err != nil {
		t.Fatalf("initialize production MySQL storage service: %v", err)
	}
	t.Cleanup(service.Stop)
	bm := NewBotManager(cfg, nil, service, nil, "")
	bm.botStatesFileOverride = filepath.Join(t.TempDir(), "fallback.json")
	return bm
}
