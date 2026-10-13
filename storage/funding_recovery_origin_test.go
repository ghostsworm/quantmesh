package storage

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"quantmesh/accounting"
)

func TestRecoveryOriginIntentCreatesOneBlockedHeldEpisode(t *testing.T) {
	store, err := NewSQLStorage(filepath.Join(t.TempDir(), "recovery-origin.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.MigrateFundingRecovery(t.Context()); err != nil {
		t.Fatal(err)
	}
	scope := fmt.Sprintf("%064x", 1179)
	generation, err := store.ClaimFundingCarryRuntimeGeneration(t.Context(), []string{scope})
	if err != nil {
		t.Fatal(err)
	}
	origin := accounting.RecoveryOriginIntent{
		Bot: "bot-origin", Strategy: "FundingCarry", Exchange: "binance", Market: "futures",
		AccountScope: "account-scope-origin", OwnerScopes: []string{scope}, Symbol: "BTCUSDT", ClientOrderID: "fc-origin-unknown-1",
		Side: "BUY", IntentRevision: 2, IntentHash: strings.Repeat("a", 64),
	}
	key, err := origin.Key()
	if err != nil {
		t.Fatal(err)
	}
	newEpisode := func(id string) accounting.RecoveryEpisode {
		return accounting.RecoveryEpisode{
			ID: id, Bot: origin.Bot, Strategy: origin.Strategy, AccountScopes: []string{scope},
			OwnerGeneration: generation.ID(), Revision: 1, RecoveryGeneration: 1, State: accounting.RecoveryBlocked,
		}
	}
	first, created, err := store.CreateRecoveryEpisodeForIntent(t.Context(), generation, origin, newEpisode("origin-"+key[:40]))
	if err != nil || !created {
		t.Fatalf("create origin episode = (%q, %t, %v), want created", first.ID, created, err)
	}
	changedOwnerScope := fmt.Sprintf("%064x", 1180)
	changedOrigin := origin
	changedOrigin.OwnerScopes = []string{changedOwnerScope}
	changedKey, err := changedOrigin.Key()
	if err != nil || changedKey != key {
		t.Fatalf("owner-scope change changed stable business origin key: key=%q want=%q err=%v", changedKey, key, err)
	}
	changedGeneration, err := store.ClaimFundingCarryRuntimeGeneration(t.Context(), []string{changedOwnerScope})
	if err != nil {
		t.Fatal(err)
	}
	changedEpisode := newEpisode("changed-owner-scope")
	changedEpisode.AccountScopes = []string{changedOwnerScope}
	changedEpisode.OwnerGeneration = changedGeneration.ID()
	if _, _, err := store.CreateRecoveryEpisodeForIntent(t.Context(), changedGeneration, changedOrigin, changedEpisode); !errors.Is(err, accounting.ErrRecoveryConflict) {
		t.Fatalf("owner-scope change created or reused an episode without audited takeover: %v", err)
	}
	hold, found, err := store.GetRecoveryHold(t.Context(), first.ID, first.RecoveryGeneration)
	if err != nil || !found || hold.State != "held" {
		t.Fatalf("initial hold = (%+v, %t, %v), want held", hold, found, err)
	}
	children, err := store.ListRecoveryChildren(t.Context(), first.ID)
	if err != nil || len(children) != 1 || children[0].State != accounting.ChildUnknown || children[0].ClientOrderID != origin.ClientOrderID ||
		children[0].OrderIntentHash != "" || children[0].SourceIntentHash != origin.IntentHash || children[0].Side != origin.Side {
		t.Fatalf("origin child = (%+v, %v), want one unknown child bound to client order ID", children, err)
	}
	second, created, err := store.CreateRecoveryEpisodeForIntent(t.Context(), generation, origin, newEpisode("different-retry-id"))
	if err != nil || created || second.ID != first.ID {
		t.Fatalf("same origin retry = (%q, %t, %v), want original episode", second.ID, created, err)
	}
	changed := origin
	changed.IntentHash = strings.Repeat("b", 64)
	if _, _, err = store.CreateRecoveryEpisodeForIntent(t.Context(), generation, changed, newEpisode("tampered-intent")); !errors.Is(err, accounting.ErrRecoveryConflict) {
		t.Fatalf("changed immutable intent hash error = %v, want conflict", err)
	}
	nextOwner, err := store.ClaimFundingCarryRuntimeGeneration(t.Context(), []string{scope})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = store.CreateRecoveryEpisodeForIntent(t.Context(), generation, origin, newEpisode("old-owner-retry")); !errors.Is(err, ErrFundingCarryRuntimeGenerationLost) {
		t.Fatalf("stale generation create error = %v, want lost-generation rejection", err)
	}
	takenOver, replayed, err := store.TakeOverRecoveryEpisode(t.Context(), nextOwner, accounting.RecoveryOwnerTakeoverAuthorization{
		EpisodeID: first.ID, OperationID: "origin-explicit-takeover", Actor: "auditor", AuditRef: "incident-1179",
		ExpectedOwnerGeneration: generation.ID(), ExpectedEpisodeRevision: first.Revision, Confirmed: true,
	})
	if err != nil || replayed || takenOver.OwnerGeneration != nextOwner.ID() {
		t.Fatalf("explicit same-scope takeover = (%+v, %t, %v)", takenOver, replayed, err)
	}
	newOwnerRetryEpisode := newEpisode("new-owner-retry")
	newOwnerRetryEpisode.OwnerGeneration = nextOwner.ID()
	retained, created, err := store.CreateRecoveryEpisodeForIntent(t.Context(), nextOwner, origin, newOwnerRetryEpisode)
	if err != nil || created || retained.ID != first.ID {
		t.Fatalf("post-takeover origin retry = (%q, %t, %v), want retained episode", retained.ID, created, err)
	}
	if _, err := origin.Key(); err != nil {
		t.Fatal("canonical origin key rejected:", err)
	}
	for name, invalid := range map[string]accounting.RecoveryOriginIntent{
		"exchange case": func() accounting.RecoveryOriginIntent { value := origin; value.Exchange = "Binance"; return value }(),
		"symbol case":   func() accounting.RecoveryOriginIntent { value := origin; value.Symbol = "btcusdt"; return value }(),
		"leading whitespace": func() accounting.RecoveryOriginIntent {
			value := origin
			value.ClientOrderID = " " + value.ClientOrderID
			return value
		}(),
	} {
		if _, err := invalid.Key(); err == nil {
			t.Errorf("%s produced an origin key", name)
		}
	}
	var episodes, holds, origins, childrenCount int
	if err := store.db.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM funding_recovery_episodes`).Scan(&episodes); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM funding_recovery_holds WHERE state='held'`).Scan(&holds); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM funding_recovery_origins`).Scan(&origins); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM funding_recovery_children`).Scan(&childrenCount); err != nil {
		t.Fatal(err)
	}
	if episodes != 1 || holds != 1 || origins != 1 || childrenCount != 1 {
		t.Fatalf("durable rows episodes=%d holds=%d origins=%d children=%d; want 1 each", episodes, holds, origins, childrenCount)
	}
}

