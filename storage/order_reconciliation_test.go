package storage

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
	"quantmesh/accounting"
)

func TestOrderReconciliationSQLiteDurableCaseAndAudit(t *testing.T) {
	ctx := context.Background()
	s, err := NewSQLStorage(filepath.Join(t.TempDir(), "reconciliation.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.MigrateOrderReconciliations(ctx); err != nil {
		t.Fatal(err)
	}
	input := NewOrderReconciliationCase{ID: "case-1", IdempotencyKey: "request-1", Owner: testReconciliationOwner(), ExpectedIntentRevision: "intent-rev-2", ExpectedStrategyRevision: "strategy-rev-7", Reason: "manual review", Actor: "admin", EvidenceRefs: []string{"incident-42"}}
	created, replayed, err := s.CreateOrderReconciliationCase(ctx, input)
	if err != nil || replayed {
		t.Fatalf("create failed: replayed=%t err=%v", replayed, err)
	}
	if created.Status != OrderReconciliationPrepared || created.Revision != 1 || created.Receipt != nil {
		t.Fatalf("unexpected prepared case: %+v", created)
	}
	retry := input
	retry.ID = "another-id"
	got, replayed, err := s.CreateOrderReconciliationCase(ctx, retry)
	if err != nil || !replayed || got.ID != created.ID {
		t.Fatalf("idempotent retry mismatch: got=%+v replayed=%t err=%v", got, replayed, err)
	}
	fill := accounting.VerifiedFill{TradeID: "trade-1", CursorSequence: 1, Side: "BUY", PositionSide: "LONG", OrderRole: "entry", Price: "10", Quantity: "0.1", CommissionAmount: "0", CommissionAsset: "USDT", TradeTime: time.Now().UTC()}
	manual := accounting.ReconcileRequest{OperationID: created.ID, Owner: input.Owner, ExpectedRevision: input.ExpectedStrategyRevision, From: accounting.Cursor{}, Target: accounting.Cursor{Sequence: 1, TradeID: "trade-1"}, Fills: []accounting.VerifiedFill{fill}, Evidence: accounting.EvidenceSummary{Source: "fake", Summary: "verified", VerifiedBy: "admin", VerifiedAt: time.Now().UTC()}}
	running, err := s.StartOrderReconciliation(ctx, created.ID, created.Revision, "admin", manual, strings.Repeat("a", 64))
	if err != nil || running.Revision != 2 || running.OperationRequest == nil || running.OperationRequest.OperationID != created.ID {
		t.Fatalf("start transition did not durably bind request: item=%+v err=%v", running, err)
	}
	receipt := OrderReconciliationReceipt{Outcome: "reconciled", OperationID: created.ID, Owner: input.Owner, ExpectedRevision: input.ExpectedStrategyRevision, From: manual.From, Target: manual.Target, Fills: manual.Fills, Evidence: manual.Evidence, Revision: "strategy-rev-8", Result: accounting.EconomicResult{NetQuantity: "0.1"}, CompletedAt: time.Now().UTC()}
	ready, err := s.TransitionOrderReconciliation(ctx, created.ID, running.Revision, OrderReconciliationRunning, OrderReconciliationReady, "admin", "test_transition", "verified", []byte(`{"ok":true}`), &receipt, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	if err != nil || ready.Revision != 3 {
		t.Fatalf("verified transition failed: item=%+v err=%v", ready, err)
	}
	if _, err := s.TransitionOrderReconciliation(ctx, created.ID, running.Revision, OrderReconciliationRunning, OrderReconciliationReady, "admin", "duplicate", "verified", []byte(`{}`), &receipt, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"); err != ErrOrderReconciliationConflict {
		t.Fatalf("stale transition should conflict, got %v", err)
	}
	audit, err := s.ListOrderReconciliationAudit(ctx, created.ID)
	if err != nil || len(audit) != 3 || audit[0].Action != "case_created" || audit[1].Action != "reconcile_confirmed" || audit[2].Action != "test_transition" {
		t.Fatalf("unexpected audit: %+v err=%v", audit, err)
	}
	readback, found, err := s.GetOrderReconciliation(ctx, created.ID)
	if err != nil || !found || readback.Status != OrderReconciliationReady || readback.OperationRequest == nil || readback.Receipt == nil {
		t.Fatalf("durable readback failed: %+v found=%t err=%v", readback, found, err)
	}
}

func TestOrderReconciliationMigrationIdempotentAndMySQLReceiptNullable(t *testing.T) {
	ctx := context.Background()
	s, err := NewSQLStorage(filepath.Join(t.TempDir(), "repeat.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.MigrateOrderReconciliations(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.MigrateOrderReconciliations(ctx); err != nil {
		t.Fatalf("migration should be idempotent: %v", err)
	}
	item, found, err := s.scanOrderReconciliation(s.db.QueryRowContext(ctx, orderReconciliationSelect+` WHERE case_id=?`, "missing"))
	if err != nil || found || item.ID != "" {
		t.Fatalf("nullable receipt/missing-row scan failed: %+v found=%t err=%v", item, found, err)
	}
}

func TestOrderReconciliationMigrationRejectsIncompatibleExistingSQLiteColumn(t *testing.T) {
	ctx := context.Background()
	store := openReconciliationSQLiteStore(t, filepath.Join(t.TempDir(), "incompatible-reconciliation.db"))
	body, err := orderReconciliationMigrations.ReadFile("migrations/2026101001_order_reconciliation_sqlite.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range strings.Split(string(body), ";") {
		if statement = strings.TrimSpace(statement); statement != "" {
			if _, err := store.db.ExecContext(ctx, statement); err != nil {
				t.Fatalf("prepare base schema: %v", err)
			}
		}
	}
	if _, err := store.db.ExecContext(ctx, `ALTER TABLE order_reconciliation_cases ADD COLUMN expected_intent_revision INTEGER NOT NULL DEFAULT 0`); err != nil {
		t.Fatalf("prepare incompatible same-name column: %v", err)
	}
	if err := store.MigrateOrderReconciliations(ctx); err == nil || !strings.Contains(err.Error(), "incompatible schema definition") {
		t.Fatalf("migration must reject incompatible existing column: %v", err)
	}
}

func TestOrderReconciliationSQLiteMigrationWaitIsCancelableAndRetryable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cancelable-reconciliation-migration.db")
	blocker := openReconciliationSQLiteStore(t, path)
	contender := openReconciliationSQLiteStore(t, path)
	lockConn, err := blocker.db.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer lockConn.Close()
	if _, err := lockConn.ExecContext(context.Background(), `BEGIN EXCLUSIVE`); err != nil {
		t.Fatalf("hold SQLite migration write lock: %v", err)
	}
	defer lockConn.ExecContext(context.Background(), `ROLLBACK`)

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	started := time.Now()
	err = contender.MigrateOrderReconciliations(ctx)
	if err == nil {
		t.Fatal("migration waiting on another initializer must return the context timeout")
	}
	if elapsed := time.Since(started); elapsed > 3*time.Second {
		t.Fatalf("migration did not honor caller cancellation: elapsed=%s err=%v", elapsed, err)
	}
	if _, err := lockConn.ExecContext(context.Background(), `ROLLBACK`); err != nil {
		t.Fatalf("release SQLite migration lock: %v", err)
	}
	if err := contender.MigrateOrderReconciliations(context.Background()); err != nil {
		t.Fatalf("migration retry after canceled wait must succeed: %v", err)
	}
}

func TestOrderReconciliationMigrationConcurrentSQLiteConnections(t *testing.T) {
	testConcurrentSQLiteReconciliationMigration(t, false)
}

func TestOrderReconciliationRevisionMigrationConcurrentSQLiteConnections(t *testing.T) {
	testConcurrentSQLiteReconciliationMigration(t, true)
}

func testConcurrentSQLiteReconciliationMigration(t *testing.T, legacySchema bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	path := filepath.Join(t.TempDir(), "concurrent-reconciliation.db")
	if legacySchema {
		store := openReconciliationSQLiteStore(t, path)
		body, err := orderReconciliationMigrations.ReadFile("migrations/2026101001_order_reconciliation_sqlite.up.sql")
		if err != nil {
			t.Fatal(err)
		}
		for _, statement := range strings.Split(string(body), ";") {
			if statement = strings.TrimSpace(statement); statement != "" {
				if _, err := store.db.ExecContext(ctx, statement); err != nil {
					t.Fatalf("prepare legacy reconciliation schema: %v", err)
				}
			}
		}
		_ = store.Close()
	}

	const workers = 12
	stores := make([]*SQLStorage, workers)
	for i := range stores {
		store := openReconciliationSQLiteStore(t, path)
		stores[i] = store
		t.Cleanup(func() { _ = store.Close() })
	}
	start := make(chan struct{})
	errs := make(chan error, workers)
	var group sync.WaitGroup
	for _, store := range stores {
		group.Add(1)
		go func(store *SQLStorage) {
			defer group.Done()
			<-start
			errs <- store.MigrateOrderReconciliations(ctx)
		}(store)
	}
	close(start)
	group.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent reconciliation migration failed (legacy=%t): %v", legacySchema, err)
		}
	}

	assertSQLiteReconciliationSchema(t, ctx, stores[0].db)
}

