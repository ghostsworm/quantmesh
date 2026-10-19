package storage

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
	"quantmesh/accounting"
)

func TestMySQLFundingRecoveryMigrationSerializesConcurrentCallers(t *testing.T) {
	dsn := os.Getenv("QUANTMESH_MYSQL_TEST_DSN")
	if dsn == "" {
		t.Skip("requires QUANTMESH_MYSQL_TEST_DSN and explicit disposable-schema opt-in")
	}
	adminCfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		t.Fatal("invalid MySQL fixture DSN")
	}
	host, _, hostErr := net.SplitHostPort(adminCfg.Addr)
	if hostErr != nil || adminCfg.Net != "tcp" || host != "127.0.0.1" || adminCfg.User != "root" || adminCfg.Passwd != "" ||
		(adminCfg.DBName != "quantmesh_test" && !strings.HasPrefix(adminCfg.DBName, "quantmesh_audit_")) || os.Getenv("QUANTMESH_MYSQL_TEST_ALLOW_DESTRUCTIVE_SCHEMA") != "1" {
		t.Skip("requires credential-free loopback root and explicit disposable MySQL schema opt-in")
	}
	admin, err := sql.Open("mysql", adminCfg.FormatDSN())
	if err != nil {
		t.Fatal("open MySQL fixture admin connection:", err)
	}
	t.Cleanup(func() { _ = admin.Close() })
	if err := admin.PingContext(t.Context()); err != nil {
		t.Fatal("connect MySQL fixture:", err)
	}
	schema := fmt.Sprintf("quantmesh_recovery_%d", time.Now().UTC().UnixNano())
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
	stores := make([]*SQLStorage, 2)
	for i := range stores {
		db, err := sql.Open("mysql", adminCfg.FormatDSN())
		if err != nil {
			t.Fatal("open isolated MySQL schema:", err)
		}
		db.SetMaxOpenConns(1)
		if err := db.PingContext(t.Context()); err != nil {
			t.Fatal("connect isolated MySQL schema:", err)
		}
		stores[i] = &SQLStorage{db: db, dbType: "mysql"}
		t.Cleanup(func() { _ = db.Close() })
	}
	start := make(chan struct{})
	errs := make([]error, len(stores))
	var wg sync.WaitGroup
	for i, store := range stores {
		wg.Add(1)
		go func(i int, store *SQLStorage) {
			defer wg.Done()
			<-start
			errs[i] = store.MigrateFundingRecovery(context.Background())
		}(i, store)
	}
	close(start)
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Errorf("concurrent MySQL migration caller %d: %v", i, err)
		}
	}
	var count int
	if err := stores[0].db.QueryRow(`SELECT COUNT(*) FROM information_schema.tables WHERE table_schema=? AND table_name='funding_recovery_episodes'`, schema).Scan(&count); err != nil || count != 1 {
		t.Fatalf("MySQL migration table readback count=%d err=%v", count, err)
	}
}

func TestMySQLFundingRecoveryRejectsStaleRuntimeGeneration(t *testing.T) {
	dsn := os.Getenv("QUANTMESH_MYSQL_TEST_DSN")
	if dsn == "" {
		t.Skip("requires QUANTMESH_MYSQL_TEST_DSN and explicit disposable-schema opt-in")
	}
	adminCfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		t.Fatal("invalid MySQL fixture DSN")
	}
	host, _, hostErr := net.SplitHostPort(adminCfg.Addr)
	if hostErr != nil || adminCfg.Net != "tcp" || host != "127.0.0.1" || adminCfg.User != "root" || adminCfg.Passwd != "" ||
		(adminCfg.DBName != "quantmesh_test" && !strings.HasPrefix(adminCfg.DBName, "quantmesh_audit_")) || os.Getenv("QUANTMESH_MYSQL_TEST_ALLOW_DESTRUCTIVE_SCHEMA") != "1" {
		t.Skip("requires credential-free loopback root and explicit disposable MySQL schema opt-in")
	}
	admin, err := sql.Open("mysql", adminCfg.FormatDSN())
	if err != nil {
		t.Fatal("open MySQL fixture admin connection:", err)
	}
	t.Cleanup(func() { _ = admin.Close() })
	if err := admin.PingContext(t.Context()); err != nil {
		t.Fatal("connect MySQL fixture:", err)
	}
	schema := fmt.Sprintf("quantmesh_recovery_fence_%d", time.Now().UTC().UnixNano())
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
	db.SetMaxOpenConns(2)
	t.Cleanup(func() { _ = db.Close() })
	if err := db.PingContext(t.Context()); err != nil {
		t.Fatal("connect isolated MySQL schema:", err)
	}
	if err := migrateFundingCarryRuntimeGeneration(db, "mysql"); err != nil {
		t.Fatalf("migrate runtime generation tables: %v", err)
	}
	store := &SQLStorage{db: db, dbType: "mysql", commitProbe: db}
	if err := store.MigrateFundingRecovery(t.Context()); err != nil {
		t.Fatalf("migrate recovery tables: %v", err)
	}

	scope := fmt.Sprintf("%064x", 3)
	oldOwner, err := store.ClaimFundingCarryRuntimeGeneration(t.Context(), []string{scope})
	if err != nil {
		t.Fatal("claim old generation:", err)
	}
	episode := accounting.RecoveryEpisode{
		ID: "mysql-stale-owner", Bot: "bot-mysql", Strategy: "FundingCarry",
		AccountScopes: []string{scope}, OwnerGeneration: oldOwner.ID(),
		Revision: 1, RecoveryGeneration: 1, NextActionOrdinal: 1, State: accounting.RecoveryPrepared,
		ActionPlan: []accounting.RecoveryAction{{
			ID: "borrow-1", Ordinal: 1, Kind: accounting.RecoveryActionBorrow,
			Role: "hedge-open", AccountScope: scope, Asset: "BTC", Amount: "0.01",
			OwnerGeneration: oldOwner.ID(), Status: "authorized",
		}},
	}
	if _, err := store.CreateRecoveryEpisode(t.Context(), oldOwner, episode); err != nil {
		t.Fatalf("create held episode: %v", err)
	}
	permit := accounting.OneShotPermit{
		ID: "permit-mysql-stale", EpisodeID: episode.ID, Generation: 1,
		ActionID: "borrow-1", ActionOrdinal: 1, Kind: accounting.RecoveryActionBorrow,
		AccountScope: scope, OwnerGeneration: oldOwner.ID(), LeaseID: 1,
		ExpectedRevision: 1, State: accounting.PermitAuthorized,
	}
	if _, err := store.CreateRecoveryPermit(t.Context(), oldOwner, permit); err != nil {
		t.Fatalf("create one-shot permit: %v", err)
	}
	if _, err := store.ClaimFundingCarryRuntimeGeneration(t.Context(), []string{scope}); err != nil {
		t.Fatalf("take over runtime owner generation: %v", err)
	}
	if _, maySubmit, err := store.ConsumeRecoveryPermit(t.Context(), oldOwner, permit); !errors.Is(err, ErrFundingCarryRuntimeGenerationLost) || maySubmit {
		t.Fatalf("stale owner consume = maySubmit %v, err %v; want fenced rejection", maySubmit, err)
	}
}

