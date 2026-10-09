package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
)

func TestMySQLFundingCarryRuntimeGenerationFencesOldOwner(t *testing.T) {
	dsn := os.Getenv("QUANTMESH_MYSQL_TEST_DSN")
	if dsn == "" {
		t.Skip("requires QUANTMESH_MYSQL_TEST_DSN pointing to a disposable MySQL schema")
	}
	adminCfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		t.Fatal("invalid MySQL fixture DSN")
	}
	host, _, hostErr := net.SplitHostPort(adminCfg.Addr)
	if hostErr != nil || adminCfg.Net != "tcp" || host != "127.0.0.1" || adminCfg.User != "root" || adminCfg.Passwd != "" ||
		(adminCfg.DBName != "quantmesh_test" && !strings.HasPrefix(adminCfg.DBName, "quantmesh_audit_")) || os.Getenv("QUANTMESH_MYSQL_TEST_ALLOW_DESTRUCTIVE_SCHEMA") != "1" {
		t.Skip("MySQL fencing test requires credential-free loopback disposable schema and QUANTMESH_MYSQL_TEST_ALLOW_DESTRUCTIVE_SCHEMA=1")
	}
	admin, err := sql.Open("mysql", adminCfg.FormatDSN())
	if err != nil {
		t.Fatal("open MySQL fixture admin connection:", err)
	}
	t.Cleanup(func() { _ = admin.Close() })
	if err := admin.PingContext(t.Context()); err != nil {
		t.Fatal("connect MySQL fixture:", err)
	}
	schema := fmt.Sprintf("quantmesh_fc_generation_%d", time.Now().UTC().UnixNano())
	if _, err := admin.ExecContext(t.Context(), "CREATE DATABASE `"+schema+"` CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci"); err != nil {
		t.Fatal("create isolated MySQL schema:", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := admin.ExecContext(ctx, "DROP DATABASE `"+schema+"`"); err != nil {
			t.Errorf("drop isolated MySQL schema: %v", err)
		}
	})
	adminCfg.DBName = schema
	db, err := sql.Open("mysql", adminCfg.FormatDSN())
	if err != nil {
		t.Fatal("open isolated MySQL schema:", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.PingContext(t.Context()); err != nil {
		t.Fatal("connect isolated MySQL schema:", err)
	}
	if err := migrateStrategyRuntimeStateTableMySQL(db); err != nil {
		t.Fatal("migrate runtime state:", err)
	}
	if err := migrateFundingCarryRuntimeGeneration(db, "mysql"); err != nil {
		t.Fatal("migrate owner generations:", err)
	}
	store := &SQLStorage{db: db, dbType: "mysql"}
	unique := fmt.Sprintf("%064x", time.Now().UTC().UnixNano())
	scopes := []string{unique, fmt.Sprintf("%064x", time.Now().UTC().UnixNano()+1)}
	botID := "mysql-fc-generation-" + unique[:20]
	generations := FundingCarryRuntimeGenerationStore(store)
	first, err := generations.ClaimFundingCarryRuntimeGeneration(t.Context(), scopes)
	if err != nil {
		t.Fatal("first full claim:", err)
	}
	assertMySQLFundingCarryGenerationScopes(t, db, scopes, first.ownerToken, 1)
	initial := &StrategyRuntimeState{BotID: botID, StrategyName: "funding_carry", SchemaVersion: 1, Payload: `{"stable":"mysql"}`}
	if err := generations.SetFundingCarryRuntimeState(t.Context(), first, initial); err != nil {
		t.Fatal("first owner Save:", err)
	}
	second, err := generations.ClaimFundingCarryRuntimeGeneration(t.Context(), scopes)
	if err != nil {
		t.Fatal("replacement full claim:", err)
	}
	assertMySQLFundingCarryGenerationScopes(t, db, scopes, second.ownerToken, 2)
	staleSave := *initial
	staleSave.Payload = `{"stale":"save"}`
	if err := generations.SetFundingCarryRuntimeState(t.Context(), first, &staleSave); !errors.Is(err, ErrFundingCarryRuntimeGenerationLost) {
		t.Fatalf("stale MySQL Save error = %v, want generation lost", err)
	}
	staleCAS := *initial
	staleCAS.Payload = `{"stale":"cas"}`
	if saved, err := generations.CompareAndSwapFundingCarryRuntimeState(t.Context(), first, &staleCAS, initial.SchemaVersion, initial.Payload); !errors.Is(err, ErrFundingCarryRuntimeGenerationLost) || saved {
		t.Fatalf("stale MySQL CAS = saved %v, err %v", saved, err)
	}
	loaded, err := store.GetStrategyRuntimeStateContext(t.Context(), botID, initial.StrategyName)
	if err != nil || loaded == nil || loaded.Payload != initial.Payload {
		t.Fatalf("stale MySQL writes changed original payload: %+v err=%v", loaded, err)
	}

	orderedScopes := append([]string(nil), scopes...)
	sort.Strings(orderedScopes)
	failedScope := orderedScopes[len(orderedScopes)-1]
	trigger := fmt.Sprintf(`CREATE TRIGGER reject_funding_carry_generation_advance
		BEFORE UPDATE ON funding_carry_runtime_generations FOR EACH ROW
		BEGIN
			IF NEW.generation > OLD.generation AND OLD.scope_key = '%s' THEN
				SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'injected generation claim failure';
			END IF;
		END`, failedScope)
	if _, err := db.ExecContext(t.Context(), trigger); err != nil {
		t.Fatal("install deterministic MySQL claim failure:", err)
	}
	if generation, err := generations.ClaimFundingCarryRuntimeGeneration(t.Context(), scopes); err == nil || generation.ownerToken != "" {
		t.Fatalf("partially successful MySQL claim returned generation=%+v err=%v", generation, err)
	}
	assertMySQLFundingCarryGenerationScopes(t, db, scopes, second.ownerToken, 2)
	if _, err := db.ExecContext(t.Context(), `DROP TRIGGER reject_funding_carry_generation_advance`); err != nil {
		t.Fatal("remove deterministic MySQL claim failure:", err)
	}

	current := *initial
	current.Payload = `{"current":"mysql"}`
	if err := generations.SetFundingCarryRuntimeState(t.Context(), second, &current); err != nil {
		t.Fatal("current MySQL owner Save:", err)
	}
	loaded, err = store.GetStrategyRuntimeStateContext(t.Context(), botID, current.StrategyName)
	if err != nil || loaded == nil || loaded.Payload != current.Payload {
		t.Fatalf("current MySQL owner payload was not committed: %+v err=%v", loaded, err)
	}
	currentCAS := current
	currentCAS.SchemaVersion++
	currentCAS.Payload = `{"current":"mysql-cas"}`
	if saved, err := generations.CompareAndSwapFundingCarryRuntimeState(t.Context(), second, &currentCAS, current.SchemaVersion, current.Payload); err != nil || !saved {
		t.Fatalf("current MySQL owner CAS = saved %v, err %v", saved, err)
	}
	loaded, err = store.GetStrategyRuntimeStateContext(t.Context(), botID, currentCAS.StrategyName)
	if err != nil || loaded == nil || loaded.SchemaVersion != currentCAS.SchemaVersion || loaded.Payload != currentCAS.Payload {
		t.Fatalf("current MySQL owner CAS payload was not committed: %+v err=%v", loaded, err)
	}
	for _, migration := range []string{
		"migrations/2026100702_funding_carry_runtime_state_receipts_mysql.down.sql",
		"migrations/2026100701_funding_carry_runtime_generation_mysql.down.sql",
	} {
		down, err := os.ReadFile(migration)
		if err != nil {
			t.Fatalf("read MySQL down migration %s: %v", migration, err)
		}
		if _, err := db.ExecContext(t.Context(), string(down)); err != nil {
			t.Fatalf("apply MySQL down migration %s: %v", migration, err)
		}
	}
	for _, table := range []string{"funding_carry_runtime_state_write_receipts", "funding_carry_runtime_generations"} {
		var exists int
		if err := db.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = DATABASE() AND table_name = ?`, table).Scan(&exists); err != nil || exists != 0 {
			t.Fatalf("MySQL down migration left table %s (exists=%d): %v", table, exists, err)
		}
	}
}

func assertMySQLFundingCarryGenerationScopes(t *testing.T, db *sql.DB, scopes []string, ownerToken string, wantGeneration int64) {
	t.Helper()
	for _, scope := range scopes {
		var generation int64
		var token string
		if err := db.QueryRowContext(t.Context(), `SELECT generation, owner_token FROM funding_carry_runtime_generations WHERE scope_key = ?`, scope).Scan(&generation, &token); err != nil {
			t.Fatalf("read claimed MySQL scope %s: %v", scope, err)
		}
		if generation != wantGeneration || token != ownerToken {
			t.Fatalf("MySQL scope %s owner=(generation %d, token %s), want (generation %d, token %s)", scope, generation, token, wantGeneration, ownerToken)
		}
	}
}
