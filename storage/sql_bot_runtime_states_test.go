package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

func newBotRuntimeListTestStore(t *testing.T) *SQLStorage {
	t.Helper()
	store, err := NewSQLStorage(filepath.Join(t.TempDir(), "bot-runtime-list.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("close fixture store: %v", err)
		}
	})
	return store
}

func TestBotRuntimeStateListIncludesAllOldStrategyNamesAndPreservesEvidence(t *testing.T) {
	store := newBotRuntimeListTestStore(t)
	when := time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC)
	const count = 105 // More than a common audit page size; no silent truncation.
	for index := count - 1; index >= 0; index-- {
		state := &StrategyRuntimeState{BotID: "owner", StrategyName: fmt.Sprintf("old-strategy-%03d", index), SchemaVersion: index + 1,
			Payload: fmt.Sprintf(`{"unresolved":true,"entry":%d}`, index), UpdatedAt: when}
		if err := store.SetStrategyRuntimeState(state); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.SetStrategyRuntimeState(&StrategyRuntimeState{BotID: "other", StrategyName: "funding_carry", SchemaVersion: 6, Payload: `{}`}); err != nil {
		t.Fatal(err)
	}
	states, err := store.ListBotStrategyRuntimeStatesContext(t.Context(), "owner")
	if err != nil || len(states) != count {
		t.Fatalf("complete owner read: got %d records: %v", len(states), err)
	}
	for index, state := range states {
		if state.BotID != "owner" || state.StrategyName != fmt.Sprintf("old-strategy-%03d", index) || state.SchemaVersion != index+1 ||
			state.Payload != fmt.Sprintf(`{"unresolved":true,"entry":%d}`, index) || !state.UpdatedAt.Equal(when) {
			t.Fatalf("record %d lost identity, order, version or financial evidence", index)
		}
	}
	states[0].Payload = "caller mutation"
	original, err := store.GetStrategyRuntimeState("owner", "old-strategy-000")
	if err != nil || original == nil || original.Payload != `{"unresolved":true,"entry":0}` {
		t.Fatalf("read result mutation changed durable evidence: %v", err)
	}
	for _, botID := range []string{"absent", "owner' OR 1=1 --"} {
		empty, err := store.ListBotStrategyRuntimeStatesContext(t.Context(), botID)
		if err != nil || empty == nil || len(empty) != 0 {
			t.Fatalf("confirmed absence/parameter isolation failed: %v", err)
		}
	}
}

func TestBotRuntimeStateListConnectionWaitCancelsAndRecovers(t *testing.T) {
	store := newBotRuntimeListTestStore(t)
	if err := store.SetStrategyRuntimeState(&StrategyRuntimeState{BotID: "owner", StrategyName: "old", SchemaVersion: 6, Payload: `{"pending":true}`}); err != nil {
		t.Fatal(err)
	}
	store.db.SetMaxOpenConns(1)
	conn, err := store.db.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := conn.Close(); err != nil && !errors.Is(err, sql.ErrConnDone) {
			t.Errorf("close fixture connection: %v", err)
		}
	})
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	states, err := store.ListBotStrategyRuntimeStatesContext(ctx, "owner")
	if !errors.Is(err, context.DeadlineExceeded) || states != nil {
		t.Fatalf("connection wait returned partial or empty proof: %v", err)
	}
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}
	states, err = store.ListBotStrategyRuntimeStatesContext(t.Context(), "owner")
	if err != nil || len(states) != 1 || states[0].Payload != `{"pending":true}` {
		t.Fatalf("cancellation broke subsequent complete reads: %v", err)
	}
}

func TestBotRuntimeStateListRejectsInvalidInputAndClosedDatabase(t *testing.T) {
	store := newBotRuntimeListTestStore(t)
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	for _, tc := range []struct {
		name  string
		ctx   context.Context
		store *SQLStorage
		botID string
	}{
		{name: "nil context", store: store, botID: "owner"},
		{name: "nil store", ctx: t.Context(), botID: "owner"},
		{name: "nil DB", ctx: t.Context(), store: &SQLStorage{}, botID: "owner"},
		{name: "empty owner", ctx: t.Context(), store: store, botID: " "},
		{name: "canceled", ctx: canceled, store: store, botID: "owner"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			states, err := tc.store.ListBotStrategyRuntimeStatesContext(tc.ctx, tc.botID)
			if err == nil || states != nil {
				t.Fatal("invalid read certified absence")
			}
			if tc.name == "canceled" && !errors.Is(err, context.Canceled) {
				t.Fatal("cancellation cause lost")
			}
		})
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if states, err := store.ListBotStrategyRuntimeStatesContext(t.Context(), "owner"); err == nil || states != nil {
		t.Fatal("closed database certified absence")
	}
}

func TestBotRuntimeStateListDiscardsPartialScanEvidence(t *testing.T) {
	store := newBotRuntimeListTestStore(t)
	if err := store.SetStrategyRuntimeState(&StrategyRuntimeState{BotID: "owner", StrategyName: "a-valid", SchemaVersion: 6, Payload: `{"pending":true}`}); err != nil {
		t.Fatal(err)
	}
	// Corrupt only this disposable fixture's second timestamp. The first row
	// is valid and sorted first, so an error cannot expose it as a complete list.
	if _, err := store.db.Exec(`INSERT INTO strategy_runtime_states (bot_id, strategy_name, schema_version, payload, updated_at)
		VALUES (?, ?, ?, ?, ?)`, "owner", "z-invalid", 6, `{}`, []byte("invalid-timestamp")); err != nil {
		t.Fatal(err)
	}
	states, err := store.ListBotStrategyRuntimeStatesContext(t.Context(), "owner")
	if err == nil || states != nil {
		t.Fatal("second-row scan failure exposed partial financial proof")
	}
	first, err := store.GetStrategyRuntimeState("owner", "a-valid")
	if err != nil || first == nil || first.Payload != `{"pending":true}` {
		t.Fatalf("failed read changed valid durable state: %v", err)
	}
}
