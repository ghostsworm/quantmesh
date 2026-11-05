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

// This fixture alone accepts MySQL 9 as well as 8. Existing MySQL-8 fixtures
// retain their version gate. It never connects without the explicit local,
// credential-free disposable-schema opt-in shared by the existing fixture.
func newMySQLReleaseResumeStores(t *testing.T) [2]*SQLStorage {
	t.Helper()
	dsn := os.Getenv("QUANTMESH_MYSQL_TEST_DSN")
	if dsn == "" {
		t.Skip("requires QUANTMESH_MYSQL_TEST_DSN and explicit temporary-schema opt-in")
	}
	cfg, err := orderReconciliationMySQLFixtureConfig(dsn, os.Getenv("QUANTMESH_MYSQL_TEST_ALLOW_DESTRUCTIVE_SCHEMA") == "1")
	if err != nil {
		t.Fatalf("release resume fixture rejected unsafe DSN: %v", err)
	}
	// The allowlisted base schema is an opt-in marker, not shared test data.
	// Connect without selecting it so a fresh local server needs no bootstrap.
	adminConfig := *cfg
	adminConfig.DBName = ""
	admin, err := sql.Open("mysql", adminConfig.FormatDSN())
	if err != nil {
		t.Fatal("open isolated MySQL fixture:", err)
	}
	t.Cleanup(func() { _ = admin.Close() })
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	t.Cleanup(cancel)
	var version string
	if err = admin.QueryRowContext(ctx, `SELECT VERSION()`).Scan(&version); err != nil {
		t.Fatal("connect local MySQL fixture:", err)
	}
	if !strings.HasPrefix(version, "8.") && !strings.HasPrefix(version, "9.") {
		t.Fatalf("release resume fixture requires MySQL 8/9, got %q", version)
	}
	t.Logf("isolated release resume fixture: MySQL %s, two independent pools", version)
	schema := fmt.Sprintf("quantmesh_audit_release_resume_%d", time.Now().UTC().UnixNano())
	if _, err = admin.ExecContext(ctx, "CREATE DATABASE `"+schema+"` CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci"); err != nil {
		t.Fatal("create temporary MySQL schema:", err)
	}
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := admin.ExecContext(cleanup, "DROP DATABASE `"+schema+"`"); err != nil {
			t.Errorf("drop temporary release resume schema: %v", err)
		}
	})
	cfg.DBName = schema
	cfg.ParseTime = true
	var stores [2]*SQLStorage
	for i := range stores {
		db, err := sql.Open("mysql", cfg.FormatDSN())
		if err != nil {
			t.Fatal(err)
		}
		db.SetMaxOpenConns(1)
		db.SetMaxIdleConns(1)
		t.Cleanup(func() { _ = db.Close() })
		stores[i] = &SQLStorage{db: db, dbType: "mysql"}
	}
	if err = migrateFundingCarryRuntimeGeneration(stores[0].db, "mysql"); err != nil {
		t.Fatal(err)
	}
	if err = stores[0].MigrateFundingRecovery(ctx); err != nil {
		t.Fatal(err)
	}
	return stores
}

func TestMySQLFundingRecoveryReleaseResumeAtomicAndReplay(t *testing.T) {
	for _, released := range []bool{false, true} {
		t.Run(fmt.Sprintf("released=%t", released), func(t *testing.T) {
			f := prepareReleaseResumeFixture(t, newMySQLReleaseResumeStores(t), released)
			testReleaseResumeAtomicAndReplay(t, f)
		})
	}
}

func TestMySQLFundingRecoveryReleaseResumeConcurrentPools(t *testing.T) {
	for _, sameOperation := range []bool{true, false} {
		t.Run(fmt.Sprintf("sameOperation=%t", sameOperation), func(t *testing.T) {
			f := prepareReleaseResumeFixture(t, newMySQLReleaseResumeStores(t), false)
			testReleaseResumeConcurrentPools(t, f, sameOperation)
		})
	}
}
