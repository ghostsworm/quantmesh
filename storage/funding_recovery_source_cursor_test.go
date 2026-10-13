package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"quantmesh/accounting"
)

type testRecoverySourceWindowVerifier struct{}

func (testRecoverySourceWindowVerifier) VerifyRecoverySourceWindow(_ context.Context, window accounting.RecoverySourceWindow, events []accounting.RecoveryEvent) error {
	if err := window.Validate(); err != nil {
		return err
	}
	if uint64(len(events)) != window.Rows {
		return errors.New("test verifier row count mismatch")
	}
	for _, event := range events {
		if event.Source != window.Source || event.Exchange != window.Exchange || event.Market != window.Market || event.AccountScope != window.AccountScope || event.Symbol != window.Symbol || event.Kind != window.Kind || event.Asset != window.Asset || event.ObservedAt.Before(window.From) || !event.ObservedAt.Before(window.Through) {
			return errors.New("test verifier event identity mismatch")
		}
	}
	return nil
}

func newSourceCursorFixture(t *testing.T, episodeID string, configureVerifier ...bool) (*SQLStorage, FundingCarryRuntimeGeneration, accounting.RecoveryEpisode, string) {
	t.Helper()
	store, err := NewSQLStorage(filepath.Join(t.TempDir(), "source-cursor.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	ctx := context.Background()
	if err = store.MigrateFundingRecovery(ctx); err != nil {
		t.Fatal(err)
	}
	if err = store.MigrateFundingRecovery(ctx); err != nil {
		t.Fatalf("repeat idempotent funding recovery migrations: %v", err)
	}
	scope := fmt.Sprintf("%064x", 808)
	generation, err := store.ClaimFundingCarryRuntimeGeneration(ctx, []string{scope})
	if err != nil {
		t.Fatal(err)
	}
	episode := accounting.RecoveryEpisode{
		ID: episodeID, Bot: "bot-cursor", Strategy: "FundingCarry", AccountScopes: []string{scope},
		OwnerGeneration: generation.ID(), Revision: 1, RecoveryGeneration: 1, State: accounting.RecoveryPrepared,
	}
	if _, err = store.CreateRecoveryEpisode(ctx, generation, episode); err != nil {
		t.Fatal(err)
	}
	if len(configureVerifier) == 0 || configureVerifier[0] {
		if err = store.SetRecoverySourceWindowVerifier(testRecoverySourceWindowVerifier{}); err != nil {
			t.Fatal("configure test source verifier:", err)
		}
	}
	return store, generation, episode, scope
}

func sourceWindow(scope string, from, through time.Time, revision, rows uint64) accounting.RecoverySourceWindow {
	return sourceWindowForSymbol(scope, "BTCUSDT", from, through, revision, rows)
}

func sourceWindowForSymbol(scope, symbol string, from, through time.Time, revision, rows uint64) accounting.RecoverySourceWindow {
	return accounting.RecoverySourceWindow{
		Source: "binance_income_history", Exchange: "binance", Market: "futures", AccountScope: scope,
		Kind: accounting.RecoveryFundingEvent, Symbol: symbol, Asset: "USDT", From: from, Through: through,
		ExpectedRevision: revision, Rows: rows, TotalRows: rows,
		CompletionMarker: accounting.RecoverySourceWindowCompleteV1, EvidenceHash: fmt.Sprintf("%064x", 809+revision+rows),
	}
}

func TestFundingRecoverySourceCursorIdentityIsSymbolScopedAndLegacyCompatible(t *testing.T) {
	store, _, _, scope := newSourceCursorFixture(t, "episode-symbol-scope")
	legacy := struct {
		Source       string                       `json:"source"`
		Exchange     string                       `json:"exchange"`
		Market       string                       `json:"market"`
		AccountScope string                       `json:"account_scope"`
		Kind         accounting.RecoveryEventKind `json:"event_type"`
		Asset        string                       `json:"asset"`
	}{"legacy-source", "binance", "futures", scope, accounting.RecoveryInterestEvent, "BTC"}
	legacyJSON, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(legacyJSON)
	if got := recoverySourceCursorKey(accounting.RecoverySourceIdentity{Source: legacy.Source, Exchange: legacy.Exchange, Market: legacy.Market, AccountScope: legacy.AccountScope, Kind: legacy.Kind, Asset: legacy.Asset}); got != hex.EncodeToString(digest[:]) {
		t.Fatal("legacy non-funding cursor identity hash changed")
	}
	from := time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)
	window := accounting.RecoverySourceWindow{Source: legacy.Source, Exchange: legacy.Exchange, Market: legacy.Market, AccountScope: scope,
		Kind: legacy.Kind, Asset: legacy.Asset, From: from, Through: from.Add(time.Hour), CompletionMarker: accounting.RecoverySourceWindowCompleteV1,
		EvidenceHash: fmt.Sprintf("%064x", 900)}
	cursor := accounting.RecoverySourceCursor{RecoverySourceWindow: window, EpisodeID: "legacy-interest-episode", Revision: 1, UpdatedAt: from.Add(time.Hour)}
	legacyPayload, err := json.Marshal(cursor)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(t.Context(), `INSERT INTO funding_recovery_source_cursors(cursor_key,episode_id,revision,source,exchange_name,market,account_scope,symbol,event_type,asset,window_from,window_through,complete_rows,total_rows,completion_marker,evidence_hash,payload_json) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		recoverySourceCursorKey(window.Identity()), cursor.EpisodeID, cursor.Revision, window.Source, window.Exchange, window.Market, window.AccountScope, "", string(window.Kind), window.Asset, window.From, window.Through, window.Rows, window.TotalRows, window.CompletionMarker, window.EvidenceHash, legacyPayload); err != nil {
		t.Fatal("insert legacy account-level cursor:", err)
	}
	loaded, found, err := store.GetRecoverySourceCursor(t.Context(), window.Identity())
	if err != nil || !found || loaded.EpisodeID != cursor.EpisodeID || loaded.Revision != cursor.Revision {
		t.Fatalf("legacy non-funding cursor readback = (%+v, %t, %v)", loaded, found, err)
	}
}

func TestFundingRecoverySourceCursorsDoNotCollideAcrossSymbols(t *testing.T) {
	store, generation, episode, scope := newSourceCursorFixture(t, "episode-symbol-scoped-cursors")
	start := time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC)
	for index, symbol := range []string{"BTCUSDT", "ETHUSDT"} {
		window := sourceWindowForSymbol(scope, symbol, start, start.Add(time.Hour), 0, 1)
		current, found, err := store.GetRecoveryEpisode(t.Context(), episode.ID)
		if err != nil || !found {
			t.Fatalf("read current source episode before %s: found=%t err=%v", symbol, found, err)
		}
		operation := fundingOperation(current, scope, "symbol-window-"+symbol, current.Revision, window, "same-transaction-id", "-0.01")
		operation.Events[0].ID = "event-" + symbol
		if _, _, err := store.ApplyRecoveryOperation(t.Context(), generation, operation); err != nil {
			t.Fatalf("apply %s source window: %v", symbol, err)
		}
		if index == 0 {
			projectSourceCursorOperation(t, store, generation, episode.ID, operation.OperationID)
		}
		cursor, found, err := store.GetRecoverySourceCursor(t.Context(), window.Identity())
		if err != nil || !found || cursor.Symbol != symbol {
			t.Fatalf("%s cursor = (%+v, %t, %v)", symbol, cursor, found, err)
		}
	}
	events, err := store.ListRecoveryEvents(t.Context(), episode.ID)
	if err != nil || len(events) != 2 || events[0].ExternalID != events[1].ExternalID || events[0].Symbol == events[1].Symbol {
		t.Fatalf("same external transaction ID across symbols was not retained independently: (%+v, %v)", events, err)
	}
}

func fundingOperation(episode accounting.RecoveryEpisode, scope, operationID string, expectedEpisodeRevision uint64, window accounting.RecoverySourceWindow, externalID, amount string) accounting.RecoveryOperation {
	op := accounting.RecoveryOperation{
		EpisodeID: episode.ID, OperationID: operationID, HashVersion: 1,
		ExpectedEpisodeRevision: expectedEpisodeRevision, ExpectedRuntimeRevision: fmt.Sprintf("runtime-%d", expectedEpisodeRevision),
		ExpectedChildRevisions: map[string]uint64{}, ExpectedEventRevisions: map[string]uint64{},
		Actor: "auditor", AuditRef: "incident-rc1174", EvidenceHash: fmt.Sprintf("%064x", 810+expectedEpisodeRevision),
		EvidenceReadAt: time.Now().UTC(), SourceWindow: &window,
	}
	if externalID != "" {
		op.Events = []accounting.RecoveryEvent{{
			ID: "event-" + externalID, Source: window.Source, Exchange: window.Exchange, Market: window.Market, Symbol: window.Symbol,
			AccountScope: scope, Kind: window.Kind, ExternalID: externalID, EpisodeID: episode.ID,
			Asset: window.Asset, Amount: amount, SourceRevision: "source-rev-1", SourceHash: fmt.Sprintf("%064x", 811),
			ObservedAt: window.From.Add(time.Second),
		}}
	}
	return op
}

func projectSourceCursorOperation(t *testing.T, store *SQLStorage, generation FundingCarryRuntimeGeneration, episodeID, operationID string) {
	t.Helper()
	outbox, found, err := store.GetRecoveryOutbox(t.Context(), episodeID, operationID)
	if err != nil || !found {
		t.Fatalf("read source cursor outbox: found=%t err=%v", found, err)
	}
	outbox.AppliedOperationID = operationID
	outbox.AppliedRevision = outbox.TargetEpisodeRevision
	outbox.AppliedHash = outbox.ProjectionHash
	outbox.State = accounting.RecoveryProjected
	if err = store.MarkRecoveryProjected(t.Context(), generation, outbox); err != nil {
		t.Fatalf("project source cursor outbox: %v", err)
	}
}

func TestSQLiteFundingRecoverySourceCursorCASIdempotencyAndAtomicOutbox(t *testing.T) {
	store, generation, episode, scope := newSourceCursorFixture(t, "episode-source-cursor-cas")
	ctx := t.Context()
	start := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	window := sourceWindow(scope, start, end, 0, 1)
	op := fundingOperation(episode, scope, "source-window-1", 1, window, "funding-1", "-0.000000000000000001")
	receipt, replayed, err := store.ApplyRecoveryOperation(ctx, generation, op)
	if err != nil || replayed {
		t.Fatalf("apply complete source window: receipt=%+v replayed=%t err=%v", receipt, replayed, err)
	}
	replayedReceipt, replayed, err := store.ApplyRecoveryOperation(ctx, generation, op)
	if err != nil || !replayed || replayedReceipt.RequestHash != receipt.RequestHash {
		t.Fatalf("same operation replay: receipt=%+v replayed=%t err=%v", replayedReceipt, replayed, err)
	}
	conflict := op
	conflict.Events = append([]accounting.RecoveryEvent(nil), op.Events...)
	conflict.Events[0].Amount = "-2"
	if _, _, err = store.ApplyRecoveryOperation(ctx, generation, conflict); !errors.Is(err, accounting.ErrRecoveryOperationConflict) {
		t.Fatalf("same operation with different payload = %v; want idempotency conflict", err)
	}
	cursor, found, err := store.GetRecoverySourceCursor(ctx, window.Identity())
	if err != nil || !found || cursor.EpisodeID != episode.ID || cursor.Revision != 1 || cursor.Rows != 1 || cursor.TotalRows != 1 || cursor.EvidenceHash != window.EvidenceHash {
		t.Fatalf("durable cursor = %+v found=%t err=%v", cursor, found, err)
	}
	var outboxPayload string
	if err = store.db.QueryRowContext(ctx, `SELECT CAST(payload_json AS TEXT) FROM funding_recovery_outbox WHERE episode_id=? AND operation_id=?`, episode.ID, op.OperationID).Scan(&outboxPayload); err != nil {
		t.Fatal("read projection outbox:", err)
	}
	if !strings.Contains(outboxPayload, "source_cursor") || !strings.Contains(outboxPayload, window.EvidenceHash) {
		t.Fatalf("outbox omitted committed source cursor projection: %s", outboxPayload)
	}
}

func TestSQLiteFundingRecoveryConcurrentOperationAcrossHandlesHasSingleCommit(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "funding-recovery-concurrent-operation.db")
	firstStore, err := NewSQLStorage(databasePath)
	if err != nil {
		t.Fatal("open first SQL handle:", err)
	}
	t.Cleanup(func() { _ = firstStore.Close() })
	secondStore, err := NewSQLStorage(databasePath)
	if err != nil {
		t.Fatal("open second SQL handle:", err)
	}
	t.Cleanup(func() { _ = secondStore.Close() })
	ctx := t.Context()
	for index, store := range []*SQLStorage{firstStore, secondStore} {
		if err := store.MigrateFundingRecovery(ctx); err != nil {
			t.Fatalf("migrate independent SQL handle %d: %v", index, err)
		}
		if err := store.SetRecoverySourceWindowVerifier(testRecoverySourceWindowVerifier{}); err != nil {
			t.Fatalf("configure independent SQL handle %d verifier: %v", index, err)
		}
	}
	scope := fmt.Sprintf("%064x", 1182)
	generation, err := firstStore.ClaimFundingCarryRuntimeGeneration(ctx, []string{scope})
	if err != nil {
		t.Fatal("claim common runtime generation:", err)
	}
	episode := accounting.RecoveryEpisode{ID: "episode-concurrent-operation", Bot: "bot-concurrent-operation", Strategy: "FundingCarry",
		AccountScopes: []string{scope}, OwnerGeneration: generation.ID(), Revision: 1, RecoveryGeneration: 1, State: accounting.RecoveryPrepared}
	if _, err := firstStore.CreateRecoveryEpisode(ctx, generation, episode); err != nil {
		t.Fatal("create shared recovery episode:", err)
	}
	from := time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC)
	window := sourceWindow(scope, from, from.Add(time.Minute), 0, 1)
	op := fundingOperation(episode, scope, "same-operation-two-handles", 1, window, "same-funding-event", "-0.00000001")
	secondEvent := op.Events[0]
	secondEvent.ID, secondEvent.ExternalID, secondEvent.Amount = "event-funding-a", "funding-event-a", "-0.00000002"
	secondEvent.SourceRevision, secondEvent.SourceHash = "source-rev-2", fmt.Sprintf("%064x", 2)
	secondEvent.ObservedAt = from.Add(30 * time.Second)
	op.Events[0].ID, op.Events[0].ExternalID = "event-funding-z", "funding-event-z"
	op.Events = append(op.Events, secondEvent)
	op.SourceWindow.Rows, op.SourceWindow.TotalRows = 2, 2
	start := make(chan struct{})
	type result struct {
		receipt  accounting.RecoveryReceipt
		replayed bool
		err      error
	}
	results := make([]result, 2)
	stores := []*SQLStorage{firstStore, secondStore}
	var workers sync.WaitGroup
	for index, store := range stores {
		workers.Add(1)
		go func(index int, store *SQLStorage) {
			defer workers.Done()
			<-start
			results[index].receipt, results[index].replayed, results[index].err = store.ApplyRecoveryOperation(ctx, generation, op)
		}(index, store)
	}
	close(start)
	workers.Wait()
	newCommits, idempotentReads := 0, 0
	for index, got := range results {
		if got.err != nil {
			t.Fatalf("independent handle %d apply same operation: %v", index, got.err)
		}
		if got.replayed {
			idempotentReads++
		} else {
			newCommits++
		}
		if got.receipt.OperationID != op.OperationID || got.receipt.RequestHash == "" {
			t.Fatalf("independent handle %d returned an unbound receipt: %+v", index, got.receipt)
		}
	}
	if newCommits != 1 || idempotentReads != 1 || results[0].receipt.RequestHash != results[1].receipt.RequestHash {
		t.Fatalf("same operation across handles did not converge to one commit: commits=%d replays=%d receipts=%+v", newCommits, idempotentReads, results)
	}
	readback, found, err := secondStore.GetRecoveryReceipt(ctx, episode.ID, op.OperationID)
	if err != nil || !found || readback.RequestHash != results[0].receipt.RequestHash {
		t.Fatalf("receipt did not durably converge across handles: receipt=%+v found=%t err=%v", readback, found, err)
	}
	events, err := secondStore.ListRecoveryEvents(ctx, episode.ID)
	if err != nil || len(events) != 2 {
		t.Fatalf("concurrent operation duplicated or lost ledger facts: events=%+v err=%v", events, err)
	}
	gotAmounts := make(map[string]string, len(events))
	for _, event := range events {
		gotAmounts[event.ExternalID] = event.Amount
	}
	if gotAmounts["funding-event-a"] != "-0.00000002" || gotAmounts["funding-event-z"] != "-0.00000001" {
		t.Fatalf("concurrent operation persisted unexpected funding facts: %+v", gotAmounts)
	}
	cursor, found, err := secondStore.GetRecoverySourceCursor(ctx, window.Identity())
	if err != nil || !found || cursor.EpisodeID != episode.ID || cursor.Revision != 1 || cursor.Rows != 2 || cursor.TotalRows != 2 || cursor.EvidenceHash != window.EvidenceHash {
		t.Fatalf("concurrent operation duplicated or lost source cursor: cursor=%+v found=%t err=%v", cursor, found, err)
	}
	outboxes, err := secondStore.ListRecoveryOutboxes(ctx, episode.ID)
	if err != nil || len(outboxes) != 1 || outboxes[0].OperationID != op.OperationID || outboxes[0].TargetEpisodeRevision != results[0].receipt.EpisodeRevision || outboxes[0].ProjectionHash != results[0].receipt.ProjectionHash {
		t.Fatalf("concurrent operation duplicated or lost outbox: outboxes=%+v err=%v", outboxes, err)
	}
	currentEpisode, found, err := secondStore.GetRecoveryEpisode(ctx, episode.ID)
	if err != nil || !found || currentEpisode.Revision != results[0].receipt.EpisodeRevision || currentEpisode.Revision != 2 {
		t.Fatalf("concurrent operation episode revision = %+v found=%t err=%v", currentEpisode, found, err)
	}
}

func TestSQLiteFundingRecoverySourceCursorFailsClosedWithoutTrustedVerifier(t *testing.T) {
	store, generation, episode, scope := newSourceCursorFixture(t, "episode-source-cursor-no-verifier", false)
	start := time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)
	window := sourceWindow(scope, start, start.Add(time.Hour), 0, 0)
	if _, _, err := store.ApplyRecoveryOperation(t.Context(), generation, fundingOperation(episode, scope, "untrusted-window", 1, window, "", "")); !errors.Is(err, ErrRecoverySourceWindowVerifierUnavailable) {
		t.Fatalf("unverified complete marker = %v; want fail-closed verifier error", err)
	}
	if _, found, err := store.GetRecoverySourceCursor(t.Context(), window.Identity()); err != nil || found {
		t.Fatalf("unverified cursor persisted found=%t err=%v", found, err)
	}
}

func TestSQLiteFundingRecoverySourceCursorRejectsEventDimensionMismatch(t *testing.T) {
	store, generation, episode, scope := newSourceCursorFixture(t, "episode-source-cursor-dimensions")
	start := time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC)
	window := sourceWindow(scope, start, start.Add(time.Hour), 0, 1)
	tests := []struct {
		name   string
		mutate func(*accounting.RecoveryEvent)
	}{
		{name: "source", mutate: func(e *accounting.RecoveryEvent) { e.Source = "other-source" }},
		{name: "exchange", mutate: func(e *accounting.RecoveryEvent) { e.Exchange = "other-exchange" }},
		{name: "market", mutate: func(e *accounting.RecoveryEvent) { e.Market = "spot" }},
		{name: "account scope", mutate: func(e *accounting.RecoveryEvent) { e.AccountScope = fmt.Sprintf("%064x", 991) }},
		{name: "event kind", mutate: func(e *accounting.RecoveryEvent) { e.Kind = accounting.RecoveryInterestEvent }},
		{name: "asset", mutate: func(e *accounting.RecoveryEvent) { e.Asset = "BTC" }},
		{name: "window upper bound", mutate: func(e *accounting.RecoveryEvent) { e.ObservedAt = window.Through }},
	}
	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			op := fundingOperation(episode, scope, fmt.Sprintf("dimension-mismatch-%d", i), 1, window, "dimension-event", "-1")
			tt.mutate(&op.Events[0])
			if _, _, err := store.ApplyRecoveryOperation(t.Context(), generation, op); err == nil {
				t.Fatal("cursor accepted an event outside the exact source dimensions/window")
			}
		})
	}
}

func TestFundingRecoveryReopenCannotBypassDebtSourceCursor(t *testing.T) {
	store, generation, episode, scope := newSourceCursorFixture(t, "episode-reopen-debt-cursor")
	event := accounting.RecoveryEvent{
		ID: "reopen-borrow", Source: "binance_margin_history", Exchange: "binance", Market: "spot_margin",
		AccountScope: scope, Kind: accounting.RecoveryBorrowEvent, ExternalID: "borrow-reopen", EpisodeID: episode.ID,
		Asset: "BTC", Amount: "0.1", SourceRevision: "1", SourceHash: fmt.Sprintf("%064x", 1200), ObservedAt: time.Now().UTC(),
		DebtEvidence: &accounting.RecoveryDebtEvidence{PrincipalRaw: "0.1", InterestRaw: "0"},
	}
	if _, err := store.ReopenRecoveryEpisode(t.Context(), generation, episode.ID, episode.Revision, event, "reopen-op"); err == nil || !strings.Contains(err.Error(), "complete source window") {
		t.Fatalf("reopen debt event = %v; want source-window fail-closed rejection", err)
	}
}

func TestSQLiteFundingRecoverySourceCursorMigrationForwardAndRollback(t *testing.T) {
	store, _, _, _ := newSourceCursorFixture(t, "episode-source-cursor-migration")
	applyMigration := func(path string) {
		t.Helper()
		body, err := fundingRecoveryMigrations.ReadFile(path)
		if err != nil {
			t.Fatalf("read migration %s: %v", path, err)
		}
		for _, statement := range strings.Split(string(body), ";") {
			statement = strings.TrimSpace(statement)
			if statement == "" {
				continue
			}
			if _, err = store.db.ExecContext(t.Context(), statement); err != nil {
				t.Fatalf("execute migration %s: %v", path, err)
			}
		}
	}
	var count int
	if err := store.db.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='funding_recovery_source_cursors'`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("forward migration table count=%d err=%v", count, err)
	}
	applyMigration("migrations/2026101202_funding_recovery_sqlite.down.sql")
	if err := store.db.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='funding_recovery_source_cursors'`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("rollback migration table count=%d err=%v", count, err)
	}
	applyMigration("migrations/2026101202_funding_recovery_sqlite.up.sql")
	if err := store.db.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='funding_recovery_source_cursors'`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("reapply forward migration table count=%d err=%v", count, err)
	}
}