func TestSQLiteFundingRecoveryMigrationSerializesConcurrentCallers(t *testing.T) {
	path := filepath.Join(t.TempDir(), "funding-recovery-migration.db")
	stores := make([]*SQLStorage, 2)
	for i := range stores {
		db, err := sql.Open("sqlite3", path+"?_busy_timeout=100")
		if err != nil {
			t.Fatal(err)
		}
		db.SetMaxOpenConns(1)
		stores[i] = &SQLStorage{db: db, dbType: "sqlite"}
		t.Cleanup(func() {
			if err := db.Close(); err != nil {
				t.Errorf("close migration test database: %v", err)
			}
		})
	}
	start := make(chan struct{})
	errs := make([]error, len(stores))
	var wg sync.WaitGroup
	for i, store := range stores {
		wg.Add(1)
		go func(i int, store *SQLStorage) {
			defer wg.Done()
			<-start
			errs[i] = store.MigrateFundingRecovery(context.Background())
		}(i, store)
	}
	close(start)
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Errorf("concurrent migration caller %d: %v", i, err)
		}
	}
	var count int
	if err := stores[0].db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='funding_recovery_episodes'`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("migration table readback count=%d err=%v", count, err)
	}
}

func TestFundingRecoveryGenerationRequiresCompleteOwnerScopesAndPreservesDatabaseErrors(t *testing.T) {
	store, err := NewSQLStorage(filepath.Join(t.TempDir(), "funding-recovery-scope.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.MigrateFundingRecovery(t.Context()); err != nil {
		t.Fatalf("migrate funding recovery tables: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	scopes := []string{fmt.Sprintf("%064x", 11), fmt.Sprintf("%064x", 12)}
	generation, err := store.ClaimFundingCarryRuntimeGeneration(t.Context(), scopes)
	if err != nil {
		t.Fatalf("claim complete owner scopes: %v", err)
	}
	tx, err := store.db.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.lockFundingRecoveryGenerationForScopes(t.Context(), tx, generation, scopes[:1]); !errors.Is(err, ErrFundingRecoveryGenerationScopeMismatch) {
		t.Fatalf("subset owner scope error = %v, want exact-scope rejection", err)
	}
	_ = tx.Rollback()

	if _, err := store.db.ExecContext(t.Context(), `DROP TABLE funding_recovery_episodes`); err != nil {
		t.Fatalf("remove episode table for diagnostic assertion: %v", err)
	}
	tx, err = store.db.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.lockFundingRecoveryGenerationForEpisode(t.Context(), tx, generation, "missing-table")
	_ = tx.Rollback()
	if err == nil || errors.Is(err, accounting.ErrRecoveryConflict) || !strings.Contains(strings.ToLower(err.Error()), "no such table") {
		t.Fatalf("episode database failure = %v; want underlying SQLite table error, not a business conflict", err)
	}
}

func TestFundingRecoveryAllowsNextCursorOperationAfterProjection(t *testing.T) {
	store, err := NewSQLStorage(filepath.Join(t.TempDir(), "funding-recovery-cursor.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("close SQLite storage: %v", err)
		}
	})
	ctx := context.Background()
	if err := store.MigrateFundingRecovery(ctx); err != nil {
		t.Fatal(err)
	}
	scope := fmt.Sprintf("%064x", 42)
	generation, err := store.ClaimFundingCarryRuntimeGeneration(ctx, []string{scope})
	if err != nil {
		t.Fatal(err)
	}
	episode := accounting.RecoveryEpisode{
		ID: "episode-cursor-repeat", Bot: "bot-1", Strategy: "FundingCarry", AccountScopes: []string{scope},
		OwnerGeneration: generation.ID(), Revision: 1, RecoveryGeneration: 1, State: accounting.RecoveryPrepared,
	}
	if _, err := store.CreateRecoveryEpisode(ctx, generation, episode); err != nil {
		t.Fatal(err)
	}
	intent := accounting.RecoveryOrderIntent{
		SchemaVersion: 1, Exchange: "binance", Market: "spot", Symbol: "BTCUSDT", Side: "BUY",
		OrderType: "LIMIT", TimeInForce: "GTC", Price: "100", Quantity: "0.02", ClientOrderID: "client-1",
	}
	intentHash, err := intent.CanonicalHash()
	if err != nil {
		t.Fatal(err)
	}

	apply := func(operationID string, expectedEpisodeRevision, expectedChildRevision, sequence uint64) {
		t.Helper()
		tradeID := fmt.Sprintf("trade-%d", sequence)
		child := accounting.RecoveryChild{
			ID: "spot-order", EpisodeID: episode.ID, Role: "spot", Exchange: "binance", Market: "spot",
			AccountScope: scope, Bot: episode.Bot, Symbol: "BTCUSDT", StrategyName: episode.Strategy,
			StrategyType: "funding_carry", ClientOrderID: "client-1", VenueOrderID: "venue-1",
			OrderIntentHash: intentHash, Side: intent.Side,
			FillCursor: accounting.Cursor{Sequence: sequence, TradeID: tradeID}, State: accounting.ChildBound,
		}
		event := accounting.RecoveryEvent{
			ID: "fill-" + tradeID, Source: "binance", Exchange: "binance", Market: "spot", AccountScope: scope,
			Kind: accounting.RecoveryFillEvent, ExternalID: tradeID, EpisodeID: episode.ID, ChildID: child.ID,
			CursorSequence: sequence, Asset: "BTC", Amount: "0.01", SourceRevision: tradeID,
			SourceHash: fmt.Sprintf("%064x", sequence), ObservedAt: time.Now().UTC(),
			FillEvidence: &accounting.RecoveryFillEvidence{
				ClientOrderID: child.ClientOrderID, VenueOrderID: child.VenueOrderID, Symbol: child.Symbol,
				Side: child.Side, OrderIntentHash: child.OrderIntentHash,
				BaseAsset: "BTC", PriceRaw: "100", QuantityRaw: "0.01",
			},
		}
		op := accounting.RecoveryOperation{
			EpisodeID: episode.ID, OperationID: operationID, HashVersion: 1,
			ExpectedEpisodeRevision: expectedEpisodeRevision, ExpectedRuntimeRevision: fmt.Sprintf("runtime-%d", expectedEpisodeRevision),
			ExpectedChildRevisions: map[string]uint64{child.ID: expectedChildRevision},
			Children:               []accounting.RecoveryChild{child}, Events: []accounting.RecoveryEvent{event},
			Actor: "test-operator", AuditRef: "test-audit", EvidenceHash: fmt.Sprintf("%064x", expectedEpisodeRevision), EvidenceReadAt: time.Now().UTC(),
		}
		receipt, replayed, err := store.ApplyRecoveryOperation(ctx, generation, op)
		if err != nil || replayed {
			t.Fatalf("ApplyRecoveryOperation(%s) = receipt %+v, replayed %v, err %v", operationID, receipt, replayed, err)
		}
		if _, replayed, err = store.ApplyRecoveryOperation(ctx, generation, op); err != nil || !replayed {
			t.Fatalf("idempotent ApplyRecoveryOperation(%s): replayed=%v err=%v", operationID, replayed, err)
		}
		outbox, ok, err := store.GetRecoveryOutbox(ctx, episode.ID, operationID)
		if err != nil || !ok {
			t.Fatalf("GetRecoveryOutbox(%s): ok=%v err=%v", operationID, ok, err)
		}
		outbox.AppliedOperationID = operationID
		outbox.AppliedRevision = outbox.TargetEpisodeRevision
		outbox.AppliedHash = outbox.ProjectionHash
		outbox.State = accounting.RecoveryProjected
		if err := store.MarkRecoveryProjected(ctx, generation, outbox); err != nil {
			t.Fatalf("MarkRecoveryProjected(%s): %v", operationID, err)
		}
	}
	apply("op-1", 1, 0, 1)
	apply("op-2", 3, 1, 2)
	children, err := store.ListRecoveryChildren(ctx, episode.ID)
	if err != nil || len(children) != 1 || children[0].FillCursor.Sequence != 2 || children[0].FillCursor.TradeID != "trade-2" {
		t.Fatalf("cursor readback = %+v, err %v", children, err)
	}
}

func TestFundingRecoveryRejectsRevisionWithoutRoomForProjection(t *testing.T) {
	store, err := NewSQLStorage(filepath.Join(t.TempDir(), "funding-recovery-revision-limit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("close SQLite storage: %v", err)
		}
	})
	ctx := context.Background()
	if err := store.MigrateFundingRecovery(ctx); err != nil {
		t.Fatal(err)
	}
	scope := fmt.Sprintf("%064x", 43)
	generation, err := store.ClaimFundingCarryRuntimeGeneration(ctx, []string{scope})
	if err != nil {
		t.Fatal(err)
	}
	const expectedRevision = accounting.MaxDurableRecoveryRevision - 1
	episode := accounting.RecoveryEpisode{ID: "episode-revision-limit", Bot: "bot-revision-limit", Strategy: "funding_carry",
		AccountScopes: []string{scope}, OwnerGeneration: generation.ID(), Revision: expectedRevision, RecoveryGeneration: 1,
		State: accounting.RecoveryPrepared}
	if _, err := store.CreateRecoveryEpisode(ctx, generation, episode); err != nil {
		t.Fatal("create high-revision episode:", err)
	}
	operation := accounting.RecoveryOperation{EpisodeID: episode.ID, OperationID: "revision-limit-operation", HashVersion: 1,
		ExpectedEpisodeRevision: expectedRevision, ExpectedRuntimeRevision: "runtime-revision-limit", ExpectedChildRevisions: map[string]uint64{},
		ExpectedEventRevisions: map[string]uint64{}, Actor: "operator", AuditRef: "audit-revision-limit",
		EvidenceHash: strings.Repeat("a", 64), EvidenceReadAt: time.Now().UTC()}
	if _, replayed, err := store.ApplyRecoveryOperation(ctx, generation, operation); err == nil || replayed {
		t.Fatalf("ApplyRecoveryOperation accepted a revision without room for both commits: replayed=%t err=%v", replayed, err)
	}
	if _, found, err := store.GetRecoveryReceipt(ctx, episode.ID, operation.OperationID); err != nil || found {
		t.Fatalf("overflowing operation left a durable receipt: found=%t err=%v", found, err)
	}
	current, found, err := store.GetRecoveryEpisode(ctx, episode.ID)
	if err != nil || !found || current.Revision != expectedRevision || current.State != accounting.RecoveryPrepared {
		t.Fatalf("overflowing operation changed the episode: episode=%+v found=%t err=%v", current, found, err)
	}
}

func TestFundingRecoveryRejectsFillIdentityMismatch(t *testing.T) {
	store, err := NewSQLStorage(filepath.Join(t.TempDir(), "funding-recovery-fill-identity.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	ctx := context.Background()
	if err := store.MigrateFundingRecovery(ctx); err != nil {
		t.Fatal(err)
	}
	scope := fmt.Sprintf("%064x", 48)
	generation, err := store.ClaimFundingCarryRuntimeGeneration(ctx, []string{scope})
	if err != nil {
		t.Fatal(err)
	}
	episode := accounting.RecoveryEpisode{
		ID: "episode-fill-identity", Bot: "bot-fill-identity", Strategy: "FundingCarry", AccountScopes: []string{scope},
		OwnerGeneration: generation.ID(), Revision: 1, RecoveryGeneration: 1, State: accounting.RecoveryPrepared,
	}
	if _, err := store.CreateRecoveryEpisode(ctx, generation, episode); err != nil {
		t.Fatal(err)
	}
	intent := accounting.RecoveryOrderIntent{
		SchemaVersion: 1, Exchange: "binance", Market: "spot", Symbol: "BTCUSDT", Side: "BUY",
		OrderType: "LIMIT", TimeInForce: "GTC", Price: "100", Quantity: "0.01", ClientOrderID: "cid-identity",
	}
	intentHash, err := intent.CanonicalHash()
	if err != nil {
		t.Fatal(err)
	}
	newOperation := func(operationID string) accounting.RecoveryOperation {
		child := accounting.RecoveryChild{
			ID: "child-fill-identity", EpisodeID: episode.ID, Role: "spot", Exchange: "binance", Market: "spot",
			AccountScope: scope, Bot: episode.Bot, Symbol: "BTCUSDT", StrategyName: episode.Strategy,
			StrategyType: "funding_carry", ClientOrderID: "cid-identity", VenueOrderID: "venue-identity",
			OrderIntentHash: intentHash, Side: intent.Side, Revision: 1, State: accounting.ChildBound,
			FillCursor: accounting.Cursor{Sequence: 1, TradeID: "trade-identity"},
		}
		event := accounting.RecoveryEvent{
			ID: "event-" + operationID, Source: "binance", Exchange: child.Exchange, Market: child.Market,
			AccountScope: scope, Kind: accounting.RecoveryFillEvent, ExternalID: "trade-identity",
			EpisodeID: episode.ID, ChildID: child.ID, CursorSequence: 1, Asset: "BTC", Amount: "0.01",
			SourceRevision: "revision-1", SourceHash: fmt.Sprintf("%064x", 50), ObservedAt: time.Now().UTC(),
			FillEvidence: &accounting.RecoveryFillEvidence{
				ClientOrderID: child.ClientOrderID, VenueOrderID: child.VenueOrderID, Symbol: child.Symbol,
				Side: child.Side, OrderIntentHash: child.OrderIntentHash,
				BaseAsset: "BTC", PriceRaw: "100", QuantityRaw: "0.01",
			},
		}
		return accounting.RecoveryOperation{
			EpisodeID: episode.ID, OperationID: operationID, HashVersion: 1, ExpectedEpisodeRevision: 1,
			ExpectedRuntimeRevision: "runtime-identity", ExpectedChildRevisions: map[string]uint64{child.ID: 0},
			Children: []accounting.RecoveryChild{child}, Events: []accounting.RecoveryEvent{event},
			Actor: "operator", AuditRef: "audit-identity", EvidenceHash: fmt.Sprintf("%064x", 51), EvidenceReadAt: time.Now().UTC(),
		}
	}
	tests := []struct {
		name   string
		mutate func(*accounting.RecoveryOperation)
	}{
		{name: "account scope", mutate: func(op *accounting.RecoveryOperation) { op.Events[0].AccountScope = fmt.Sprintf("%064x", 52) }},
		{name: "exchange", mutate: func(op *accounting.RecoveryOperation) { op.Events[0].Exchange = "other-exchange" }},
		{name: "market", mutate: func(op *accounting.RecoveryOperation) { op.Events[0].Market = "futures" }},
		{name: "symbol", mutate: func(op *accounting.RecoveryOperation) { op.Events[0].FillEvidence.Symbol = "ETHUSDT" }},
		{name: "client order id", mutate: func(op *accounting.RecoveryOperation) { op.Events[0].FillEvidence.ClientOrderID = "other-cid" }},
		{name: "venue order id", mutate: func(op *accounting.RecoveryOperation) { op.Events[0].FillEvidence.VenueOrderID = "other-venue" }},
		{name: "side", mutate: func(op *accounting.RecoveryOperation) { op.Events[0].FillEvidence.Side = "SELL" }},
		{name: "intent hash", mutate: func(op *accounting.RecoveryOperation) {
			op.Events[0].FillEvidence.OrderIntentHash = fmt.Sprintf("%064x", 53)
		}},
		{name: "unbound child", mutate: func(op *accounting.RecoveryOperation) { op.Children[0].State = accounting.ChildProvisional }},
		{name: "missing venue id", mutate: func(op *accounting.RecoveryOperation) { op.Children[0].VenueOrderID = "" }},
	}
	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			op := newOperation(fmt.Sprintf("identity-reject-%d", i))
			tt.mutate(&op)
			if _, _, err := store.ApplyRecoveryOperation(ctx, generation, op); err == nil {
				t.Fatal("ApplyRecoveryOperation accepted mismatched or unbound fill identity")
			}
		})
	}
}

func TestFundingRecoveryOpeningBalanceRequiresMatchingOperationAuditAndPersistsRawFact(t *testing.T) {
	store, err := NewSQLStorage(filepath.Join(t.TempDir(), "funding-recovery-opening-balance.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	ctx := context.Background()
	if err := store.MigrateFundingRecovery(ctx); err != nil {
		t.Fatal(err)
	}
	scope := fmt.Sprintf("%064x", 49)
	generation, err := store.ClaimFundingCarryRuntimeGeneration(ctx, []string{scope})
	if err != nil {
		t.Fatal(err)
	}
	episode := accounting.RecoveryEpisode{
		ID: "episode-opening-balance", Bot: "bot-opening-balance", Strategy: "FundingCarry", AccountScopes: []string{scope},
		OwnerGeneration: generation.ID(), Revision: 1, RecoveryGeneration: 1, State: accounting.RecoveryPrepared,
	}
	if _, err := store.CreateRecoveryEpisode(ctx, generation, episode); err != nil {
		t.Fatal(err)
	}
	snapshotHash := fmt.Sprintf("%064x", 50)
	event := accounting.RecoveryEvent{
		ID: "opening-fact-1", Source: accounting.RecoveryManualOpeningBalanceSource, Exchange: "binance", Market: "spot_margin",
		AccountScope: scope, Kind: accounting.RecoveryOpeningBalanceEvent, ExternalID: "opening-operation-1", EpisodeID: episode.ID,
		Asset: "BTC", Amount: "0.123456789012345678", SourceRevision: "snapshot-v1", SourceHash: snapshotHash,
		ObservedAt: time.Date(2026, 10, 13, 1, 2, 3, 0, time.UTC),
		OpeningBalance: &accounting.RecoveryOpeningBalanceEvidence{
			Actor: "operator", AuditRef: "audit-opening-1", SnapshotHash: snapshotHash,
			SnapshotAt: time.Date(2026, 10, 13, 1, 0, 0, 0, time.UTC),
		},
	}
	operation := accounting.RecoveryOperation{
		EpisodeID: episode.ID, OperationID: "opening-operation-1", HashVersion: 1, ExpectedEpisodeRevision: 1,
		ExpectedRuntimeRevision: "runtime-opening-1", ExpectedEventRevisions: map[string]uint64{}, Events: []accounting.RecoveryEvent{event},
		Actor: "operator", AuditRef: "audit-opening-1", EvidenceHash: snapshotHash, EvidenceReadAt: event.ObservedAt,
	}
	bad := operation
	bad.Actor = "different-operator"
	if _, _, err := store.ApplyRecoveryOperation(ctx, generation, bad); err == nil {
		t.Fatal("operation with mismatched actor was accepted")
	}
	bad = operation
	bad.EvidenceHash = fmt.Sprintf("%064x", 51)
	if _, _, err := store.ApplyRecoveryOperation(ctx, generation, bad); err == nil {
		t.Fatal("operation receipt with a different evidence digest was accepted")
	}
	if _, replayed, err := store.ApplyRecoveryOperation(ctx, generation, operation); err != nil || replayed {
		t.Fatalf("ApplyRecoveryOperation opening baseline: replayed=%v err=%v", replayed, err)
	}
	events, err := store.ListRecoveryEvents(ctx, episode.ID)
	if err != nil || len(events) != 1 {
		t.Fatalf("ListRecoveryEvents = %d, err %v", len(events), err)
	}
	if events[0].Kind != accounting.RecoveryOpeningBalanceEvent || events[0].Amount != "0.123456789012345678" ||
		events[0].OpeningBalance == nil || events[0].OpeningBalance.Actor != operation.Actor || events[0].SourceHash != snapshotHash {
		t.Fatalf("opening balance fact did not round-trip exactly: %+v", events[0])
	}
}

func TestMarkRecoveryProjectedAfterTakeoverPreservesNewOwnerAudit(t *testing.T) {
	store, err := NewSQLStorage(filepath.Join(t.TempDir(), "funding-recovery-takeover-projection.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	ctx := context.Background()
	if err = store.MigrateFundingRecovery(ctx); err != nil {
		t.Fatal(err)
	}
	scope := fmt.Sprintf("%064x", 43)
	oldOwner, err := store.ClaimFundingCarryRuntimeGeneration(ctx, []string{scope})
	if err != nil {
		t.Fatal(err)
	}
	episode := accounting.RecoveryEpisode{ID: "episode-takeover-projection", Bot: "bot-takeover-projection", Strategy: "funding_carry", AccountScopes: []string{scope}, OwnerGeneration: oldOwner.ID(), Revision: 1, RecoveryGeneration: 1, State: accounting.RecoveryPrepared}
	if _, err = store.CreateRecoveryEpisode(ctx, oldOwner, episode); err != nil {
		t.Fatal(err)
	}
	event := accounting.RecoveryEvent{ID: "terminal-event", Source: "audit", Exchange: "binance", Market: "spot_margin", AccountScope: scope, Kind: accounting.RecoveryTerminalEvent, ExternalID: "terminal-1", EpisodeID: episode.ID, Asset: "BTC", Amount: "0", SourceRevision: "r1", SourceHash: fmt.Sprintf("%064x", 44), ObservedAt: time.Now().UTC()}
	op := accounting.RecoveryOperation{EpisodeID: episode.ID, OperationID: "pre-takeover-operation", HashVersion: 1, ExpectedEpisodeRevision: 1, ExpectedRuntimeRevision: "runtime-before-takeover", ExpectedEventRevisions: map[string]uint64{}, Events: []accounting.RecoveryEvent{event}, Actor: "admin", AuditRef: "incident-pre-takeover", EvidenceHash: fmt.Sprintf("%064x", 45), EvidenceReadAt: time.Now().UTC()}
	if _, replayed, applyErr := store.ApplyRecoveryOperation(ctx, oldOwner, op); applyErr != nil || replayed {
		t.Fatalf("commit pre-existing outbox: replayed=%t err=%v", replayed, applyErr)
	}
	current, found, err := store.GetRecoveryEpisode(ctx, episode.ID)
	if err != nil || !found || current.State != accounting.RecoveryLedgerCommitted || current.Revision != 2 {
		t.Fatalf("pre-takeover episode readback: %+v found=%t err=%v", current, found, err)
	}
	newOwner, err := store.ClaimFundingCarryRuntimeGeneration(ctx, []string{scope})
	if err != nil {
		t.Fatal(err)
	}
	takenOver, replayed, err := store.TakeOverRecoveryEpisode(ctx, newOwner, accounting.RecoveryOwnerTakeoverAuthorization{EpisodeID: episode.ID, OperationID: "explicit-takeover", Actor: "admin", AuditRef: "incident-takeover", ExpectedOwnerGeneration: oldOwner.ID(), ExpectedEpisodeRevision: current.Revision, Confirmed: true})
	if err != nil || replayed || takenOver.State != accounting.RecoveryBlocked || takenOver.OwnerGeneration != newOwner.ID() {
		t.Fatalf("explicit takeover: episode=%+v replayed=%t err=%v", takenOver, replayed, err)
	}
	outbox, found, err := store.GetRecoveryOutbox(ctx, episode.ID, op.OperationID)
	if err != nil || !found || outbox.State != accounting.RecoveryProjectionPending {
		t.Fatalf("outbox changed during takeover: %+v found=%t err=%v", outbox, found, err)
	}
	if _, err = store.db.ExecContext(ctx, `UPDATE funding_recovery_outbox SET target_episode_revision=? WHERE episode_id=? AND operation_id=?`, outbox.TargetEpisodeRevision+1, episode.ID, op.OperationID); err != nil {
		t.Fatal(err)
	}
	if _, err = store.ListRecoveryOutboxes(ctx, episode.ID); err == nil {
		t.Fatal("outbox list accepted indexed revision that disagrees with its hashed payload")
	}
	if _, err = store.db.ExecContext(ctx, `UPDATE funding_recovery_outbox SET target_episode_revision=? WHERE episode_id=? AND operation_id=?`, outbox.TargetEpisodeRevision, episode.ID, op.OperationID); err != nil {
		t.Fatal(err)
	}
	if _, err = store.db.ExecContext(ctx, `UPDATE funding_recovery_outbox SET operation_id=? WHERE episode_id=? AND operation_id=?`, "tampered-operation", episode.ID, op.OperationID); err != nil {
		t.Fatal(err)
	}
	if _, err = store.ListRecoveryOutboxes(ctx, episode.ID); err == nil {
		t.Fatal("outbox list accepted indexed operation ID that disagrees with its payload")
	}
	if _, err = store.db.ExecContext(ctx, `UPDATE funding_recovery_outbox SET operation_id=? WHERE episode_id=? AND operation_id=?`, op.OperationID, episode.ID, "tampered-operation"); err != nil {
		t.Fatal(err)
	}
	if _, err = store.db.ExecContext(ctx, `UPDATE funding_recovery_outbox SET state=? WHERE episode_id=? AND operation_id=?`, "tampered-state", episode.ID, op.OperationID); err != nil {
		t.Fatal(err)
	}
	if _, err = store.ListRecoveryOutboxes(ctx, episode.ID); err == nil {
		t.Fatal("outbox list accepted indexed state that disagrees with its payload")
	}
	if _, err = store.db.ExecContext(ctx, `UPDATE funding_recovery_outbox SET state=? WHERE episode_id=? AND operation_id=?`, string(accounting.RecoveryProjectionPending), episode.ID, op.OperationID); err != nil {
		t.Fatal(err)
	}
	if _, err = store.db.ExecContext(ctx, `UPDATE funding_recovery_outbox SET projection_hash=? WHERE episode_id=? AND operation_id=?`, strings.Repeat("a", 64), episode.ID, op.OperationID); err != nil {
		t.Fatal(err)
	}
	if _, err = store.ListRecoveryOutboxes(ctx, episode.ID); err == nil {
		t.Fatal("outbox list accepted indexed projection hash that disagrees with its payload")
	}
	if _, err = store.db.ExecContext(ctx, `UPDATE funding_recovery_outbox SET projection_hash=? WHERE episode_id=? AND operation_id=?`, outbox.ProjectionHash, episode.ID, op.OperationID); err != nil {
		t.Fatal(err)
	}
	outbox, found, err = store.GetRecoveryOutbox(ctx, episode.ID, op.OperationID)
	if err != nil || !found {
		t.Fatalf("restore outbox fixture after index integrity checks: %+v found=%t err=%v", outbox, found, err)
	}
	outbox.AppliedOperationID = outbox.OperationID
	outbox.AppliedRevision = outbox.TargetEpisodeRevision
	outbox.AppliedHash = outbox.ProjectionHash
	outbox.State = accounting.RecoveryProjected
	if err = store.MarkRecoveryProjected(ctx, newOwner, outbox); err != nil {
		t.Fatalf("project existing outbox without losing takeover record: %v", err)
	}
	projected, found, err := store.GetRecoveryEpisode(ctx, episode.ID)
	if err != nil || !found || projected.State != accounting.RecoveryProjected || projected.OwnerGeneration != newOwner.ID() ||
		projected.Revision != takenOver.Revision+1 || !reflect.DeepEqual(projected.OwnerTakeovers, takenOver.OwnerTakeovers) {
		t.Fatalf("projected episode lost current owner/audit: %+v found=%t err=%v", projected, found, err)
	}
	projectedOutbox, found, err := store.GetRecoveryOutbox(ctx, episode.ID, op.OperationID)
	if err != nil || !found || projectedOutbox.State != accounting.RecoveryProjected || projectedOutbox.AppliedHash != outbox.ProjectionHash {
		t.Fatalf("projected outbox readback: %+v found=%t err=%v", projectedOutbox, found, err)
	}
}

func TestConsumeRecoveryPermitGrantsSinglePhysicalSubmissionClaim(t *testing.T) {
	store, err := NewSQLStorage(filepath.Join(t.TempDir(), "funding-recovery-permit.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.MigrateFundingRecovery(context.Background()); err != nil {
		t.Fatalf("migrate funding recovery tables: %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("close SQLite storage: %v", err)
		}
	})

	ctx := context.Background()
	scopeKey := fmt.Sprintf("%064x", 1)
	generation, err := store.ClaimFundingCarryRuntimeGeneration(ctx, []string{scopeKey})
	if err != nil {
		t.Fatalf("claim runtime generation: %v", err)
	}
	episode := accounting.RecoveryEpisode{
		ID: "episode-permit-cas", Bot: "bot-1", Strategy: "FundingCarry",
		AccountScopes: []string{scopeKey}, OwnerGeneration: generation.ID(),
		Revision: 1, RecoveryGeneration: 1, NextActionOrdinal: 1,
		State: accounting.RecoveryPrepared,
		ActionPlan: []accounting.RecoveryAction{{
			ID: "action-1", Ordinal: 1, Kind: accounting.RecoveryActionBorrow,
			Role: "hedge-open", AccountScope: scopeKey, Asset: "BTC", Amount: "0.01",
			OwnerGeneration: generation.ID(), Status: "authorized",
			GateSourceExceptions: []string{"execution_intent_recovery"},
		}},
	}
	if _, err := store.CreateRecoveryEpisode(ctx, generation, episode); err != nil {
		t.Fatal(err)
	}
	permit := accounting.OneShotPermit{
		ID: "permit-cas-1", EpisodeID: episode.ID, Generation: 1, ActionID: "action-1",
		ActionOrdinal: 1, Kind: accounting.RecoveryActionBorrow, AccountScope: scopeKey,
		OwnerGeneration: generation.ID(), LeaseID: 1, ExpectedRevision: 1,
		State: accounting.PermitAuthorized, GateSourceExceptions: []string{"execution_intent_recovery"},
	}
	if _, err := store.CreateRecoveryPermit(ctx, generation, permit); err != nil {
		t.Fatal(err)
	}
	mismatchedSources := permit
	mismatchedSources.GateSourceExceptions = nil
	if _, _, err := store.ConsumeRecoveryPermit(ctx, generation, mismatchedSources); !errors.Is(err, accounting.ErrRecoveryConflict) {
		t.Fatalf("permit with mismatched persisted Gate source set was accepted: %v", err)
	}

	const contenders = 8
	var wg sync.WaitGroup
	var mu sync.Mutex
	claims := 0
	errs := make([]error, contenders)
	for i := range contenders {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, maySubmit, consumeErr := store.ConsumeRecoveryPermit(ctx, generation, permit)
			if consumeErr != nil {
				errs[i] = consumeErr
				return
			}
			if maySubmit {
				mu.Lock()
				claims++
				mu.Unlock()
			}
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Errorf("contender %d consume permit: %v", i, err)
		}
	}
	if claims != 1 {
		t.Fatalf("physical submission claim count = %d, want exactly one", claims)
	}
	_, maySubmit, err := store.ConsumeRecoveryPermit(ctx, generation, permit)
	if err != nil {
		t.Fatal(err)
	}
	if maySubmit {
		t.Fatal("idempotent readback of consumed permit authorized a second physical submission")
	}
	foreignScope := fmt.Sprintf("%064x", 2)
	foreignGeneration, err := store.ClaimFundingCarryRuntimeGeneration(ctx, []string{foreignScope})
	if err != nil {
		t.Fatalf("claim unrelated account scope: %v", err)
	}
	if _, maySubmit, err = store.ConsumeRecoveryPermit(ctx, foreignGeneration, permit); !errors.Is(err, ErrFundingRecoveryGenerationScopeMismatch) || maySubmit {
		t.Fatalf("unrelated owner consume = maySubmit %v, err %v; want scope rejection", maySubmit, err)
	}
	if _, err := store.ClaimFundingCarryRuntimeGeneration(ctx, []string{scopeKey}); err != nil {
		t.Fatalf("take over runtime owner generation: %v", err)
	}
	if _, maySubmit, err = store.ConsumeRecoveryPermit(ctx, generation, permit); !errors.Is(err, ErrFundingCarryRuntimeGenerationLost) || maySubmit {
		t.Fatalf("stale owner consume = maySubmit %v, err %v; want fenced rejection", maySubmit, err)
	}
}

func TestConsumeOrderPermitAtomicallyPersistsUnknownSubmissionAttempt(t *testing.T) {
	store, err := NewSQLStorage(filepath.Join(t.TempDir(), "funding-recovery-order-attempt.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.MigrateFundingRecovery(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	ctx := t.Context()
	scope := fmt.Sprintf("%064x", 1)
	generation, err := store.ClaimFundingCarryRuntimeGeneration(ctx, []string{scope})
	if err != nil {
		t.Fatal(err)
	}
	intent := accounting.RecoveryOrderIntent{
		SchemaVersion: 1, Exchange: "binance", Market: "futures", Symbol: "BTCUSDT", Side: "BUY", OrderType: "LIMIT",
		TimeInForce: "GTC", Price: "100", Quantity: "0.25", ClientOrderID: "durable-cid-1",
	}
	intentHash, err := intent.CanonicalHash()
	if err != nil {
		t.Fatal(err)
	}
	episode := accounting.RecoveryEpisode{
		ID: "episode-attempt", Bot: "bot-1", Strategy: "funding_carry", AccountScopes: []string{scope},
		OwnerGeneration: generation.ID(), Revision: 1, RecoveryGeneration: 1, NextActionOrdinal: 1,
		State: accounting.RecoveryPrepared,
		ActionPlan: []accounting.RecoveryAction{{
			ID: "action-attempt", Ordinal: 1, Kind: accounting.RecoveryActionOrder, Role: "open_futures",
			AccountScope: scope, Market: intent.Market, Symbol: intent.Symbol, OwnerGeneration: generation.ID(),
			OrderIntentHash: intentHash, Status: "authorized",
		}},
	}
	if _, err := store.CreateRecoveryEpisode(ctx, generation, episode); err != nil {
		t.Fatal(err)
	}
	permit := accounting.OneShotPermit{
		ID: "permit-attempt", EpisodeID: episode.ID, Generation: 1, ActionID: "action-attempt", ActionOrdinal: 1,
		Kind: accounting.RecoveryActionOrder, AccountScope: scope, OwnerGeneration: generation.ID(),
		LeaseID: 1, OrderIntentHash: intentHash, OrderIntent: &intent, ExpectedRevision: 1, State: accounting.PermitAuthorized,
	}
	if _, err := store.CreateRecoveryPermit(ctx, generation, permit); err != nil {
		t.Fatal(err)
	}
	// Force the attempt insert to fail after permit UPDATE; the SQL transaction
	// must roll the consume back, so no caller can get a submission claim.
	if _, err := store.db.ExecContext(ctx, `INSERT INTO funding_recovery_submission_attempts(permit_id,episode_id,recovery_generation,action_id,revision,state,client_order_id,intent_hash,payload_json) VALUES(?,?,?,?,?,?,?,?,?)`, permit.ID, episode.ID, 1, permit.ActionID, 1, string(accounting.RecoverySubmissionUnknown), intent.ClientOrderID, intentHash, `{}`); err != nil {
		t.Fatal(err)
	}
	if _, maySubmit, err := store.ConsumeRecoveryPermit(ctx, generation, permit); err == nil || maySubmit {
		t.Fatalf("consume with conflicting attempt row = maySubmit %v, err %v; want fail-closed rollback", maySubmit, err)
	}
	var state string
	if err := store.db.QueryRowContext(ctx, `SELECT state FROM funding_recovery_permits WHERE permit_id=?`, permit.ID).Scan(&state); err != nil || state != string(accounting.PermitAuthorized) {
		t.Fatalf("failed attempt transaction left permit state %q, err %v; want authorized", state, err)
	}
	children, err := store.ListRecoveryChildren(ctx, episode.ID)
	if err != nil || len(children) != 0 {
		t.Fatalf("failed attempt transaction left provisional children %+v, err %v; want none", children, err)
	}
	if _, err := store.db.ExecContext(ctx, `DELETE FROM funding_recovery_submission_attempts WHERE permit_id=?`, permit.ID); err != nil {
		t.Fatal(err)
	}
	consumed, maySubmit, err := store.ConsumeRecoveryPermit(ctx, generation, permit)
	if err != nil || !maySubmit || consumed.State != accounting.PermitConsumed {
		t.Fatalf("consume = permit %s, maySubmit %v, err %v; want one consumed claim", consumed.State, maySubmit, err)
	}
	attempt, found, err := store.GetRecoverySubmissionAttempt(ctx, episode.ID, permit.ID)
	if err != nil || !found || attempt.State != accounting.RecoverySubmissionUnknown || attempt.Intent.ClientOrderID != intent.ClientOrderID || attempt.IntentHash != intentHash {
		t.Fatalf("attempt readback = %+v, found %v, err %v", attempt, found, err)
	}
	children, err = store.ListRecoveryChildren(ctx, episode.ID)
	if err != nil || len(children) != 1 || children[0].ID != attempt.ChildID || children[0].State != accounting.ChildProvisional || children[0].ClientOrderID != intent.ClientOrderID || children[0].OrderIntentHash != intentHash || children[0].Side != intent.Side {
		t.Fatalf("provisional child readback = %+v, attempt=%+v, err=%v", children, attempt, err)
	}
	attempts, err := store.ListRecoverySubmissionAttempts(ctx, episode.ID)
	if err != nil || len(attempts) != 1 {
		t.Fatalf("attempt list = %d rows, err %v; want exactly one", len(attempts), err)
	}
	if _, maySubmit, err = store.ConsumeRecoveryPermit(ctx, generation, permit); err != nil || maySubmit {
		t.Fatalf("consumed permit replay = maySubmit %v, err %v; must never reauthorize RPC", maySubmit, err)
	}
	releaseAuth := accounting.RecoveryReleaseAuthorization{
		EpisodeID: episode.ID, OperationID: "release-before-reconcile", Actor: "admin", AuditRef: "incident-release-before-reconcile", EvidenceHash: strings.Repeat("a", 64),
		ExposureClearanceHash: strings.Repeat("b", 64), ExposureClearanceConfirmed: true,
		ProjectionHash: strings.Repeat("c", 64), RuntimeRevision: "runtime-revision", OwnerGeneration: generation.ID(),
		ExpectedEpisodeRevision: 1, RecoveryGeneration: 1, ExpectedHoldRevision: 1, Confirmed: true,
	}
	if _, err = store.PrepareRecoveryRelease(ctx, generation, releaseAuth); !errors.Is(err, accounting.ErrRecoveryConflict) {
		t.Fatalf("release with unknown attempt = %v; want unresolved-attempt conflict", err)
	}
	staleEpisode, found, err := store.GetRecoveryEpisode(ctx, episode.ID)
	if err != nil || !found {
		t.Fatalf("read episode before generation-fence test: found=%v err=%v", found, err)
	}
	staleEpisode.RecoveryGeneration++
	staleEpisode.Revision++
	staleEpisode.State = accounting.RecoveryReopened
	stalePayload, err := json.Marshal(staleEpisode)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.db.ExecContext(ctx, `UPDATE funding_recovery_episodes SET revision=?,recovery_generation=?,state=?,payload_json=? WHERE episode_id=?`, staleEpisode.Revision, staleEpisode.RecoveryGeneration, staleEpisode.State, stalePayload, episode.ID); err != nil {
		t.Fatal(err)
	}
	if err = store.AcknowledgeRecoverySubmission(ctx, generation, permit, "venue-order-stale"); !errors.Is(err, accounting.ErrRecoveryConflict) {
		t.Fatalf("stale generation acknowledgement = %v; want conflict", err)
	}
	staleEpisode.RecoveryGeneration = permit.Generation
	staleEpisode.Revision++
	staleEpisode.State = accounting.RecoveryPrepared
	stalePayload, err = json.Marshal(staleEpisode)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.db.ExecContext(ctx, `UPDATE funding_recovery_episodes SET revision=?,recovery_generation=?,state=?,payload_json=? WHERE episode_id=?`, staleEpisode.Revision, staleEpisode.RecoveryGeneration, staleEpisode.State, stalePayload, episode.ID); err != nil {
		t.Fatal(err)
	}
	ackStart := make(chan struct{})
	ackReady := make(chan struct{}, 8)
	ackErrors := make(chan error, 8)
	var ackWG sync.WaitGroup
	for i := 0; i < cap(ackErrors); i++ {
		ackWG.Add(1)
		go func() {
			defer ackWG.Done()
			ackReady <- struct{}{}
			<-ackStart
			ackErrors <- store.AcknowledgeRecoverySubmission(ctx, generation, permit, "venue-order-1")
		}()
	}
	for i := 0; i < cap(ackErrors); i++ {
		<-ackReady
	}
	close(ackStart)
	ackWG.Wait()
	close(ackErrors)
	for ackErr := range ackErrors {
		if ackErr != nil {
			t.Fatalf("concurrent idempotent venue acknowledgement: %v", ackErr)
		}
	}
	children, err = store.ListRecoveryChildren(ctx, episode.ID)
	if err != nil || len(children) != 1 || children[0].ID != attempt.ChildID || children[0].State != accounting.ChildBound || children[0].VenueOrderID != "venue-order-1" || children[0].Revision != 2 {
		t.Fatalf("bound child readback = %+v, err=%v", children, err)
	}
	attempt, found, err = store.GetRecoverySubmissionAttempt(ctx, episode.ID, permit.ID)
	if err != nil || !found || attempt.State != accounting.RecoverySubmissionAcknowledged || attempt.Revision != 2 || attempt.VenueOrderID != "venue-order-1" {
		t.Fatalf("acknowledged attempt readback = %+v, found %v, err %v", attempt, found, err)
	}
	releaseAuth.OperationID = "release-before-accounting"
	if _, err = store.PrepareRecoveryRelease(ctx, generation, releaseAuth); !errors.Is(err, accounting.ErrRecoveryConflict) {
		t.Fatalf("release with acknowledged but unaccounted attempt = %v; want unresolved-attempt conflict", err)
	}
	if err = store.AcknowledgeRecoverySubmission(ctx, generation, permit, "venue-order-1"); err != nil {
		t.Fatalf("idempotent acknowledgement readback: %v", err)
	}
	if err = store.AcknowledgeRecoverySubmission(ctx, generation, permit, "venue-order-2"); !errors.Is(err, accounting.ErrRecoveryConflict) {
		t.Fatalf("conflicting venue acknowledgement = %v; want CAS conflict", err)
	}
	forgedPermit := permit
	forgedPermit.ExpectedRevision++
	if err = store.AcknowledgeRecoverySubmission(ctx, generation, forgedPermit, "venue-order-1"); !errors.Is(err, accounting.ErrRecoveryConflict) {
		t.Fatalf("forged permit acknowledgement = %v; want identity conflict", err)
	}
	attempt.State = accounting.RecoverySubmissionFound
	attempt.Revision++
	attempt.UpdatedAt = time.Now().UTC()
	attemptPayload, err := json.Marshal(attempt)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.db.ExecContext(ctx, `UPDATE funding_recovery_submission_attempts SET state=?,revision=?,payload_json=? WHERE episode_id=? AND permit_id=?`, string(attempt.State), attempt.Revision, attemptPayload, episode.ID, permit.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = store.PrepareRecoveryRelease(ctx, generation, releaseAuth); !errors.Is(err, accounting.ErrRecoveryConflict) {
		t.Fatalf("release with found but unaccounted attempt = %v; want unresolved-attempt conflict", err)
	}
}

func TestRecoverySubmissionAttemptSurvivesSQLiteReopenWithoutReplay(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "funding-recovery-restart.db")
	store, err := NewSQLStorage(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.MigrateFundingRecovery(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	ctx := t.Context()
	scope := fmt.Sprintf("%064x", 91)
	generation, err := store.ClaimFundingCarryRuntimeGeneration(ctx, []string{scope})
	if err != nil {
		t.Fatal(err)
	}
	intent := accounting.RecoveryOrderIntent{
		SchemaVersion: 1, Exchange: "binance", Market: "futures", Symbol: "BTCUSDT", Side: "BUY", OrderType: "LIMIT",
		TimeInForce: "GTC", Price: "100", Quantity: "0.1", ClientOrderID: "restart-cid",
	}
	hash, err := intent.CanonicalHash()
	if err != nil {
		t.Fatal(err)
	}
	episode := accounting.RecoveryEpisode{
		ID: "episode-restart", Bot: "bot-restart", Strategy: "funding_carry", AccountScopes: []string{scope},
		OwnerGeneration: generation.ID(), Revision: 1, RecoveryGeneration: 1, NextActionOrdinal: 1,
		State: accounting.RecoveryPrepared, ActionPlan: []accounting.RecoveryAction{{
			ID: "action-restart", Ordinal: 1, Kind: accounting.RecoveryActionOrder, Role: "open_futures",
			AccountScope: scope, Market: intent.Market, Symbol: intent.Symbol, OwnerGeneration: generation.ID(),
			OrderIntentHash: hash, GateSourceExceptions: []string{"execution_intent_recovery"}, Status: "authorized",
		}},
	}
	if _, err = store.CreateRecoveryEpisode(ctx, generation, episode); err != nil {
		t.Fatal(err)
	}
	permit := accounting.OneShotPermit{
		ID: "permit-restart", EpisodeID: episode.ID, Generation: 1, ActionID: "action-restart", ActionOrdinal: 1,
		Kind: accounting.RecoveryActionOrder, AccountScope: scope, OwnerGeneration: generation.ID(), LeaseID: 1,
		GateSourceExceptions: []string{"execution_intent_recovery"}, OrderIntentHash: hash, OrderIntent: &intent,
		ExpectedRevision: 1, State: accounting.PermitAuthorized,
	}
	if _, err = store.CreateRecoveryPermit(ctx, generation, permit); err != nil {
		t.Fatal(err)
	}
	if _, maySubmit, consumeErr := store.ConsumeRecoveryPermit(ctx, generation, permit); consumeErr != nil || !maySubmit {
		t.Fatalf("initial consume = maySubmit %v, err %v", maySubmit, consumeErr)
	}
	if err = store.AcknowledgeRecoverySubmission(ctx, generation, permit, "venue-order-before-restart"); err != nil {
		t.Fatalf("record positive venue acknowledgement: %v", err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = NewSQLStorage(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	attempt, found, err := store.GetRecoverySubmissionAttempt(ctx, episode.ID, permit.ID)
	if err != nil || !found || attempt.State != accounting.RecoverySubmissionAcknowledged || attempt.VenueOrderID != "venue-order-before-restart" || attempt.Intent.ClientOrderID != intent.ClientOrderID {
		t.Fatalf("restart attempt readback = %+v found=%v err=%v", attempt, found, err)
	}
	children, err := store.ListRecoveryChildren(ctx, episode.ID)
	if err != nil || len(children) != 1 || children[0].ID != attempt.ChildID || children[0].State != accounting.ChildBound || children[0].VenueOrderID != attempt.VenueOrderID {
		t.Fatalf("restart provisional child readback = %+v err=%v", children, err)
	}
	newGeneration, err := store.ClaimFundingCarryRuntimeGeneration(ctx, []string{scope})
	if err != nil {
		t.Fatal(err)
	}
	if _, maySubmit, consumeErr := store.ConsumeRecoveryPermit(ctx, newGeneration, permit); !errors.Is(consumeErr, accounting.ErrRecoveryConflict) || maySubmit {
		t.Fatalf("restart replay = maySubmit %v err %v; want old-permit conflict", maySubmit, consumeErr)
	}
	holds, err := store.ListActiveRecoveryHolds(ctx)
	if err != nil || len(holds) != 1 || holds[0].EpisodeID != episode.ID || holds[0].State != "held" {
		t.Fatalf("restart active holds = %+v err=%v; want original episode still held", holds, err)
	}
	takeoverAuth := accounting.RecoveryOwnerTakeoverAuthorization{
		EpisodeID: episode.ID, OperationID: "restart-takeover-op", Actor: "admin-restart", AuditRef: "restart-incident",
		ExpectedOwnerGeneration: generation.ID(), ExpectedEpisodeRevision: episode.Revision, Confirmed: true,
	}
	takenOver, replayed, err := store.TakeOverRecoveryEpisode(ctx, newGeneration, takeoverAuth)
	if err != nil || replayed || takenOver.OwnerGeneration != newGeneration.ID() || takenOver.State != accounting.RecoveryBlocked {
		t.Fatalf("takeover after restart = episode %+v replayed=%v err=%v", takenOver, replayed, err)
	}
	attemptAfterTakeover, found, err := store.GetRecoverySubmissionAttempt(ctx, episode.ID, permit.ID)
	if err != nil || !found || attemptAfterTakeover.State != accounting.RecoverySubmissionAcknowledged || attemptAfterTakeover.VenueOrderID != "venue-order-before-restart" || attemptAfterTakeover.Intent.ClientOrderID != intent.ClientOrderID || attemptAfterTakeover.OwnerGeneration != generation.ID() {
		t.Fatalf("takeover modified consumed attempt = %+v found=%v err=%v", attemptAfterTakeover, found, err)
	}
	holdAfterTakeover, found, err := store.GetRecoveryHold(ctx, episode.ID, episode.RecoveryGeneration)
	if err != nil || !found || holdAfterTakeover.State != "held" || holdAfterTakeover.Revision != holds[0].Revision {
		t.Fatalf("takeover modified original hold = %+v found=%v err=%v", holdAfterTakeover, found, err)
	}
	adjudication := accounting.RecoverySubmissionAdjudication{
		EpisodeID: episode.ID, OperationID: "operator-found-order", PermitID: permit.ID,
		ExpectedAttemptRevision: attemptAfterTakeover.Revision, ExpectedEpisodeRevision: takenOver.Revision,
		Decision: accounting.RecoverySubmissionFound, VenueOrderID: "venue-order-before-restart",
		Actor: "admin-restart", AuditRef: "incident-restart", EvidenceHash: fmt.Sprintf("%064x", 901), Confirmed: true,
	}
	conflictingVenueOrder := adjudication
	conflictingVenueOrder.VenueOrderID = "different-venue-order"
	if _, _, err = store.AdjudicateRecoverySubmission(ctx, newGeneration, conflictingVenueOrder); !errors.Is(err, accounting.ErrRecoveryConflict) {
		t.Fatalf("acknowledged order ID mismatch = %v; want held conflict", err)
	}
	adjudicated, replayed, err := store.AdjudicateRecoverySubmission(ctx, newGeneration, adjudication)
	if err != nil || replayed || adjudicated.State != accounting.RecoverySubmissionFound || adjudicated.Actor != adjudication.Actor || adjudicated.EvidenceHash != adjudication.EvidenceHash {
		t.Fatalf("positive found adjudication = %+v replayed=%v err=%v", adjudicated, replayed, err)
	}
	adjudicated, replayed, err = store.AdjudicateRecoverySubmission(ctx, newGeneration, adjudication)
	if err != nil || !replayed || adjudicated.VenueOrderID != adjudication.VenueOrderID {
		t.Fatalf("same adjudication replay = %+v replayed=%v err=%v", adjudicated, replayed, err)
	}
	conflicting := adjudication
	conflicting.AuditRef = "different-incident-reference"
	if _, _, err = store.AdjudicateRecoverySubmission(ctx, newGeneration, conflicting); !errors.Is(err, accounting.ErrRecoveryOperationConflict) {
		t.Fatalf("operation ID with different evidence = %v; want idempotency conflict", err)
	}
	children, err = store.ListRecoveryChildren(ctx, episode.ID)
	if err != nil || len(children) != 1 || children[0].State != accounting.ChildBound || children[0].VenueOrderID != adjudication.VenueOrderID {
		t.Fatalf("adjudicated child = %+v err=%v", children, err)
	}
	holdAfterAdjudication, found, err := store.GetRecoveryHold(ctx, episode.ID, episode.RecoveryGeneration)
	if err != nil || !found || holdAfterAdjudication.State != "held" {
		t.Fatalf("positive order adjudication must preserve hold: %+v found=%v err=%v", holdAfterAdjudication, found, err)
	}
}

func TestRecoveryOwnerTakeoverRevokesAuthorizedPermitAndPreservesHold(t *testing.T) {
	store, err := NewSQLStorage(filepath.Join(t.TempDir(), "funding-recovery-takeover.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err = store.MigrateFundingRecovery(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	ctx := t.Context()
	scope := fmt.Sprintf("%064x", 117)
	oldOwner, err := store.ClaimFundingCarryRuntimeGeneration(ctx, []string{scope})
	if err != nil {
		t.Fatal(err)
	}
	episode := accounting.RecoveryEpisode{
		ID: "episode-owner-takeover", Bot: "bot-takeover", Strategy: "funding_carry", AccountScopes: []string{scope},
		OwnerGeneration: oldOwner.ID(), Revision: 1, RecoveryGeneration: 1, NextActionOrdinal: 1,
		State: accounting.RecoveryPrepared, ActionPlan: []accounting.RecoveryAction{{
			ID: "action-takeover", Ordinal: 1, Kind: accounting.RecoveryActionBorrow, Role: "open-margin",
			AccountScope: scope, Asset: "BTC", Amount: "0.01", OwnerGeneration: oldOwner.ID(), Status: "authorized",
		}},
	}
	if _, err = store.CreateRecoveryEpisode(ctx, oldOwner, episode); err != nil {
		t.Fatal(err)
	}
	permit := accounting.OneShotPermit{
		ID: "permit-takeover", EpisodeID: episode.ID, Generation: 1, ActionID: "action-takeover", ActionOrdinal: 1,
		Kind: accounting.RecoveryActionBorrow, AccountScope: scope, OwnerGeneration: oldOwner.ID(), LeaseID: 1,
		ExpectedRevision: 1, State: accounting.PermitAuthorized,
	}
	if _, err = store.CreateRecoveryPermit(ctx, oldOwner, permit); err != nil {
		t.Fatal(err)
	}
	newOwner, err := store.ClaimFundingCarryRuntimeGeneration(ctx, []string{scope})
	if err != nil {
		t.Fatal(err)
	}
	wrongScopeOwner, err := store.ClaimFundingCarryRuntimeGeneration(ctx, []string{fmt.Sprintf("%064x", 118)})
	if err != nil {
		t.Fatal(err)
	}
	auth := accounting.RecoveryOwnerTakeoverAuthorization{
		EpisodeID: episode.ID, OperationID: "takeover-op-1", Actor: "admin-1", AuditRef: "incident-117",
		ExpectedOwnerGeneration: oldOwner.ID(), ExpectedEpisodeRevision: episode.Revision, Confirmed: true,
	}
	if _, _, err = store.TakeOverRecoveryEpisode(ctx, wrongScopeOwner, auth); !errors.Is(err, ErrFundingRecoveryGenerationScopeMismatch) {
		t.Fatalf("cross-scope takeover = %v; want exact-scope rejection", err)
	}
	type takeoverResult struct {
		episode  accounting.RecoveryEpisode
		replayed bool
		err      error
	}
	const concurrentTakeovers = 4
	start := make(chan struct{})
	ready := make(chan struct{}, concurrentTakeovers)
	results := make(chan takeoverResult, concurrentTakeovers)
	var takeoverWG sync.WaitGroup
	for i := 0; i < concurrentTakeovers; i++ {
		takeoverWG.Add(1)
		go func() {
			defer takeoverWG.Done()
			ready <- struct{}{}
			<-start
			result, replay, takeoverErr := store.TakeOverRecoveryEpisode(ctx, newOwner, auth)
			results <- takeoverResult{episode: result, replayed: replay, err: takeoverErr}
		}()
	}
	for i := 0; i < concurrentTakeovers; i++ {
		<-ready
	}
	close(start)
	takeoverWG.Wait()
	close(results)
	writers, replays := 0, 0
	var takenOver accounting.RecoveryEpisode
	for result := range results {
		if result.err != nil {
			t.Fatalf("concurrent takeover: %v", result.err)
		}
		if result.replayed {
			replays++
		} else {
			writers++
		}
		takenOver = result.episode
	}
	if writers != 1 || replays != concurrentTakeovers-1 || takenOver.OwnerGeneration != newOwner.ID() || takenOver.Revision != episode.Revision+1 || takenOver.State != accounting.RecoveryBlocked {
		t.Fatalf("concurrent takeover writers=%d replays=%d episode=%+v", writers, replays, takenOver)
	}
	evidenceDigest := ""
	if len(takenOver.OwnerTakeovers) == 1 {
		evidenceDigest = takenOver.OwnerTakeovers[0].EvidenceHash
	}
	_, evidenceErr := hex.DecodeString(evidenceDigest)
	if len(takenOver.OwnerTakeovers) != 1 || takenOver.OwnerTakeovers[0].Actor != auth.Actor || takenOver.OwnerTakeovers[0].AuditRef != auth.AuditRef || takenOver.OwnerTakeovers[0].PreviousOwnerGeneration != oldOwner.ID() || takenOver.OwnerTakeovers[0].OwnerGeneration != newOwner.ID() || len(evidenceDigest) != 64 || evidenceErr != nil {
		t.Fatalf("takeover audit record = %+v", takenOver.OwnerTakeovers)
	}
	receipt, found, err := store.GetRecoveryReceipt(ctx, episode.ID, auth.OperationID)
	if err != nil || !found || receipt.Actor != auth.Actor || receipt.AuditRef != auth.AuditRef || receipt.PreviousOwnerGeneration != oldOwner.ID() || receipt.OwnerGeneration != newOwner.ID() || receipt.EvidenceHash != evidenceDigest {
		t.Fatalf("takeover operation receipt = %+v found=%v err=%v", receipt, found, err)
	}
	var permitRaw []byte
	if err = store.db.QueryRowContext(ctx, `SELECT payload_json FROM funding_recovery_permits WHERE permit_id=?`, permit.ID).Scan(&permitRaw); err != nil {
		t.Fatal(err)
	}
	var persistedPermit accounting.OneShotPermit
	if err = json.Unmarshal(permitRaw, &persistedPermit); err != nil || persistedPermit.State != accounting.PermitRevoked {
		t.Fatalf("old authorized permit = %+v err=%v; want revoked", persistedPermit, err)
	}
	oldHold, found, err := store.GetRecoveryHold(ctx, episode.ID, episode.RecoveryGeneration)
	if err != nil || !found || oldHold.State != "held" || oldHold.Revision != 1 {
		t.Fatalf("takeover hold = %+v found=%v err=%v; want unchanged active hold", oldHold, found, err)
	}
	if _, err = store.CreateRecoveryPermit(ctx, newOwner, permit); !errors.Is(err, accounting.ErrRecoveryConflict) {
		t.Fatalf("new permit against blocked takeover episode = %v; want conflict", err)
	}
	if _, replayed, err := store.TakeOverRecoveryEpisode(ctx, newOwner, auth); err != nil || !replayed {
		t.Fatalf("idempotent takeover retry replayed=%v err=%v", replayed, err)
	}
	if len(takenOver.ActionPlan) != 1 || takenOver.ActionPlan[0].OwnerGeneration != oldOwner.ID() {
		t.Fatalf("takeover rewrote original action attribution: %+v", takenOver.ActionPlan)
	}
	manualEvent := accounting.RecoveryEvent{
		ID: "manual-fee-1", Source: "operator-evidence", Exchange: "binance", Market: "spot", AccountScope: scope,
		Kind: accounting.RecoveryFeeEvent, ExternalID: "fee-117-1", EpisodeID: episode.ID,
		Asset: "USDT", Amount: "0.01", SourceRevision: "audit-rev-1", SourceHash: fmt.Sprintf("%064x", 117), ObservedAt: time.Now().UTC(),
	}
	manualOp := accounting.RecoveryOperation{
		EpisodeID: episode.ID, OperationID: "manual-adjudication-1", HashVersion: 1, ExpectedEpisodeRevision: takenOver.Revision,
		ExpectedRuntimeRevision: "runtime-adjudication-1", ExpectedEventRevisions: map[string]uint64{},
		Events: []accounting.RecoveryEvent{manualEvent}, Actor: "admin-1", AuditRef: "incident-117",
		EvidenceHash: evidenceDigest, EvidenceReadAt: time.Now().UTC(),
	}
	manualReceipt, replayed, err := store.ApplyRecoveryOperation(ctx, newOwner, manualOp)
	if err != nil || replayed || manualReceipt.Actor != manualOp.Actor || manualReceipt.AuditRef != manualOp.AuditRef || manualReceipt.EvidenceHash != evidenceDigest {
		t.Fatalf("manual takeover adjudication receipt=%+v replayed=%v err=%v", manualReceipt, replayed, err)
	}
	outbox, found, err := store.GetRecoveryOutbox(ctx, episode.ID, manualOp.OperationID)
	if err != nil || !found {
		t.Fatalf("manual adjudication outbox found=%v err=%v", found, err)
	}
	outbox.AppliedOperationID = manualOp.OperationID
	outbox.AppliedRevision = outbox.TargetEpisodeRevision
	outbox.AppliedHash = outbox.ProjectionHash
	outbox.State = accounting.RecoveryProjected
	if err = store.MarkRecoveryProjected(ctx, newOwner, outbox); err != nil {
		t.Fatalf("project manual adjudication: %v", err)
	}
	projectedEpisode, found, err := store.GetRecoveryEpisode(ctx, episode.ID)
	if err != nil || !found || projectedEpisode.State != accounting.RecoveryProjected || !projectedEpisode.HasCurrentOwnerTakeover() {
		t.Fatalf("manual adjudication projection episode=%+v found=%v err=%v", projectedEpisode, found, err)
	}
	newPermit := permit
	newPermit.ID = "permit-takeover-after-adjudication"
	newPermit.OwnerGeneration = newOwner.ID()
	newPermit.ExpectedRevision = projectedEpisode.Revision
	if _, err = store.CreateRecoveryPermit(ctx, newOwner, newPermit); !errors.Is(err, accounting.ErrRecoveryConflict) {
		t.Fatalf("takeover adjudication must remain execution-disabled after projection, got %v", err)
	}
	latestOwner, err := store.ClaimFundingCarryRuntimeGeneration(ctx, []string{scope})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = store.TakeOverRecoveryEpisode(ctx, newOwner, auth); !errors.Is(err, ErrFundingCarryRuntimeGenerationLost) {
		t.Fatalf("stale takeover generation = %v; want owner fence", err)
	}
	if latestOwner.ID() == newOwner.ID() {
		t.Fatal("runtime generation claim did not rotate owner token")
	}
}
