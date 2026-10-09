package storage

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sync/atomic"
	"testing"

	"github.com/mattn/go-sqlite3"
)

func fundingCarryTestScopeKeys() []string {
	return []string{fmt.Sprintf("%064x", 2), fmt.Sprintf("%064x", 1)}
}

func TestFundingCarryRuntimeCommitResultPreservesPostCommitCancellation(t *testing.T) {
	tests := []struct {
		name          string
		cancel        bool
		saved         bool
		wantSaved     bool
		wantConfirmed bool
		wantCanceled  bool
	}{
		{name: "committed and active", saved: true, wantSaved: true},
		{name: "committed then canceled", cancel: true, saved: true, wantConfirmed: true, wantCanceled: true},
		{name: "CAS conflict then canceled", cancel: true, wantConfirmed: false, wantCanceled: true},
		{name: "CAS conflict and active", saved: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			if tt.cancel {
				cancel()
			} else {
				defer cancel()
			}
			saved, err := fundingCarryRuntimeCommitResult(ctx, tt.saved)
			if saved != tt.wantSaved {
				t.Fatalf("saved=%v, want %v", saved, tt.wantSaved)
			}
			if errors.Is(err, ErrFundingCarryRuntimeStateCommitConfirmedCanceled) != tt.wantConfirmed {
				t.Fatalf("confirmed-canceled classification=%v, want %v (err=%v)", errors.Is(err, ErrFundingCarryRuntimeStateCommitConfirmedCanceled), tt.wantConfirmed, err)
			}
			if errors.Is(err, context.Canceled) != tt.wantCanceled {
				t.Fatalf("context cancellation classification=%v, want %v (err=%v)", errors.Is(err, context.Canceled), tt.wantCanceled, err)
			}
		})
	}
}

func TestFundingCarryRuntimeSetAndCASDetectCancellationAfterSuccessfulSQLCommit(t *testing.T) {
	for _, operation := range []string{"set", "cas"} {
		t.Run(operation, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "post-commit-cancel.db")
			base, err := NewSQLStorage(path)
			if err != nil {
				t.Fatal("open isolated SQLite storage:", err)
			}
			defer base.Close()
			generation, err := base.ClaimFundingCarryRuntimeGeneration(t.Context(), fundingCarryTestScopeKeys())
			if err != nil {
				t.Fatal("claim runtime generation:", err)
			}

			current := &StrategyRuntimeState{BotID: "post-commit-cancel", StrategyName: "funding_carry", SchemaVersion: 7, Payload: `{"state":"S0"}`}
			next := &StrategyRuntimeState{BotID: current.BotID, StrategyName: current.StrategyName, SchemaVersion: 7, Payload: `{"state":"S1"}`}
			if operation == "cas" {
				if err := base.SetFundingCarryRuntimeState(t.Context(), generation, current); err != nil {
					t.Fatal("seed CAS source:", err)
				}
			}

			armed := &atomic.Bool{}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			writerDB := sql.OpenDB(&cancelAfterCommitConnector{
				dsn: path + "?_journal_mode=WAL&_synchronous=NORMAL", armed: armed, cancel: cancel,
			})
			writerDB.SetMaxOpenConns(1)
			defer writerDB.Close()
			writer := &SQLStorage{db: writerDB, commitProbe: base.commitProbe, dbType: "sqlite"}
			armed.Store(true)
			if operation == "set" {
				err = writer.SetFundingCarryRuntimeState(ctx, generation, next)
				if !errors.Is(err, ErrFundingCarryRuntimeStateCommitConfirmedCanceled) || !errors.Is(err, context.Canceled) {
					t.Fatalf("successful Set commit followed by caller cancellation = %v, want confirmed-canceled", err)
				}
			} else {
				var saved bool
				saved, err = writer.CompareAndSwapFundingCarryRuntimeState(ctx, generation, next, current.SchemaVersion, current.Payload)
				if saved || !errors.Is(err, ErrFundingCarryRuntimeStateCommitConfirmedCanceled) || !errors.Is(err, context.Canceled) {
					t.Fatalf("successful CAS commit followed by caller cancellation = saved %v err %v, want false + confirmed-canceled", saved, err)
				}
			}
			if ctx.Err() != context.Canceled {
				t.Fatalf("test driver did not cancel caller after underlying SQL COMMIT returned nil: %v", ctx.Err())
			}
			persisted, err := base.GetStrategyRuntimeStateContext(t.Context(), next.BotID, next.StrategyName)
			if err != nil || persisted == nil || persisted.SchemaVersion != next.SchemaVersion || persisted.Payload != next.Payload {
				t.Fatalf("SQL transaction was not durably committed before cancellation: state=%+v err=%v", persisted, err)
			}
		})
	}
}