func TestSQLiteFundingRecoverySourceCursorRejectsIncompleteGapAndOldCAS(t *testing.T) {
	store, generation, episode, scope := newSourceCursorFixture(t, "episode-source-cursor-gap")
	ctx := t.Context()
	start := time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	incomplete := sourceWindow(scope, start, end, 0, 1)
	incomplete.TotalRows = 2
	if _, _, err := store.ApplyRecoveryOperation(ctx, generation, fundingOperation(episode, scope, "incomplete-window", 1, incomplete, "funding-incomplete", "-1")); err == nil {
		t.Fatal("incomplete source window was accepted")
	}
	first := sourceWindow(scope, start, end, 0, 1)
	if _, _, err := store.ApplyRecoveryOperation(ctx, generation, fundingOperation(episode, scope, "window-first", 1, first, "funding-first", "-1")); err != nil {
		t.Fatalf("apply first source window: %v", err)
	}
	projectSourceCursorOperation(t, store, generation, episode.ID, "window-first")
	gapStart := end.Add(time.Millisecond)
	gap := sourceWindow(scope, gapStart, gapStart.Add(time.Hour), 1, 1)
	if _, _, err := store.ApplyRecoveryOperation(ctx, generation, fundingOperation(episode, scope, "window-gap", 3, gap, "funding-gap", "-1")); !errors.Is(err, accounting.ErrRecoveryConflict) {
		t.Fatalf("gapped source window = %v; want CAS conflict", err)
	}
	oldCAS := sourceWindow(scope, end, end.Add(time.Hour), 0, 1)
	if _, _, err := store.ApplyRecoveryOperation(ctx, generation, fundingOperation(episode, scope, "window-old-cas", 3, oldCAS, "funding-old-cas", "-1")); !errors.Is(err, accounting.ErrRecoveryConflict) {
		t.Fatalf("old source cursor revision = %v; want CAS conflict", err)
	}
	contiguous := sourceWindow(scope, end, end.Add(time.Hour), 1, 1)
	if _, _, err := store.ApplyRecoveryOperation(ctx, generation, fundingOperation(episode, scope, "window-contiguous", 3, contiguous, "funding-next", "-2")); err != nil {
		t.Fatalf("apply contiguous source window: %v", err)
	}
	projectSourceCursorOperation(t, store, generation, episode.ID, "window-contiguous")
	cursor, found, err := store.GetRecoverySourceCursor(ctx, contiguous.Identity())
	if err != nil || !found || cursor.Revision != 2 || !cursor.Through.Equal(contiguous.Through) {
		t.Fatalf("contiguous cursor = %+v found=%t err=%v", cursor, found, err)
	}
}