func assertSQLiteReconciliationSchema(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	for table, expectedColumns := range map[string][]string{
		"order_reconciliation_cases": {"case_id", "idempotency_key", "owner_json", "expected_revision", "expected_intent_revision", "expected_strategy_revision", "reason", "evidence_refs_json", "status", "revision", "operation_request_json", "receipt_json", "release_evidence_hash", "operation_evidence_hash", "created_by", "created_at", "updated_at"},
		"order_reconciliation_audit": {"audit_id", "case_id", "case_revision", "action", "actor", "outcome", "detail_json", "created_at"},
	} {
		rows, err := db.QueryContext(ctx, `PRAGMA table_info(`+table+`)`)
		if err != nil {
			t.Fatalf("read %s columns: %v", table, err)
		}
		columns := make(map[string]bool)
		for rows.Next() {
			var index, notNull, primary int
			var name, dataType string
			var defaultValue any
			if err := rows.Scan(&index, &name, &dataType, &notNull, &defaultValue, &primary); err != nil {
				_ = rows.Close()
				t.Fatalf("scan %s column: %v", table, err)
			}
			columns[name] = true
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			t.Fatalf("iterate %s columns: %v", table, err)
		}
		_ = rows.Close()
		for _, name := range expectedColumns {
			if !columns[name] {
				t.Errorf("required column %s.%s missing", table, name)
			}
		}
	}
	for _, name := range []string{"idx_order_reconciliation_status_created", "idx_order_reconciliation_audit_case"} {
		var count int
		if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name=?`, name).Scan(&count); err != nil || count != 1 {
			t.Errorf("required index %q missing or duplicated: count=%d err=%v", name, count, err)
		}
	}
	rows, err := db.QueryContext(ctx, `PRAGMA index_list(order_reconciliation_cases)`)
	if err != nil {
		t.Fatal(err)
	}
	uniqueIdempotency := false
	for rows.Next() {
		var sequence, unique, partial int
		var name, origin string
		if err := rows.Scan(&sequence, &name, &unique, &origin, &partial); err != nil {
			_ = rows.Close()
			t.Fatal(err)
		}
		if name == "sqlite_autoindex_order_reconciliation_cases_2" && unique == 1 {
			uniqueIdempotency = true
		}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		t.Fatal(err)
	}
	_ = rows.Close()
	if !uniqueIdempotency {
		t.Error("unique idempotency-key index missing")
	}
}

func openReconciliationSQLiteStore(t *testing.T, path string) *SQLStorage {
	t.Helper()
	dsn := filepath.ToSlash(path) + "?_journal_mode=WAL&_busy_timeout=30000"
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	store := &SQLStorage{db: db, dbType: "sqlite"}
	t.Cleanup(func() { _ = db.Close() })
	return store
}

func TestOrderReconciliationMigrationConcurrentMySQLConnections(t *testing.T) {
	dsn := os.Getenv("QUANTMESH_MYSQL_TEST_DSN")
	if dsn == "" {
		t.Skip("requires QUANTMESH_MYSQL_TEST_DSN pointing to a disposable MySQL schema")
	}
	adminCfg, err := orderReconciliationMySQLFixtureConfig(dsn, os.Getenv("QUANTMESH_MYSQL_TEST_ALLOW_DESTRUCTIVE_SCHEMA") == "1")
	if err != nil {
		t.Skipf("MySQL reconciliation migration refused unsafe fixture DSN: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	admin, err := sql.Open("mysql", adminCfg.FormatDSN())
	if err != nil {
		t.Fatal("open MySQL fixture admin connection:", err)
	}
	t.Cleanup(func() { _ = admin.Close() })
	if err := admin.PingContext(ctx); err != nil {
		t.Fatal("connect MySQL fixture:", err)
	}
	schema := fmt.Sprintf("quantmesh_reconciliation_%d", time.Now().UTC().UnixNano())
	if _, err := admin.ExecContext(ctx, "CREATE DATABASE `"+schema+"` CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci"); err != nil {
		t.Fatal("create isolated MySQL schema:", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		if _, err := admin.ExecContext(cleanupCtx, "DROP DATABASE `"+schema+"`"); err != nil {
			t.Errorf("drop isolated MySQL schema: %v", err)
		}
	})
	fixtureCfg := *adminCfg
	fixtureCfg.DBName = schema
	legacyDB, err := sql.Open("mysql", fixtureCfg.FormatDSN())
	if err != nil {
		t.Fatal("open isolated MySQL schema:", err)
	}
	legacyStore := &SQLStorage{db: legacyDB, dbType: "mysql"}
	legacyBody, err := orderReconciliationMigrations.ReadFile("migrations/2026101001_order_reconciliation_mysql.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range strings.Split(string(legacyBody), ";") {
		if statement = strings.TrimSpace(statement); statement != "" {
			if _, err := legacyDB.ExecContext(ctx, statement); err != nil {
				t.Fatalf("prepare legacy MySQL reconciliation schema: %v", err)
			}
		}
	}
	if _, err := legacyDB.ExecContext(ctx, `ALTER TABLE order_reconciliation_cases ADD COLUMN expected_intent_revision VARCHAR(191) NOT NULL DEFAULT ''`); err != nil {
		t.Fatalf("prepare partially migrated legacy schema: %v", err)
	}
	_ = legacyStore.Close()
	stores := openConcurrentMySQLReconciliationStores(t, &fixtureCfg, 8)
	runConcurrentReconciliationMigrations(t, ctx, stores)
	assertMySQLReconciliationSchema(t, ctx, stores[0].db)

	for _, table := range []string{"order_reconciliation_audit", "order_reconciliation_cases"} {
		if _, err := stores[0].db.ExecContext(ctx, `DROP TABLE `+table); err != nil {
			t.Fatalf("reset isolated schema for concurrent cold-start test: %v", err)
		}
	}
	runConcurrentReconciliationMigrations(t, ctx, stores)
	assertMySQLReconciliationSchema(t, ctx, stores[0].db)
	for _, table := range []string{"order_reconciliation_audit", "order_reconciliation_cases"} {
		if _, err := stores[0].db.ExecContext(ctx, `DROP TABLE `+table); err != nil {
			t.Fatalf("reset isolated schema for partial-upgrade test: %v", err)
		}
	}
	for _, statement := range strings.Split(string(legacyBody), ";") {
		if statement = strings.TrimSpace(statement); statement != "" {
			if _, err := stores[0].db.ExecContext(ctx, statement); err != nil {
				t.Fatalf("recreate partial legacy schema: %v", err)
			}
		}
	}
	if _, err := stores[0].db.ExecContext(ctx, `ALTER TABLE order_reconciliation_cases ADD COLUMN expected_intent_revision VARCHAR(191) NOT NULL DEFAULT ''`); err != nil {
		t.Fatalf("prepare partially migrated schema: %v", err)
	}
	runConcurrentReconciliationMigrations(t, ctx, stores)
	assertMySQLReconciliationSchema(t, ctx, stores[0].db)
	testMySQLReconciliationMigrationLockWait(t, stores[0])

	if _, err := stores[0].db.ExecContext(ctx, `ALTER TABLE order_reconciliation_cases MODIFY COLUMN expected_intent_revision VARCHAR(190) NOT NULL DEFAULT ''`); err != nil {
		t.Fatalf("prepare incompatible same-name column: %v", err)
	}
	if err := stores[0].MigrateOrderReconciliations(ctx); err == nil {
		t.Fatal("migration must reject an existing revision column with an incompatible definition")
	}
	if _, err := stores[0].db.ExecContext(ctx, `ALTER TABLE order_reconciliation_cases MODIFY COLUMN expected_intent_revision VARCHAR(191) NOT NULL DEFAULT ''`); err != nil {
		t.Fatalf("restore exact revision column schema: %v", err)
	}
	if err := stores[0].MigrateOrderReconciliations(ctx); err != nil {
		t.Fatalf("migration lock was not released after schema validation error: %v", err)
	}
}

func testMySQLReconciliationMigrationLockWait(t *testing.T, store *SQLStorage) {
	t.Helper()
	lockConn, err := store.db.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer lockConn.Close()
	var acquired sql.NullInt64
	if err := lockConn.QueryRowContext(context.Background(), `SELECT GET_LOCK(?, 1)`, orderReconciliationMySQLLockName).Scan(&acquired); err != nil || !acquired.Valid || acquired.Int64 != 1 {
		t.Fatalf("hold MySQL reconciliation migration lock: acquired=%+v err=%v", acquired, err)
	}
	defer func() {
		var released sql.NullInt64
		if err := lockConn.QueryRowContext(context.Background(), `SELECT RELEASE_LOCK(?)`, orderReconciliationMySQLLockName).Scan(&released); err != nil || !released.Valid || released.Int64 != 1 {
			t.Errorf("release held test migration lock: released=%+v err=%v", released, err)
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	started := time.Now()
	err = store.MigrateOrderReconciliations(ctx)
	if err == nil {
		t.Fatal("migration blocked by another MySQL initializer must fail on context deadline")
	}
	if elapsed := time.Since(started); elapsed > 3*time.Second {
		t.Fatalf("MySQL migration lock wait ignored context deadline: elapsed=%s err=%v", elapsed, err)
	}
}

func openConcurrentMySQLReconciliationStores(t *testing.T, cfg *mysql.Config, count int) []*SQLStorage {
	t.Helper()
	stores := make([]*SQLStorage, count)
	for i := range stores {
		db, err := sql.Open("mysql", cfg.FormatDSN())
		if err != nil {
			t.Fatal(err)
		}
		db.SetMaxOpenConns(2)
		db.SetMaxIdleConns(1)
		stores[i] = &SQLStorage{db: db, dbType: "mysql"}
		t.Cleanup(func() { _ = db.Close() })
	}
	return stores
}

func runConcurrentReconciliationMigrations(t *testing.T, ctx context.Context, stores []*SQLStorage) {
	t.Helper()
	start := make(chan struct{})
	errs := make(chan error, len(stores))
	var group sync.WaitGroup
	for _, store := range stores {
		group.Add(1)
		go func(store *SQLStorage) {
			defer group.Done()
			<-start
			errs <- store.MigrateOrderReconciliations(ctx)
		}(store)
	}
	close(start)
	group.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent reconciliation migration failed: %v", err)
		}
	}
}

func TestOrderReconciliationMySQLFixtureGuard(t *testing.T) {
	tests := []struct {
		name   string
		dsn    string
		allow  bool
		should bool
	}{
		{name: "allow disposable loopback schema with opt-in", dsn: "root@tcp(127.0.0.1:3306)/quantmesh_test", allow: true, should: true},
		{name: "allow disposable audit schema with opt-in", dsn: "root@tcp(127.0.0.1:3306)/quantmesh_audit_fixture", allow: true, should: true},
		{name: "refuse missing opt-in", dsn: "root@tcp(127.0.0.1:3306)/quantmesh_test"},
		{name: "refuse other schema", dsn: "root@tcp(127.0.0.1:3306)/production", allow: true},
		{name: "refuse remote host", dsn: "root@tcp(db.example.test:3306)/quantmesh_test", allow: true},
		{name: "refuse password credential", dsn: "root:secret@tcp(127.0.0.1:3306)/quantmesh_test", allow: true},
		{name: "refuse malformed DSN", dsn: "not a mysql dsn", allow: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := orderReconciliationMySQLFixtureConfig(tt.dsn, tt.allow)
			if tt.should && err != nil {
				t.Fatalf("safe fixture rejected: %v", err)
			}
			if !tt.should && err == nil {
				t.Fatalf("unsafe fixture accepted: config=%+v", cfg)
			}
		})
	}
}

func orderReconciliationMySQLFixtureConfig(dsn string, allowDestructive bool) (*mysql.Config, error) {
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		return nil, fmt.Errorf("invalid MySQL fixture DSN")
	}
	host, _, hostErr := net.SplitHostPort(cfg.Addr)
	if hostErr != nil || cfg.Net != "tcp" || host != "127.0.0.1" || cfg.User != "root" || cfg.Passwd != "" {
		return nil, fmt.Errorf("fixture must use credential-free loopback MySQL")
	}
	if cfg.DBName != "quantmesh_test" && !strings.HasPrefix(cfg.DBName, "quantmesh_audit_") {
		return nil, fmt.Errorf("fixture schema is not on the disposable-schema allowlist")
	}
	if !allowDestructive {
		return nil, fmt.Errorf("explicit QUANTMESH_MYSQL_TEST_ALLOW_DESTRUCTIVE_SCHEMA=1 opt-in is required")
	}
	return cfg, nil
}

func assertMySQLReconciliationSchema(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	for table, expectedColumns := range map[string][]string{
		"order_reconciliation_cases": {"case_id", "idempotency_key", "owner_json", "expected_revision", "expected_intent_revision", "expected_strategy_revision", "reason", "evidence_refs_json", "status", "revision", "operation_request_json", "receipt_json", "release_evidence_hash", "operation_evidence_hash", "created_by", "created_at", "updated_at"},
		"order_reconciliation_audit": {"audit_id", "case_id", "case_revision", "action", "actor", "outcome", "detail_json", "created_at"},
	} {
		for _, column := range expectedColumns {
			var count int
			if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM information_schema.COLUMNS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME=? AND COLUMN_NAME=?`, table, column).Scan(&count); err != nil || count != 1 {
				t.Errorf("required MySQL column %s.%s missing or duplicated: count=%d err=%v", table, column, count, err)
			}
		}
	}
	for _, index := range []string{"PRIMARY", "uk_order_reconciliation_idempotency", "idx_order_reconciliation_status_created"} {
		var count int
		if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM information_schema.STATISTICS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='order_reconciliation_cases' AND INDEX_NAME=?`, index).Scan(&count); err != nil || count == 0 {
			t.Errorf("required MySQL index %q missing: count=%d err=%v", index, count, err)
		}
	}
	var auditIndex int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(DISTINCT INDEX_NAME) FROM information_schema.STATISTICS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='order_reconciliation_audit' AND INDEX_NAME='idx_order_reconciliation_audit_case'`).Scan(&auditIndex); err != nil || auditIndex != 1 {
		t.Errorf("required MySQL audit index missing: count=%d err=%v", auditIndex, err)
	}
}

func testReconciliationOwner() accounting.OrderOwner {
	return accounting.OrderOwner{Exchange: "fake", Market: "spot", AccountScope: "acct-1", Bot: "bot-1", Symbol: "BTCUSDT", StrategyName: "trend", StrategyType: "trend", ClientOrderID: "cid-1", VenueOrderID: "42"}
}