func TestRecoveryOriginPayloadComparisonIgnoresJSONKeyOrdering(t *testing.T) {
	expected := accounting.RecoveryOriginIntent{
		Bot: "bot", Strategy: "FundingCarry", Exchange: "binance", Market: "futures", AccountScope: "scope",
		OwnerScopes: []string{"scope"}, Symbol: "BTCUSDT", ClientOrderID: "cid", Side: "BUY",
		IntentRevision: 2, IntentHash: strings.Repeat("a", 64),
	}
	// MySQL JSON storage may return object keys in a different order from the
	// bytes originally inserted; semantic readback must still be idempotent.
	payload := []byte(`{"intent_hash":"` + strings.Repeat("a", 64) + `","side":"BUY","client_order_id":"cid","symbol":"BTCUSDT","owner_scopes":["scope"],"account_scope":"scope","market":"futures","exchange":"binance","strategy":"FundingCarry","bot_id":"bot","intent_revision":2}`)
	matches, err := recoveryOriginPayloadMatches(payload, expected)
	if err != nil || !matches {
		t.Fatalf("reordered origin JSON matches=%t err=%v", matches, err)
	}
	expected.IntentHash = strings.Repeat("b", 64)
	if matches, err = recoveryOriginPayloadMatches(payload, expected); err != nil || matches {
		t.Fatalf("changed immutable origin hash matches=%t err=%v", matches, err)
	}
}