func TestSQLiteFundingRecoverySourceCursorCompleteEmptyWindowAndCrossEpisodeOwnership(t *testing.T) {
	store, generation, episode, scope := newSourceCursorFixture(t, "episode-source-cursor-empty")
	ctx := t.Context()
	start := time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	empty := sourceWindow(scope, start, end, 0, 0)
	if _, _, err := store.ApplyRecoveryOperation(ctx, generation, fundingOperation(episode, scope, "empty-window", 1, empty, "", "")); err != nil {
		t.Fatalf("complete empty window must advance cursor: %v", err)
	}
	cursor, found, err := store.GetRecoverySourceCursor(ctx, empty.Identity())
	if err != nil || !found || cursor.Revision != 1 || cursor.Rows != 0 || cursor.TotalRows != 0 {
		t.Fatalf("empty cursor = %+v found=%t err=%v", cursor, found, err)
	}

	second := episode
	second.ID = "episode-source-cursor-other"
	second.Revision = 1
	if _, err = store.CreateRecoveryEpisode(ctx, generation, second); err != nil {
		t.Fatal("create second episode:", err)
	}
	duplicate := sourceWindow(scope, start, end, 0, 0)
	duplicate.EvidenceHash = fmt.Sprintf("%064x", 999)
	if _, _, err = store.ApplyRecoveryOperation(ctx, generation, fundingOperation(second, scope, "cross-episode-cursor", 1, duplicate, "", "")); !errors.Is(err, accounting.ErrRecoveryConflict) {
		t.Fatalf("cross-episode cursor reassignment = %v; want conflict", err)
	}
}