type cancelAfterCommitConnector struct {
	dsn    string
	armed  *atomic.Bool
	cancel context.CancelFunc
}

func (c *cancelAfterCommitConnector) Connect(ctx context.Context) (driver.Conn, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	conn, err := (&sqlite3.SQLiteDriver{}).Open(c.dsn)
	if err != nil {
		return nil, err
	}
	return &cancelAfterCommitConn{Conn: conn, armed: c.armed, cancel: c.cancel}, nil
}

func (c *cancelAfterCommitConnector) Driver() driver.Driver { return c }

func (c *cancelAfterCommitConnector) Open(string) (driver.Conn, error) {
	return (&sqlite3.SQLiteDriver{}).Open(c.dsn)
}

type cancelAfterCommitConn struct {
	driver.Conn
	armed  *atomic.Bool
	cancel context.CancelFunc
}

func (c *cancelAfterCommitConn) Begin() (driver.Tx, error) {
	tx, err := c.Conn.Begin()
	if err != nil {
		return nil, err
	}
	return &cancelAfterCommitTx{Tx: tx, armed: c.armed, cancel: c.cancel}, nil
}

func (c *cancelAfterCommitConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	beginner, ok := c.Conn.(driver.ConnBeginTx)
	if !ok {
		return nil, errors.New("SQLite connection does not support BeginTx")
	}
	tx, err := beginner.BeginTx(ctx, opts)
	if err != nil {
		return nil, err
	}
	return &cancelAfterCommitTx{Tx: tx, armed: c.armed, cancel: c.cancel}, nil
}

type cancelAfterCommitTx struct {
	driver.Tx
	armed  *atomic.Bool
	cancel context.CancelFunc
}

func (tx *cancelAfterCommitTx) Commit() error {
	if err := tx.Tx.Commit(); err != nil {
		return err
	}
	if tx.armed.CompareAndSwap(true, false) {
		tx.cancel()
	}
	return nil
}