func TestSQLiteRecoveryOriginMigrationForwardAndRollback(t *testing.T) {
	store, err := NewSQLStorage(filepath.Join(t.TempDir(), "recovery-origin-migration.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.MigrateFundingRecovery(t.Context()); err != nil {
		t.Fatal(err)
	}
	applyMigration := func(path string) {
		t.Helper()
		body, err := fundingRecoveryMigrations.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, statement := range strings.Split(string(body), ";") {
			statement = strings.TrimSpace(statement)
			if statement == "" {
				continue
			}
			if _, err := store.db.ExecContext(t.Context(), statement); err != nil {
				t.Fatalf("apply %s: %v", path, err)
			}
		}
	}
	countOrigins := func() int {
		t.Helper()
		var count int
		if err := store.db.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='funding_recovery_origins'`).Scan(&count); err != nil {
			t.Fatal(err)
		}
		return count
	}
	if countOrigins() != 1 {
		t.Fatal("origin binding migration did not create the table")
	}
	if _, err := store.db.ExecContext(t.Context(), `INSERT INTO funding_recovery_origins(origin_key,episode_id,payload_json) VALUES(?,?,?)`, strings.Repeat("e", 64), "episode-retained", `{}`); err != nil {
		t.Fatal("seed retained origin identity:", err)
	}
	applyMigration("migrations/2026101203_funding_recovery_sqlite.down.sql")
	if countOrigins() != 1 {
		t.Fatal("data-preserving down migration unexpectedly removed origin identities")
	}
	var retained int
	if err := store.db.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM funding_recovery_origins WHERE origin_key=?`, strings.Repeat("e", 64)).Scan(&retained); err != nil || retained != 1 {
		t.Fatalf("origin identity after down migration count=%d err=%v", retained, err)
	}
	applyMigration("migrations/2026101203_funding_recovery_sqlite.up.sql")
	if countOrigins() != 1 {
		t.Fatal("origin binding up migration lost the retained table")
	}
	if err := store.db.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM funding_recovery_origins WHERE origin_key=?`, strings.Repeat("e", 64)).Scan(&retained); err != nil || retained != 1 {
		t.Fatalf("origin identity after re-up migration count=%d err=%v", retained, err)
	}
}

func TestRecoveryOriginIntentConcurrentRetryHasSingleWinner(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "recovery-origin-concurrent.db")
	store, err := NewSQLStorage(databasePath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.MigrateFundingRecovery(t.Context()); err != nil {
		t.Fatal(err)
	}
	secondStore, err := NewSQLStorage(databasePath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = secondStore.Close() })
	scope := fmt.Sprintf("%064x", 1180)
	generation, err := store.ClaimFundingCarryRuntimeGeneration(t.Context(), []string{scope})
	if err != nil {
		t.Fatal(err)
	}
	origin := accounting.RecoveryOriginIntent{
		Bot: "bot-origin-race", Strategy: "FundingCarry", Exchange: "binance", Market: "futures",
		AccountScope: scope, OwnerScopes: []string{scope}, Symbol: "BTCUSDT", ClientOrderID: "fc-origin-race-1",
		Side: "SELL", IntentRevision: 1, IntentHash: strings.Repeat("c", 64),
	}
	key, err := origin.Key()
	if err != nil {
		t.Fatal(err)
	}
	episode := accounting.RecoveryEpisode{
		ID: "origin-" + key[:40], Bot: origin.Bot, Strategy: origin.Strategy, AccountScopes: []string{scope},
		OwnerGeneration: generation.ID(), Revision: 1, RecoveryGeneration: 1, State: accounting.RecoveryBlocked,
	}
	const workers = 8
	start := make(chan struct{})
	ids := make([]string, workers)
	errs := make([]error, workers)
	var wg sync.WaitGroup
	for index := range workers {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			<-start
			selectedStore := store
			if index%2 != 0 {
				selectedStore = secondStore
			}
			created, _, callErr := selectedStore.CreateRecoveryEpisodeForIntent(t.Context(), generation, origin, episode)
			ids[index], errs[index] = created.ID, callErr
		}(index)
	}
	close(start)
	wg.Wait()
	for index, callErr := range errs {
		if callErr != nil || ids[index] != episode.ID {
			t.Fatalf("concurrent retry %d returned id=%q err=%v", index, ids[index], callErr)
		}
	}
	var episodes, holds, origins, childrenCount int
	for query, destination := range map[string]*int{
		`SELECT COUNT(*) FROM funding_recovery_episodes`:                 &episodes,
		`SELECT COUNT(*) FROM funding_recovery_holds WHERE state='held'`: &holds,
		`SELECT COUNT(*) FROM funding_recovery_origins`:                  &origins,
		`SELECT COUNT(*) FROM funding_recovery_children`:                 &childrenCount,
	} {
		if err := store.db.QueryRowContext(t.Context(), query).Scan(destination); err != nil {
			t.Fatal(err)
		}
	}
	if episodes != 1 || holds != 1 || origins != 1 || childrenCount != 1 {
		t.Fatalf("concurrent durable rows episodes=%d holds=%d origins=%d children=%d; want one each", episodes, holds, origins, childrenCount)
	}
}

func TestRecoveryOriginIntentHoldFailureRollsBackBindingAndEpisode(t *testing.T) {
	store, err := NewSQLStorage(filepath.Join(t.TempDir(), "recovery-origin-rollback.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.MigrateFundingRecovery(t.Context()); err != nil {
		t.Fatal(err)
	}
	scope := fmt.Sprintf("%064x", 1181)
	generation, err := store.ClaimFundingCarryRuntimeGeneration(t.Context(), []string{scope})
	if err != nil {
		t.Fatal(err)
	}
	origin := accounting.RecoveryOriginIntent{
		Bot: "bot-origin-rollback", Strategy: "FundingCarry", Exchange: "binance", Market: "futures",
		AccountScope: scope, OwnerScopes: []string{scope}, Symbol: "BTCUSDT", ClientOrderID: "fc-origin-rollback-1",
		Side: "BUY", IntentRevision: 1, IntentHash: strings.Repeat("d", 64),
	}
	key, err := origin.Key()
	if err != nil {
		t.Fatal(err)
	}
	episode := accounting.RecoveryEpisode{
		ID: "origin-" + key[:40], Bot: origin.Bot, Strategy: origin.Strategy, AccountScopes: []string{scope},
		OwnerGeneration: generation.ID(), Revision: 1, RecoveryGeneration: 1, State: accounting.RecoveryBlocked,
	}
	if _, err := store.db.ExecContext(t.Context(), `CREATE TRIGGER reject_recovery_hold BEFORE INSERT ON funding_recovery_holds BEGIN SELECT RAISE(ABORT, 'injected hold failure'); END`); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.CreateRecoveryEpisodeForIntent(t.Context(), generation, origin, episode); err == nil {
		t.Fatal("episode creation succeeded despite injected hold failure")
	}
	for table, want := range map[string]int{"funding_recovery_origins": 0, "funding_recovery_episodes": 0, "funding_recovery_holds": 0, "funding_recovery_children": 0} {
		var got int
		if err := store.db.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM `+table).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Fatalf("%s rows=%d after rollback, want %d", table, got, want)
		}
	}
}