func TestSQLiteFundingRecoveryExternalEventCannotMoveAcrossEpisodes(t *testing.T) {
	store, generation, firstEpisode, scope := newSourceCursorFixture(t, "episode-external-owner-a")
	secondEpisode := firstEpisode
	secondEpisode.ID = "episode-external-owner-b"
	secondEpisode.Revision = 1
	if _, err := store.CreateRecoveryEpisode(t.Context(), generation, secondEpisode); err != nil {
		t.Fatal("create second episode:", err)
	}
	start := time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	firstWindow := sourceWindow(scope, start, end, 0, 1)
	firstOp := fundingOperation(firstEpisode, scope, "external-owner-a", 1, firstWindow, "same-external-id", "-1")
	if _, _, err := store.ApplyRecoveryOperation(t.Context(), generation, firstOp); err != nil {
		t.Fatalf("apply first event owner: %v", err)
	}
	secondWindow := firstWindow
	secondWindow.Asset = "BTC"
	secondWindow.EvidenceHash = fmt.Sprintf("%064x", 1001)
	secondOp := fundingOperation(secondEpisode, scope, "external-owner-b", 1, secondWindow, "same-external-id", "-1")
	secondOp.Events[0].Asset = secondWindow.Asset
	if _, _, err := store.ApplyRecoveryOperation(t.Context(), generation, secondOp); !errors.Is(err, accounting.ErrRecoveryConflict) {
		t.Fatalf("cross-episode external event reattribution = %v; want conflict", err)
	}
}