func TestFundingCarryRuntimeGenerationSQLiteFirstClaimAndFencing(t *testing.T) {
	store, err := NewSQLStorage(filepath.Join(t.TempDir(), "funding-carry-generation.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	generations := FundingCarryRuntimeGenerationStore(store)

	scopes := fundingCarryTestScopeKeys()
	first, err := generations.ClaimFundingCarryRuntimeGeneration(t.Context(), scopes)
	if err != nil {
		t.Fatal("first complete claim:", err)
	}
	wantScopes := []string{scopes[1], scopes[0]}
	if !reflect.DeepEqual(first.scopeKeys, wantScopes) || len(first.ownerToken) != 64 {
		t.Fatalf("first generation = %+v, want sorted scopes %v and 256-bit token", first, wantScopes)
	}
	for _, scope := range wantScopes {
		var generation int64
		var token string
		if err := store.db.QueryRowContext(t.Context(), `SELECT generation, owner_token FROM funding_carry_runtime_generations WHERE scope_key = ?`, scope).Scan(&generation, &token); err != nil {
			t.Fatal("read claimed scope:", err)
		}
		if generation != 1 || token != first.ownerToken {
			t.Fatalf("scope %s generation/token = %d/%q, want 1/shared first token", scope, generation, token)
		}
	}

	initial := &StrategyRuntimeState{BotID: "fencing-bot", StrategyName: "funding_carry", SchemaVersion: 9, Payload: `{"stable":"initial"}`}
	if err := generations.SetFundingCarryRuntimeState(t.Context(), first, initial); err != nil {
		t.Fatal("first owner save:", err)
	}
	second, err := generations.ClaimFundingCarryRuntimeGeneration(t.Context(), scopes)
	if err != nil {
		t.Fatal("replacement complete claim:", err)
	}
	if second.ownerToken == first.ownerToken || !reflect.DeepEqual(second.scopeKeys, first.scopeKeys) {
		t.Fatalf("replacement generation = %+v, old=%+v", second, first)
	}

	staleSave := *initial
	staleSave.Payload = `{"stale":"save"}`
	if err := generations.SetFundingCarryRuntimeState(t.Context(), first, &staleSave); !errors.Is(err, ErrFundingCarryRuntimeGenerationLost) {
		t.Fatalf("old owner Save error = %v, want generation lost", err)
	}
	staleCAS := *initial
	staleCAS.Payload = `{"stale":"cas"}`
	if saved, err := generations.CompareAndSwapFundingCarryRuntimeState(t.Context(), first, &staleCAS, initial.SchemaVersion, initial.Payload); !errors.Is(err, ErrFundingCarryRuntimeGenerationLost) || saved {
		t.Fatalf("old owner CAS = saved %v, err %v; want generation lost", saved, err)
	}
	loaded, err := store.GetStrategyRuntimeState(initial.BotID, initial.StrategyName)
	if err != nil || loaded == nil || loaded.SchemaVersion != initial.SchemaVersion || loaded.Payload != initial.Payload {
		t.Fatalf("stale writes changed source payload: state=%+v err=%v", loaded, err)
	}

	currentSave := *initial
	currentSave.Payload = `{"current":"save"}`
	if err := generations.SetFundingCarryRuntimeState(t.Context(), second, &currentSave); err != nil {
		t.Fatal("current owner Save:", err)
	}
	currentCAS := *initial
	currentCAS.Payload = `{"current":"cas"}`
	if saved, err := generations.CompareAndSwapFundingCarryRuntimeState(t.Context(), second, &currentCAS, currentSave.SchemaVersion, currentSave.Payload); err != nil || !saved {
		t.Fatalf("current owner CAS = saved %v, err %v", saved, err)
	}
	loaded, err = store.GetStrategyRuntimeState(initial.BotID, initial.StrategyName)
	if err != nil || loaded == nil || loaded.Payload != currentCAS.Payload {
		t.Fatalf("current owner result = %+v, err %v", loaded, err)
	}
}

func TestFundingCarryRuntimeGenerationSQLiteClaimRollsBackAllScopes(t *testing.T) {
	store, err := NewSQLStorage(filepath.Join(t.TempDir(), "funding-carry-generation-rollback.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	generations := FundingCarryRuntimeGenerationStore(store)
	scopes := fundingCarryTestScopeKeys()
	failScope := scopes[0] // normalized lexical first scope
	trigger := fmt.Sprintf(`CREATE TRIGGER reject_generation_advance BEFORE UPDATE ON funding_carry_runtime_generations WHEN OLD.scope_key = '%s' BEGIN SELECT RAISE(ABORT, 'injected generation claim failure'); END`, failScope)
	if _, err := store.db.ExecContext(t.Context(), trigger); err != nil {
		t.Fatal("install deterministic claim failure:", err)
	}
	if generation, err := generations.ClaimFundingCarryRuntimeGeneration(t.Context(), scopes); err == nil || generation.ownerToken != "" {
		t.Fatalf("partially successful claim returned generation=%+v err=%v", generation, err)
	}
	var rows int
	if err := store.db.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM funding_carry_runtime_generations WHERE scope_key IN (?, ?)`, scopes[0], scopes[1]).Scan(&rows); err != nil {
		t.Fatal("count scopes after failed claim:", err)
	}
	if rows != 0 {
		t.Fatalf("failed all-scope claim left %d scope rows; want transaction rollback to zero", rows)
	}
}

func TestFundingCarryRuntimeGenerationSQLiteClaimRejectsInvalidSet(t *testing.T) {
	store, err := NewSQLStorage(filepath.Join(t.TempDir(), "funding-carry-generation-invalid.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	generations := FundingCarryRuntimeGenerationStore(store)
	valid := fundingCarryTestScopeKeys()
	for _, scopes := range [][]string{nil, {valid[0], valid[0]}, {"not-a-scope"}} {
		if _, err := generations.ClaimFundingCarryRuntimeGeneration(t.Context(), scopes); err == nil {
			t.Fatalf("invalid scope set accepted: %v", scopes)
		}
	}
}

func TestFundingCarryRuntimeGenerationMigrationPreservesLegacyRuntimePayloads(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy-runtime-state.db")
	legacyDB, err := sql.Open("sqlite3", path+"?_journal_mode=WAL&_synchronous=NORMAL")
	if err != nil {
		t.Fatal("open legacy SQLite database:", err)
	}
	if _, err := legacyDB.Exec(`CREATE TABLE strategy_runtime_states (
		bot_id TEXT NOT NULL,
		strategy_name TEXT NOT NULL,
		schema_version INTEGER NOT NULL,
		payload TEXT NOT NULL,
		updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
		PRIMARY KEY (bot_id, strategy_name)
	)`); err != nil {
		t.Fatal("create pre-generation runtime-state schema:", err)
	}
	legacy := []StrategyRuntimeState{
		{BotID: "legacy-fc-a", StrategyName: "funding_carry", SchemaVersion: 8, Payload: `{"debt":0.125,"note":"keep exact bytes"}`},
		{BotID: "legacy-fc-b", StrategyName: "funding_carry", SchemaVersion: 11, Payload: `{"unknown":true,"text":"é"}`},
	}
	for _, state := range legacy {
		if _, err := legacyDB.Exec(`INSERT INTO strategy_runtime_states (bot_id, strategy_name, schema_version, payload) VALUES (?, ?, ?, ?)`, state.BotID, state.StrategyName, state.SchemaVersion, state.Payload); err != nil {
			t.Fatal("seed legacy runtime payload:", err)
		}
	}
	if err := legacyDB.Close(); err != nil {
		t.Fatal("close pre-migration database:", err)
	}

	upgraded, err := NewSQLStorage(path)
	if err != nil {
		t.Fatal("open and migrate legacy database:", err)
	}
	defer upgraded.Close()
	for _, want := range legacy {
		got, err := upgraded.GetStrategyRuntimeState(want.BotID, want.StrategyName)
		if err != nil || got == nil || got.SchemaVersion != want.SchemaVersion || got.Payload != want.Payload {
			t.Errorf("legacy state %s changed across migration: got=%+v err=%v want version=%d payload=%q", want.BotID, got, err, want.SchemaVersion, want.Payload)
		}
	}
	var rows int
	if err := upgraded.db.QueryRow(`SELECT COUNT(*) FROM funding_carry_runtime_generations`).Scan(&rows); err != nil {
		t.Fatal("new generation table missing after migration:", err)
	}
	if rows != 0 {
		t.Fatalf("migration unexpectedly inserted owner generations: %d rows", rows)
	}
	for _, migration := range []string{
		"migrations/2026100702_funding_carry_runtime_state_receipts_sqlite.down.sql",
		"migrations/2026100701_funding_carry_runtime_generation_sqlite.down.sql",
	} {
		down, err := os.ReadFile(migration)
		if err != nil {
			t.Fatalf("read SQLite down migration %s: %v", migration, err)
		}
		if _, err := upgraded.db.Exec(string(down)); err != nil {
			t.Fatalf("apply SQLite down migration %s: %v", migration, err)
		}
	}
	for _, table := range []string{"funding_carry_runtime_state_write_receipts", "funding_carry_runtime_generations"} {
		var exists int
		if err := upgraded.db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&exists); err != nil || exists != 0 {
			t.Fatalf("SQLite down migration left table %s (exists=%d): %v", table, exists, err)
		}
	}
}
