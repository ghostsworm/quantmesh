package main

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"quantmesh/config"
	"quantmesh/exchange"
	"quantmesh/exchange/accounting"
	"quantmesh/risk"
	"quantmesh/storage"
)

func TestRetiredEquityAccountsArchiveEncryptsCredentialsAndSurvivesReload(t *testing.T) {
	db, err := storage.NewSQLStorage(filepath.Join(t.TempDir(), "retired-equity.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if err := db.MigrateRiskCheckpoints(t.Context()); err != nil {
		t.Fatal(err)
	}

	oldCredentials := config.ExchangeConfig{APIKey: "retired-account-api-key", SecretKey: "retired-account-secret", Testnet: true}
	previous := buildEquityScopeSnapshot(&config.Config{Exchanges: map[string]config.ExchangeConfig{"binance": oldCredentials},
		Bots: []config.BotConfig{{Exchange: "binance", Symbol: "BTCUSDT", MarketType: "futures"}}})
	next := buildEquityScopeSnapshot(&config.Config{Exchanges: map[string]config.ExchangeConfig{}, Bots: []config.BotConfig{}})
	key := []byte("0123456789abcdef0123456789abcdef")
	store := &retiredEquityAccountsStore{backend: db, key: key}
	if err := store.archiveRemoved(t.Context(), previous, next, time.Date(2026, 10, 9, 1, 2, 3, 0, time.UTC)); err != nil {
		t.Fatalf("archive removed account: %v", err)
	}

	payload, revision, err := db.LoadRiskCheckpoint(t.Context(), retiredEquityAccountsCheckpointKey)
	if err != nil {
		t.Fatal(err)
	}
	if revision != 1 || strings.Contains(string(payload), oldCredentials.APIKey) || strings.Contains(string(payload), oldCredentials.SecretKey) {
		t.Fatalf("archive must be revisioned and contain no plaintext credentials: revision=%d", revision)
	}

	reloaded := &retiredEquityAccountsStore{backend: db, key: key}
	accounts, loadedRevision, err := reloaded.load(t.Context())
	if err != nil {
		t.Fatalf("reload encrypted archive: %v", err)
	}
	if loadedRevision != 1 || len(accounts) != 1 || accounts[0].Status != retiredEquityAccountPendingVerification ||
		accounts[0].credentials.APIKey != oldCredentials.APIKey || accounts[0].credentials.SecretKey != oldCredentials.SecretKey ||
		accounts[0].MarketType != "futures" || accounts[0].Scope != equityAccountScopeID("binance", oldCredentials) {
		t.Fatalf("reloaded retired-account record mismatch: revision=%d count=%d record=%+v", loadedRevision, len(accounts), accounts)
	}
	if err := reloaded.archiveRemoved(t.Context(), previous, next, time.Now()); err != nil {
		t.Fatalf("idempotent archive retry: %v", err)
	}
	_, revision, err = db.LoadRiskCheckpoint(t.Context(), retiredEquityAccountsCheckpointKey)
	if err != nil || revision != 1 {
		t.Fatalf("idempotent archive retry changed state: revision=%d err=%v", revision, err)
	}
}

func TestRetiredEquityAccountsRejectRotationWithoutMasterKey(t *testing.T) {
	db, err := storage.NewSQLStorage(filepath.Join(t.TempDir(), "retired-equity-no-key.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if err := db.MigrateRiskCheckpoints(t.Context()); err != nil {
		t.Fatal(err)
	}
	credentials := config.ExchangeConfig{APIKey: "account-key", SecretKey: "account-secret"}
	previous := buildEquityScopeSnapshot(&config.Config{Exchanges: map[string]config.ExchangeConfig{"binance": credentials},
		Bots: []config.BotConfig{{Exchange: "binance", Symbol: "BTCUSDT", MarketType: "futures"}}})
	next := buildEquityScopeSnapshot(&config.Config{Exchanges: map[string]config.ExchangeConfig{}, Bots: []config.BotConfig{}})
	store := &retiredEquityAccountsStore{backend: db}
	if err := store.archiveRemoved(context.Background(), previous, next, time.Now()); err == nil {
		t.Fatal("account rotation proceeded without an available master key")
	}
	if payload, revision, err := db.LoadRiskCheckpoint(context.Background(), retiredEquityAccountsCheckpointKey); err != nil || payload != nil || revision != 0 {
		t.Fatalf("failed archive wrote partial state: payload=%q revision=%d err=%v", payload, revision, err)
	}
}

func TestRemovedEquityAccountsArchiveOnlyCredentialVersionsNoLongerConfigured(t *testing.T) {
	oldCredentials := config.ExchangeConfig{APIKey: "same-account-key", SecretKey: "old-secret"}
	newCredentials := oldCredentials
	newCredentials.SecretKey = "new-secret"
	previous := buildEquityScopeSnapshot(&config.Config{Exchanges: map[string]config.ExchangeConfig{"binance": oldCredentials},
		Bots: []config.BotConfig{{Exchange: "binance", Symbol: "BTCUSDT", MarketType: "futures"}}})
	unchanged := buildEquityScopeSnapshot(&config.Config{Exchanges: map[string]config.ExchangeConfig{"binance": oldCredentials},
		Bots: []config.BotConfig{{Exchange: "binance", Symbol: "ETHUSDT", MarketType: "futures"}}})
	rotated := buildEquityScopeSnapshot(&config.Config{Exchanges: map[string]config.ExchangeConfig{"binance": newCredentials},
		Bots: []config.BotConfig{{Exchange: "binance", Symbol: "ETHUSDT", MarketType: "futures"}}})
	if removed := removedEquityAccounts(previous, unchanged); len(removed) != 0 {
		t.Fatalf("symbol-only config edit retired account credentials: %d", len(removed))
	}
	removed := removedEquityAccounts(previous, rotated)
	if len(removed) != 1 || removed[0].SecretKey != oldCredentials.SecretKey {
		t.Fatalf("credential rotation did not preserve the previous credential version: %+v", removed)
	}
}

func TestRetiredEquityAccountRequiresTwoSeparatedCompleteFlatSnapshots(t *testing.T) {
	db, err := storage.NewSQLStorage(filepath.Join(t.TempDir(), "retired-equity-evidence.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if err := db.MigrateRiskCheckpoints(t.Context()); err != nil {
		t.Fatal(err)
	}
	credentials := config.ExchangeConfig{APIKey: "account-key", SecretKey: "account-secret"}
	previous := buildEquityScopeSnapshot(&config.Config{Exchanges: map[string]config.ExchangeConfig{"binance": credentials},
		Bots: []config.BotConfig{{Exchange: "binance", Symbol: "BTCUSDT", MarketType: "futures"}}})
	next := buildEquityScopeSnapshot(&config.Config{Exchanges: map[string]config.ExchangeConfig{}, Bots: []config.BotConfig{}})
	store := &retiredEquityAccountsStore{backend: db, key: []byte("0123456789abcdef0123456789abcdef")}
	retiredAt := time.Date(2026, 10, 9, 1, 0, 0, 0, time.UTC)
	if err := store.archiveRemoved(t.Context(), previous, next, retiredAt); err != nil {
		t.Fatal(err)
	}
	accounts, _, err := store.load(t.Context())
	if err != nil || len(accounts) != 1 {
		t.Fatalf("load retired account: count=%d err=%v", len(accounts), err)
	}
	id := accounts[0].ID
	first := retiredAt.Add(time.Minute)
	if err := store.recordEvidence(t.Context(), id, true, true, first); err != nil {
		t.Fatal(err)
	}
	assertRetiredEvidenceState(t, store, id, retiredEquityAccountPendingVerification, 1)
	if err := store.recordEvidence(t.Context(), id, true, true, first.Add(retiredEquityFlatEvidenceInterval-time.Second)); err != nil {
		t.Fatal(err)
	}
	assertRetiredEvidenceState(t, store, id, retiredEquityAccountPendingVerification, 1)
	if err := store.recordEvidence(t.Context(), id, false, true, first.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	assertRetiredEvidenceState(t, store, id, retiredEquityAccountPendingVerification, 0)
	third := first.Add(3 * time.Minute)
	if err := store.recordEvidence(t.Context(), id, true, true, third); err != nil {
		t.Fatal(err)
	}
	if err := store.recordEvidence(t.Context(), id, true, true, third.Add(retiredEquityFlatEvidenceInterval)); err != nil {
		t.Fatal(err)
	}
	assertRetiredEvidenceState(t, store, id, retiredEquityAccountReadyForReset, retiredEquityFlatEvidenceRequired)
	for i := 2; i <= 5; i++ {
		at := third.Add(time.Duration(i) * retiredEquityFlatEvidenceInterval)
		if err := store.recordEvidence(t.Context(), id, true, true, at); err != nil {
			t.Fatalf("continued flat verification %d: %v", i, err)
		}
		assertRetiredEvidenceState(t, store, id, retiredEquityAccountReadyForReset, retiredEquityFlatEvidenceRequired)
	}
	if err := store.recordEvidence(t.Context(), id, true, false, third.Add(6*time.Minute)); err != nil {
		t.Fatal(err)
	}
	assertRetiredEvidenceState(t, store, id, retiredEquityAccountPendingVerification, 0)
}

func assertRetiredEvidenceState(t *testing.T, store *retiredEquityAccountsStore, id, status string, count int) {
	t.Helper()
	accounts, _, err := store.load(t.Context())
	if err != nil {
		t.Fatalf("load retired evidence: %v", err)
	}
	if len(accounts) != 1 {
		t.Fatalf("retired evidence account count = %d, want 1", len(accounts))
	}
	if accounts[0].Status != status || accounts[0].FlatEvidenceCount != count {
		t.Fatalf("retired evidence state = (%q, %d), want (%q, %d)", accounts[0].Status, accounts[0].FlatEvidenceCount, status, count)
	}
}

func TestRetiredEquityAccountRejectsMissingOrRegressedEvidence(t *testing.T) {
	store := &retiredEquityAccountsStore{}
	if err := store.recordEvidence(t.Context(), "missing", true, true, time.Now()); err == nil {
		t.Fatal("evidence without durable archive was accepted")
	}
	if err := store.archiveRemoved(t.Context(), equityScopeSnapshot{}, equityScopeSnapshot{}, time.Now()); err == nil {
		t.Fatal("nil archive backend was accepted")
	}
}

func TestPrepareEquityScopeConfigArchivesBeforePublishingNewScope(t *testing.T) {
	db, err := storage.NewSQLStorage(filepath.Join(t.TempDir(), "retired-equity-preflight.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if err := db.MigrateRiskCheckpoints(t.Context()); err != nil {
		t.Fatal(err)
	}
	credentials := config.ExchangeConfig{APIKey: "account-key", SecretKey: "account-secret"}
	oldConfig := &config.Config{Exchanges: map[string]config.ExchangeConfig{"binance": credentials},
		Bots: []config.BotConfig{{Exchange: "binance", Symbol: "BTCUSDT", MarketType: "futures"}}}
	newConfig := &config.Config{Exchanges: map[string]config.ExchangeConfig{}, Bots: []config.BotConfig{}}
	manager := NewBotManager(oldConfig, nil, nil, nil, "")
	manager.retiredEquityAccounts = &retiredEquityAccountsStore{backend: db, key: []byte("0123456789abcdef0123456789abcdef")}
	pauseCoordinator := risk.NewOpeningPauseCoordinator()
	manager.SetOpeningPauseCoordinator(pauseCoordinator)
	oldScope := manager.equityScopeSnapshot().scope
	if err := manager.PrepareEquityScopeConfig(t.Context(), newConfig); err != nil {
		t.Fatalf("preflight config change: %v", err)
	}
	if !pauseCoordinator.IsHeldBy(retiredEquityAccountPauseSource) {
		t.Fatal("retiring an account did not install the dedicated opening hold")
	}
	archived, _, err := manager.retiredEquityAccounts.load(t.Context())
	if err != nil || len(archived) != 1 {
		t.Fatalf("old account was not durably archived before configuration publication: count=%d err=%v", len(archived), err)
	}
	if manager.equityScopeSnapshot().scope != oldScope {
		t.Fatal("preflight published the new equity scope before config persistence")
	}
	manager.registerEquityScopeConfig(newConfig)
	if manager.equityScopeSnapshot().scope == oldScope {
		t.Fatal("successful config publication did not install the new equity scope")
	}
}

func TestRetiredEquityAccountHoldRestoresBeforeRuntimeStartup(t *testing.T) {
	db, err := storage.NewSQLStorage(filepath.Join(t.TempDir(), "retired-equity-restore.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if err := db.MigrateRiskCheckpoints(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := db.MigrateOpeningPauseHolders(t.Context()); err != nil {
		t.Fatal(err)
	}
	credentials := config.ExchangeConfig{APIKey: "account-key", SecretKey: "account-secret"}
	previous := buildEquityScopeSnapshot(&config.Config{Exchanges: map[string]config.ExchangeConfig{"binance": credentials},
		Bots: []config.BotConfig{{Exchange: "binance", Symbol: "BTCUSDT", MarketType: "futures"}}})
	next := buildEquityScopeSnapshot(&config.Config{Exchanges: map[string]config.ExchangeConfig{}, Bots: []config.BotConfig{}})
	store := &retiredEquityAccountsStore{backend: db, key: []byte("0123456789abcdef0123456789abcdef")}
	if err := store.archiveRemoved(t.Context(), previous, next, time.Now().Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	coordinator, err := risk.NewOpeningPauseCoordinatorWithStore(t.Context(), db)
	if err != nil {
		t.Fatal(err)
	}
	manager := NewBotManager(&config.Config{}, nil, nil, nil, "")
	manager.retiredEquityAccounts = store
	manager.SetOpeningPauseCoordinator(coordinator)
	if err := manager.RestoreRetiredEquityAccountHold(t.Context()); err != nil {
		t.Fatal(err)
	}
	if !coordinator.IsHeldBy(retiredEquityAccountPauseSource) {
		t.Fatal("startup did not restore the retired-account opening hold")
	}
	rows, err := db.LoadOpeningPauseHolders(t.Context())
	retiredPauseFound := false
	for _, row := range rows {
		retiredPauseFound = retiredPauseFound || row.Source == retiredEquityAccountPauseSource
	}
	if err != nil || !retiredPauseFound {
		t.Fatalf("retired-account hold was not made durable before startup: rows=%+v err=%v", rows, err)
	}
}

type retiredAccountObserverFixture struct {
	complete   bool
	flat       bool
	observed   time.Time
	observeErr error
}

func (f retiredAccountObserverFixture) ObserveAccountFuturesFlatness(context.Context) (bool, bool, time.Time, error) {
	return f.complete, f.flat, f.observed, f.observeErr
}

func TestRetiredEquityVerifierLoopRecordsOnlyCompleteFlatEvidence(t *testing.T) {
	db, err := storage.NewSQLStorage(filepath.Join(t.TempDir(), "retired-equity-observer.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if err := db.MigrateRiskCheckpoints(t.Context()); err != nil {
		t.Fatal(err)
	}
	credentials := config.ExchangeConfig{APIKey: "account-key", SecretKey: "account-secret"}
	previous := buildEquityScopeSnapshot(&config.Config{Exchanges: map[string]config.ExchangeConfig{"binance": credentials},
		Bots: []config.BotConfig{{Exchange: "binance", Symbol: "BTCUSDT", MarketType: "futures"}}})
	next := buildEquityScopeSnapshot(&config.Config{Exchanges: map[string]config.ExchangeConfig{}, Bots: []config.BotConfig{}})
	store := &retiredEquityAccountsStore{backend: db, key: []byte("0123456789abcdef0123456789abcdef")}
	initialObservation := time.Now().UTC().Add(-4 * time.Minute)
	retiredAt := initialObservation.Add(-time.Minute)
	if err := store.archiveRemoved(t.Context(), previous, next, retiredAt); err != nil {
		t.Fatal(err)
	}
	manager := NewBotManager(&config.Config{}, nil, nil, nil, "")
	manager.retiredEquityAccounts = store
	seenCredentials := false
	newObserver := func(exchangeName string, cfg config.ExchangeConfig) (exchange.AccountFuturesFlatnessObserver, error) {
		seenCredentials = exchangeName == "binance" && cfg.APIKey == credentials.APIKey && cfg.SecretKey == credentials.SecretKey
		return retiredAccountObserverFixture{complete: true, flat: false, observed: initialObservation}, nil
	}
	if err := manager.verifyRetiredEquityAccountsOnce(t.Context(), newObserver); err != nil {
		t.Fatalf("complete non-flat evidence should be persisted without treating it as verifier failure: %v", err)
	}
	if !seenCredentials {
		t.Fatal("read-only verifier did not receive the decrypted retired credential version")
	}
	accounts, _, err := store.load(t.Context())
	if err != nil || len(accounts) != 1 || accounts[0].LastEvidenceResult != "open_exposure" || accounts[0].FlatEvidenceCount != 0 {
		t.Fatalf("non-flat observation advanced reset readiness: records=%+v err=%v", accounts, err)
	}
	first := initialObservation.Add(time.Minute)
	newObserver = func(string, config.ExchangeConfig) (exchange.AccountFuturesFlatnessObserver, error) {
		return retiredAccountObserverFixture{complete: true, flat: true, observed: first}, nil
	}
	if err := manager.verifyRetiredEquityAccountsOnce(t.Context(), newObserver); err != nil {
		t.Fatal(err)
	}
	first = first.Add(retiredEquityFlatEvidenceInterval)
	if err := manager.verifyRetiredEquityAccountsOnce(t.Context(), newObserver); err != nil {
		t.Fatal(err)
	}
	accounts, _, err = store.load(t.Context())
	if err != nil || len(accounts) != 1 || accounts[0].Status != retiredEquityAccountReadyForReset {
		t.Fatalf("two separated complete flat observations did not persist readiness: records=%+v err=%v", accounts, err)
	}
}

func TestRetiredEquityVerifierPersistsActionableSafeResultCodes(t *testing.T) {
	sensitiveDetail := "private-exchange-response-must-not-be-stored"
	cases := []struct {
		name       string
		exchange   string
		market     string
		factoryErr error
		observer   exchange.AccountFuturesFlatnessObserver
		wantResult string
		wantErr    bool
	}{
		{name: "unsupported market", exchange: "bitget", market: "spot", wantResult: retiredEquityEvidenceUnsupportedMarket, wantErr: true},
		{name: "factory unavailable", exchange: "binance", market: "futures", factoryErr: errors.New(sensitiveDetail), wantResult: retiredEquityEvidenceObserverUnavailable, wantErr: true},
		{name: "read-only query failed", exchange: "binance", market: "futures", observer: retiredAccountObserverFixture{observeErr: errors.New(sensitiveDetail)}, wantResult: retiredEquityEvidenceQueryFailed, wantErr: true},
		{name: "evidence incomplete", exchange: "binance", market: "futures", observer: retiredAccountObserverFixture{complete: false}, wantResult: retiredEquityEvidenceIncomplete},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db, err := storage.NewSQLStorage(filepath.Join(t.TempDir(), "retired-equity-result.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = db.Close() }()
			if err := db.MigrateRiskCheckpoints(t.Context()); err != nil {
				t.Fatal(err)
			}
			credentials := config.ExchangeConfig{APIKey: "account-key", SecretKey: "account-secret", Passphrase: "passphrase"}
			previous := buildEquityScopeSnapshot(&config.Config{Exchanges: map[string]config.ExchangeConfig{tc.exchange: credentials},
				Bots: []config.BotConfig{{Exchange: tc.exchange, Symbol: "BTCUSDT", MarketType: tc.market}}})
			next := buildEquityScopeSnapshot(&config.Config{Exchanges: map[string]config.ExchangeConfig{}, Bots: []config.BotConfig{}})
			store := &retiredEquityAccountsStore{backend: db, key: []byte("0123456789abcdef0123456789abcdef")}
			if err := store.archiveRemoved(t.Context(), previous, next, time.Now().Add(-time.Minute)); err != nil {
				t.Fatal(err)
			}
			manager := NewBotManager(&config.Config{}, nil, nil, nil, "")
			manager.retiredEquityAccounts = store
			factory := func(string, config.ExchangeConfig) (exchange.AccountFuturesFlatnessObserver, error) {
				return tc.observer, tc.factoryErr
			}
			if err := manager.verifyRetiredEquityAccountsOnce(t.Context(), factory); (err != nil) != tc.wantErr {
				t.Fatalf("verification error=%v, wantErr=%v", err, tc.wantErr)
			}
			accounts, _, err := store.load(t.Context())
			if err != nil || len(accounts) != 1 || accounts[0].Status != retiredEquityAccountPendingVerification ||
				accounts[0].FlatEvidenceCount != 0 || accounts[0].LastEvidenceResult != tc.wantResult {
				t.Fatalf("unexpected persisted verification state: records=%+v err=%v", accounts, err)
			}
			payload, _, err := db.LoadRiskCheckpoint(t.Context(), retiredEquityAccountsCheckpointKey)
			if err != nil || strings.Contains(string(payload), sensitiveDetail) {
				t.Fatalf("raw exchange failure detail must not be persisted: err=%v", err)
			}
		})
	}
}

func TestRetiredEquityResetIntentIsIdempotentAndPersistsAuditWithoutCredentials(t *testing.T) {
	db, err := storage.NewSQLStorage(filepath.Join(t.TempDir(), "retired-equity-reset.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if err := db.MigrateRiskCheckpoints(t.Context()); err != nil {
		t.Fatal(err)
	}
	credentials := config.ExchangeConfig{APIKey: "reset-account-key", SecretKey: "reset-account-secret"}
	previous := buildEquityScopeSnapshot(&config.Config{Exchanges: map[string]config.ExchangeConfig{"binance": credentials},
		Bots: []config.BotConfig{{Exchange: "binance", Symbol: "BTCUSDT", MarketType: "futures"}}})
	next := buildEquityScopeSnapshot(&config.Config{Exchanges: map[string]config.ExchangeConfig{}, Bots: []config.BotConfig{}})
	store := &retiredEquityAccountsStore{backend: db, key: []byte("0123456789abcdef0123456789abcdef")}
	retiredAt := time.Now().UTC().Add(-5 * time.Minute)
	if err := store.archiveRemoved(t.Context(), previous, next, retiredAt); err != nil {
		t.Fatal(err)
	}
	accounts, _, err := store.load(t.Context())
	if err != nil || len(accounts) != 1 {
		t.Fatalf("load archived account: count=%d err=%v", len(accounts), err)
	}
	if _, _, err := store.beginReset(t.Context(), "admin-user", "new-scope", time.Now()); err == nil {
		t.Fatal("reset intent accepted before the retired account met its flat-evidence threshold")
	}
	firstFlat := retiredAt.Add(time.Minute)
	if err := store.recordEvidence(t.Context(), accounts[0].ID, true, true, firstFlat); err != nil {
		t.Fatal(err)
	}
	if err := store.recordEvidence(t.Context(), accounts[0].ID, true, true, firstFlat.Add(retiredEquityFlatEvidenceInterval)); err != nil {
		t.Fatal(err)
	}
	startedAt := time.Now().UTC()
	operation, completed, err := store.beginReset(t.Context(), "admin-user", "new-scope", startedAt)
	if err != nil || completed || operation.ID == "" {
		t.Fatalf("begin reset: operation=%+v completed=%v err=%v", operation, completed, err)
	}
	retried, alreadyCompleted, err := store.beginReset(t.Context(), "admin-user", "new-scope", startedAt.Add(time.Second))
	if err != nil || alreadyCompleted || retried.ID != operation.ID {
		t.Fatalf("pending operation retry changed identity: original=%+v retry=%+v completed=%v err=%v", operation, retried, alreadyCompleted, err)
	}
	if _, _, err := store.beginReset(t.Context(), "different-admin", "new-scope", startedAt.Add(2*time.Second)); err == nil {
		t.Fatal("a different operator took over the pending reset")
	}
	completedAt := time.Now().UTC()
	audit, err := store.completeReset(t.Context(), operation.ID, 7, completedAt)
	if err != nil || audit.BaselineRevision != 7 || audit.CompletedAt.IsZero() {
		t.Fatalf("complete reset audit: audit=%+v err=%v", audit, err)
	}
	remaining, _, err := store.load(t.Context())
	if err != nil || len(remaining) != 0 {
		t.Fatalf("reset did not clear retired credential records: remaining=%d err=%v", len(remaining), err)
	}
	payload, _, err := db.LoadRiskCheckpoint(t.Context(), retiredEquityAccountsCheckpointKey)
	if err != nil || strings.Contains(string(payload), credentials.APIKey) || strings.Contains(string(payload), credentials.SecretKey) {
		t.Fatalf("completed reset audit retained plaintext credentials: err=%v", err)
	}
	historyStore := &retiredEquityAccountsStore{backend: db, key: store.key}
	manager := NewBotManager(&config.Config{}, nil, nil, nil, "")
	manager.retiredEquityAccounts = historyStore
	history, err := manager.RetiredEquityAccountResetHistory(t.Context())
	if err != nil || len(history) != 1 || history[0].ID != operation.ID || history[0].Actor != "admin-user" || history[0].BaselineRevision != 7 {
		t.Fatalf("completed reset audit history mismatch: history=%+v err=%v", history, err)
	}
	retriedCompletion, alreadyCompleted, err := store.beginReset(t.Context(), "admin-user", "new-scope", time.Now())
	if err != nil || !alreadyCompleted || retriedCompletion.ID != operation.ID {
		t.Fatalf("completed reset retry was not recognized: operation=%+v completed=%v err=%v", retriedCompletion, alreadyCompleted, err)
	}
}

type retiredResetEquitySource struct{ observation risk.EquityObservation }

func (s retiredResetEquitySource) ObserveAccountEquity(context.Context, map[string]time.Time) (risk.EquityObservation, error) {
	observation := s.observation
	observedAt := time.Now().UTC()
	observation.ObservedAt = observedAt
	for id, wallet := range observation.Wallets {
		wallet.ObservedAt = observedAt
		wallet.Through = observedAt.Add(-time.Millisecond)
		observation.Wallets[id] = wallet
	}
	return observation, nil
}

func (s retiredResetEquitySource) TotalEquity(context.Context) (float64, error) {
	return s.observation.Equity, nil
}

func TestResetRetiredEquityAccountsRequiresFreshFlatEvidenceAndReleasesOnlyAfterAudit(t *testing.T) {
	db, err := storage.NewSQLStorage(filepath.Join(t.TempDir(), "retired-equity-reset-workflow.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if err := db.MigrateRiskCheckpoints(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := db.MigrateOpeningPauseHolders(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := db.DeleteOpeningPauseHolder(t.Context(), storage.OpeningPauseHolder{Source: "opening_pause_legacy_state_unverified"}); err != nil {
		t.Fatal(err)
	}
	oldCredentials := config.ExchangeConfig{APIKey: "old-account-key", SecretKey: "old-account-secret"}
	newCredentials := config.ExchangeConfig{APIKey: "new-account-key", SecretKey: "new-account-secret"}
	oldConfig := &config.Config{Exchanges: map[string]config.ExchangeConfig{"binance": oldCredentials},
		Bots: []config.BotConfig{{Exchange: "binance", Symbol: "BTCUSDT", MarketType: "futures"}}}
	newConfig := &config.Config{Exchanges: map[string]config.ExchangeConfig{"binance": newCredentials},
		Bots: []config.BotConfig{{Exchange: "binance", Symbol: "BTCUSDT", MarketType: "futures"}}}
	oldScope, newScope := buildEquityScopeSnapshot(oldConfig), buildEquityScopeSnapshot(newConfig)
	archive := &retiredEquityAccountsStore{backend: db, key: []byte("0123456789abcdef0123456789abcdef")}
	now := time.Now().UTC()
	retiredAt := now.Add(-5 * time.Minute)
	if err := archive.archiveRemoved(t.Context(), oldScope, newScope, retiredAt); err != nil {
		t.Fatal(err)
	}
	accounts, _, err := archive.load(t.Context())
	if err != nil || len(accounts) != 1 {
		t.Fatalf("load retired account: count=%d err=%v", len(accounts), err)
	}
	firstFlat := now.Add(-3 * time.Minute)
	if err := archive.recordEvidence(t.Context(), accounts[0].ID, true, true, firstFlat); err != nil {
		t.Fatal(err)
	}
	if err := archive.recordEvidence(t.Context(), accounts[0].ID, true, true, firstFlat.Add(retiredEquityFlatEvidenceInterval)); err != nil {
		t.Fatal(err)
	}
	equityStore := &persistedEquityState{backend: db}
	oldCheckpoint := risk.EquityCheckpoint{Version: 1, Revision: 1, Scope: oldScope.scope, Currency: "USDT", CashFlowAdjusted: true,
		BaseAt: now.Add(-time.Hour), LastAt: now.Add(-time.Minute), LastEquity: 1000, AdjustedEquity: 1000, HighWater: 1000,
		Receipts: map[string]risk.EquityCashFlow{}}
	if err := equityStore.SaveEquityState(t.Context(), 0, oldCheckpoint); err != nil {
		t.Fatal(err)
	}
	coordinator, err := risk.NewOpeningPauseCoordinatorWithStore(t.Context(), db, "retired-reset-test")
	if err != nil {
		t.Fatal(err)
	}
	manager := NewBotManager(newConfig, nil, nil, nil, "")
	manager.retiredEquityAccounts = archive
	manager.SetOpeningPauseCoordinator(coordinator)
	observationAt := time.Now().UTC()
	equitySource := retiredResetEquitySource{observation: risk.EquityObservation{Scope: newScope.scope, Currency: "USDT", Equity: 1200,
		ObservedAt: observationAt, CashFlowComplete: true, Wallets: map[string]accounting.Wallet{"new-account": {
			Balance: "1200", Currency: "USDT", ObservedAt: observationAt, From: observationAt.Add(-time.Hour), Through: observationAt.Add(-time.Millisecond),
		}}}}
	feeder := risk.NewMetricsFeeder(passiveWithdrawalMetricsSink{}, nil, equitySource, nil, risk.MetricsFeederOptions{
		MaxEquityAge: time.Hour, EquityStore: equityStore, RequirePersistence: true, RequireCashFlowReconciliation: true,
	})
	manager.SetEquityBaselineResetter(feeder)
	operation, err := manager.ResetRetiredEquityAccounts(t.Context(), "admin-1", func(_ string, credentials config.ExchangeConfig) (exchange.AccountFuturesFlatnessObserver, error) {
		if credentials.APIKey != oldCredentials.APIKey || credentials.SecretKey != oldCredentials.SecretKey {
			t.Fatal("reset verifier did not use archived old-account credentials")
		}
		return retiredAccountObserverFixture{complete: true, flat: true, observed: time.Now().UTC()}, nil
	})
	if err != nil {
		t.Fatalf("explicit retired-account reset: %v", err)
	}
	if operation.Actor != "admin-1" || operation.TargetScope != newScope.scope || operation.BaselineRevision != 2 || operation.CompletedAt.IsZero() {
		t.Fatalf("reset audit operation is incomplete: %+v", operation)
	}
	remaining, _, err := archive.load(t.Context())
	if err != nil || len(remaining) != 0 {
		t.Fatalf("retired credential archive was not cleared after audit: remaining=%d err=%v", len(remaining), err)
	}
	checkpoint, err := equityStore.LoadEquityState(t.Context())
	if err != nil || checkpoint.Scope != newScope.scope || checkpoint.ResetOperationID != operation.ID || checkpoint.Revision != 2 {
		t.Fatalf("new-scope equity baseline missing: checkpoint=%+v err=%v", checkpoint, err)
	}
	if coordinator.IsHeldBy(retiredEquityAccountPauseSource) {
		t.Fatal("retired-account opening hold remained after durable reset completion")
	}
	rows, err := db.LoadOpeningPauseHolders(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if row.Source == retiredEquityAccountPauseSource {
			t.Fatal("retired-account opening hold remained durable after reset completion")
		}
	}
}
