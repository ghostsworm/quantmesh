package storage

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"quantmesh/config"
)

func TestAppConfigDocumentSnapshotsAndBotSync(t *testing.T) {
	ctx := context.Background()
	store, err := NewSQLStorage(filepath.Join(t.TempDir(), "app_config.db"))
	if err != nil {
		t.Fatalf("new sql storage: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	if sha256Hex("abc") == "" || !isAppConfigTableMissing(assertStorageErr("no such table: app_config")) ||
		!isAppConfigTableMissing(assertStorageErr("Error 1146: Table 'db.app_config' doesn't exist")) ||
		isAppConfigTableMissing(nil) {
		t.Fatalf("app config helper mismatch")
	}
	if !isBotConfigsTableMissing(assertStorageErr("no such table: bot_configs")) || isBotConfigsTableMissing(assertStorageErr("no such table: other")) {
		t.Fatalf("bot config missing helper mismatch")
	}
	if nullStr("") != nil || nullStr("operator").(string) != "operator" {
		t.Fatalf("nullStr mismatch")
	}
	if err := (&SQLStorage{}).EnsureAppConfigDocumentTables(); err == nil {
		t.Fatalf("nil db ensure should fail")
	}

	if doc, err := store.GetAppConfigDocument(ctx); err != nil || doc != nil {
		t.Fatalf("empty app doc=%#v err=%v", doc, err)
	}
	if _, err := SaveAppConfigSnapshotFromJSON(ctx, nil, []byte(`{}`), "op", "src"); err == nil {
		t.Fatalf("nil storage save json should fail")
	}
	if _, err := SaveAppConfigSnapshotFromJSON(ctx, store, []byte(` `), "op", "src"); err == nil {
		t.Fatalf("empty json should fail")
	}

	rev, err := SaveAppConfigSnapshotFromJSON(ctx, store, []byte(`{"app":{"name":"qm"}}`), "op", "src")
	if err != nil || rev != 1 {
		t.Fatalf("save json rev=%d err=%v", rev, err)
	}
	doc, err := store.GetAppConfigDocument(ctx)
	if err != nil || doc == nil || doc.Revision != 1 || !strings.Contains(doc.Content, "qm") || doc.ContentHash == "" {
		t.Fatalf("app doc=%#v err=%v", doc, err)
	}
	rev, err = SaveAppConfigSnapshotFromJSON(ctx, store, []byte(`{"app":{"name":"qm2"}}`), "", "")
	if err != nil || rev != 2 {
		t.Fatalf("second save json rev=%d err=%v", rev, err)
	}

	cfg := config.CreateMinimalConfig()
	cfg.App.CurrentExchange = "binance"
	cfg.Bots = []config.BotConfig{{
		ID:            "bot-1",
		Exchange:      "binance",
		Symbol:        "BTCUSDT",
		MarketType:    "futures",
		CreatedAt:     "2026-06-01T00:00:00Z",
		OrderQuantity: 100,
	}}
	rev, err = SaveAppConfigSnapshotWithBotSource(ctx, store, cfg, "tester", "app_src", "bot_src")
	if err != nil || rev != 3 {
		t.Fatalf("save config rev=%d err=%v", rev, err)
	}
	botDoc, err := store.GetBotConfigDocument(ctx, "bot-1")
	if err != nil || botDoc == nil || botDoc.Revision != 1 || botDoc.ContentHash == "" {
		t.Fatalf("bot doc=%#v err=%v", botDoc, err)
	}
	if empty, err := store.GetBotConfigDocument(ctx, ""); err != nil || empty != nil {
		t.Fatalf("empty bot id doc=%#v err=%v", empty, err)
	}
	list, err := store.ListBotConfigDocuments(ctx)
	if err != nil || len(list) != 1 {
		t.Fatalf("bot list=%#v err=%v", list, err)
	}

	bf := config.ConvertFromBotConfig(cfg.Bots[0])
	bf.BotID = "bot-1"
	bf.Symbol = "ETHUSDT"
	rev, err = SaveBotConfigSnapshot(ctx, store, bf, "tester", "manual")
	if err != nil || rev != 2 {
		t.Fatalf("save bot rev=%d err=%v", rev, err)
	}
	if _, err := SaveBotConfigSnapshot(ctx, nil, bf, "", ""); err == nil {
		t.Fatalf("nil storage bot save should fail")
	}
	if _, err := SaveBotConfigSnapshot(ctx, store, nil, "", ""); err == nil {
		t.Fatalf("nil bot config should fail")
	}
	bf.BotID = ""
	if _, err := SaveBotConfigSnapshot(ctx, store, bf, "", ""); err == nil {
		t.Fatalf("empty bot id should fail")
	}

	if err := SyncBotConfigSnapshotsFromMainConfig(ctx, store, cfg, "tester", "sync"); err != nil {
		t.Fatalf("sync bot snapshots: %v", err)
	}
	list, err = store.ListBotConfigDocuments(ctx)
	if err != nil || len(list) < 1 {
		t.Fatalf("synced bot list=%#v err=%v", list, err)
	}
	if err := SyncBotConfigSnapshotsFromMainConfig(ctx, nil, cfg, "", ""); err != nil {
		t.Fatalf("nil storage sync should no-op: %v", err)
	}
	if err := SyncBotConfigSnapshotsFromMainConfig(ctx, store, nil, "", ""); err != nil {
		t.Fatalf("nil config sync should no-op: %v", err)
	}

	if err := DeleteBotConfigSnapshot(ctx, store, "bot-1"); err != nil {
		t.Fatalf("delete bot: %v", err)
	}
	if deleted, err := store.GetBotConfigDocument(ctx, "bot-1"); err != nil || deleted != nil {
		t.Fatalf("deleted bot=%#v err=%v", deleted, err)
	}
	if err := DeleteBotConfigSnapshot(ctx, nil, "bot-1"); err == nil {
		t.Fatalf("nil storage delete should fail")
	}
	if err := DeleteBotConfigSnapshot(ctx, store, ""); err == nil {
		t.Fatalf("empty bot delete should fail")
	}
}

func TestAppConfigSnapshotsEncryptSecretsAndMigrateHistory(t *testing.T) {
	t.Setenv(config.MasterKeyEnvVar, "01234567890123456789012345678901")
	ctx := context.Background()
	store, err := NewSQLStorage(filepath.Join(t.TempDir(), "app_config_secrets.db"))
	if err != nil {
		t.Fatalf("new sql storage: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	legacy := `{"exchanges":{"legacy":{"api_key":"legacy-plaintext-key","secret_key":"legacy-plaintext-secret"}},"unknown":{"keep":true}}`
	if _, err := store.db.ExecContext(ctx, `INSERT INTO app_config_history (revision, content, content_hash, operator, source) VALUES (?, ?, ?, ?, ?)`, 0, legacy, sha256Hex(legacy), "legacy", "fixture"); err != nil {
		t.Fatalf("insert legacy history fixture: %v", err)
	}

	cfg := config.CreateMinimalConfig()
	cfg.App.CurrentExchange = "binance"
	cfg.Exchanges = map[string]config.ExchangeConfig{"binance": {
		APIKey: "exchange-api-key", SecretKey: "exchange-secret", Passphrase: "exchange-passphrase",
	}}
	cfg.AI.APIKey = "global-ai-key"
	cfg.AI.GeminiAPIKey = "gemini-ai-key"
	cfg.AI.Upstreams = map[string]config.AIUpstreamProfile{"primary": {APIKey: "profile-ai-key"}}
	if _, err := SaveAppConfigSnapshot(ctx, store, cfg, "tester", "secret-test"); err != nil {
		t.Fatalf("save config: %v", err)
	}
	doc, err := store.GetAppConfigDocument(ctx)
	if err != nil || doc == nil {
		t.Fatalf("load persisted config: doc=%+v err=%v", doc, err)
	}
	for _, secret := range []string{"exchange-api-key", "exchange-secret", "exchange-passphrase", "global-ai-key", "gemini-ai-key", "profile-ai-key"} {
		if strings.Contains(doc.Content, secret) {
			t.Fatalf("persisted app_config leaked plaintext credential %q", secret)
		}
	}
	var loaded config.Config
	if err := json.Unmarshal([]byte(doc.Content), &loaded); err != nil {
		t.Fatalf("decode persisted config: %v", err)
	}
	if err := config.DecryptSensitiveFields(&loaded); err != nil {
		t.Fatalf("decrypt persisted config: %v", err)
	}
	if loaded.Exchanges["binance"].APIKey != "exchange-api-key" || loaded.Exchanges["binance"].SecretKey != "exchange-secret" ||
		loaded.Exchanges["binance"].Passphrase != "exchange-passphrase" || loaded.AI.APIKey != "global-ai-key" ||
		loaded.AI.GeminiAPIKey != "gemini-ai-key" || loaded.AI.Upstreams["primary"].APIKey != "profile-ai-key" {
		t.Fatalf("persisted credentials did not round trip through encryption")
	}

	if _, err := SaveAppConfigSnapshot(ctx, store, config.CreateMinimalConfig(), "tester", "migrate-legacy-history"); err != nil {
		t.Fatalf("save config while migrating legacy history: %v", err)
	}
	var migrated string
	var migratedHash string
	if err := store.db.QueryRowContext(ctx, `SELECT content, content_hash FROM app_config_history WHERE revision = 0`).Scan(&migrated, &migratedHash); err != nil {
		t.Fatalf("read migrated legacy history: %v", err)
	}
	if strings.Contains(migrated, "legacy-plaintext-key") || strings.Contains(migrated, "legacy-plaintext-secret") {
		t.Fatal("legacy app_config_history still contains plaintext credentials")
	}
	if migratedHash != sha256Hex(migrated) {
		t.Fatal("legacy app_config_history hash was not updated with protected content")
	}
	var preserved map[string]json.RawMessage
	if err := json.Unmarshal([]byte(migrated), &preserved); err != nil || len(preserved["unknown"]) == 0 {
		t.Fatalf("legacy migration lost unknown config fields: %s err=%v", migrated, err)
	}
}

func TestProtectAppConfigCredentialsMigratesExistingRowsAtomically(t *testing.T) {
	t.Setenv(config.MasterKeyEnvVar, "01234567890123456789012345678901")
	ctx := context.Background()
	store, err := NewSQLStorage(filepath.Join(t.TempDir(), "app_config_legacy.db"))
	if err != nil {
		t.Fatalf("new sql storage: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.EnsureAppConfigDocumentTables(); err != nil {
		t.Fatalf("ensure app config tables: %v", err)
	}
	legacy := `{"Exchanges":{"binance":{"api_key":"legacy-current-key","secret_key":"legacy-current-secret"}},"AI":{"APIKey":"legacy-ai-key"},"custom":{"kept":true}}`
	if _, err := store.db.ExecContext(ctx, `INSERT INTO app_config (id, schema_version, content, revision, content_hash) VALUES (?, ?, ?, ?, ?)`,
		appConfigSingletonID, 1, legacy, 7, sha256Hex(legacy)); err != nil {
		t.Fatalf("insert legacy current config: %v", err)
	}
	if _, err := store.db.ExecContext(ctx, `INSERT INTO app_config_history (revision, content, content_hash, operator, source) VALUES (?, ?, ?, ?, ?)`,
		7, legacy, sha256Hex(legacy), "legacy", "fixture"); err != nil {
		t.Fatalf("insert legacy history: %v", err)
	}
	if err := ProtectAppConfigCredentials(ctx, store); err != nil {
		t.Fatalf("protect legacy config: %v", err)
	}
	doc, err := store.GetAppConfigDocument(ctx)
	if err != nil || doc == nil || doc.Revision != 7 || doc.ContentHash != sha256Hex(doc.Content) {
		t.Fatalf("migrated current config metadata: doc=%+v err=%v", doc, err)
	}
	for _, secret := range []string{"legacy-current-key", "legacy-current-secret", "legacy-ai-key"} {
		if strings.Contains(doc.Content, secret) {
			t.Fatalf("current config still contains plaintext credential %q", secret)
		}
	}
	var loaded config.Config
	if err := json.Unmarshal([]byte(doc.Content), &loaded); err != nil {
		t.Fatalf("decode migrated current config: %v", err)
	}
	if err := config.DecryptSensitiveFields(&loaded); err != nil {
		t.Fatalf("decrypt migrated current config: %v", err)
	}
	if loaded.Exchanges["binance"].APIKey != "legacy-current-key" || loaded.Exchanges["binance"].SecretKey != "legacy-current-secret" || loaded.AI.APIKey != "legacy-ai-key" {
		t.Fatal("current credentials did not survive in-place encryption migration")
	}
	var history string
	if err := store.db.QueryRowContext(ctx, `SELECT content FROM app_config_history WHERE revision = 7`).Scan(&history); err != nil {
		t.Fatalf("read migrated history: %v", err)
	}
	if strings.Contains(history, "legacy-current-key") || !strings.Contains(history, "custom") {
		t.Fatalf("legacy history secret migration or unknown-field preservation failed: %s", history)
	}
}

func TestProtectAppConfigCredentialsFailsClosedWithInvalidMasterKey(t *testing.T) {
	t.Setenv(config.MasterKeyEnvVar, "short-invalid-key")
	ctx := context.Background()
	store, err := NewSQLStorage(filepath.Join(t.TempDir(), "app_config_bad_key.db"))
	if err != nil {
		t.Fatalf("new sql storage: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.EnsureAppConfigDocumentTables(); err != nil {
		t.Fatalf("ensure app config tables: %v", err)
	}
	legacy := `{"Exchanges":{"binance":{"api_key":"plaintext-must-not-be-written"}}}`
	if _, err := store.db.ExecContext(ctx, `INSERT INTO app_config (id, schema_version, content, revision, content_hash) VALUES (?, ?, ?, ?, ?)`,
		appConfigSingletonID, 1, legacy, 3, sha256Hex(legacy)); err != nil {
		t.Fatalf("insert legacy config: %v", err)
	}
	if err := ProtectAppConfigCredentials(ctx, store); err == nil {
		t.Fatal("credential migration succeeded with an invalid master key")
	}
	var content string
	var revision int
	if err := store.db.QueryRowContext(ctx, `SELECT content, revision FROM app_config WHERE id = ?`, appConfigSingletonID).Scan(&content, &revision); err != nil {
		t.Fatalf("read config after rejected migration: %v", err)
	}
	if content != legacy || revision != 3 {
		t.Fatalf("failed migration partially changed config: revision=%d content=%s", revision, content)
	}
	var markerCount int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM app_config_security_migrations WHERE id = 1`).Scan(&markerCount); err != nil {
		t.Fatalf("read migration marker after rollback: %v", err)
	}
	if markerCount != 0 {
		t.Fatal("failed migration left its completion marker committed")
	}
}

func TestSaveAppConfigSnapshotRollsBackWhenBotSnapshotSyncFails(t *testing.T) {
	ctx := context.Background()
	store, err := NewSQLStorage(filepath.Join(t.TempDir(), "app_config_atomic.db"))
	if err != nil {
		t.Fatalf("new sql storage: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	baseline := config.CreateMinimalConfig()
	baseline.App.CurrentExchange = "baseline"
	if _, err := SaveAppConfigSnapshot(ctx, store, baseline, "tester", "baseline"); err != nil {
		t.Fatalf("save baseline config: %v", err)
	}
	if _, err := store.db.Exec(`CREATE TRIGGER reject_bot_snapshot BEFORE INSERT ON bot_configs
		WHEN NEW.bot_id = 'fail-bot' BEGIN SELECT RAISE(ABORT, 'injected bot snapshot failure'); END`); err != nil {
		t.Fatalf("create failure trigger: %v", err)
	}

	updated := config.CreateMinimalConfig()
	updated.App.CurrentExchange = "updated"
	updated.Bots = []config.BotConfig{{ID: "fail-bot", Exchange: "binance", Symbol: "BTCUSDT", MarketType: "futures"}}
	if _, err := SaveAppConfigSnapshot(ctx, store, updated, "tester", "atomic-test"); err == nil {
		t.Fatal("save unexpectedly succeeded when bot snapshot sync failed")
	}

	doc, err := store.GetAppConfigDocument(ctx)
	if err != nil || doc == nil || doc.Revision != 1 || !strings.Contains(doc.Content, `"CurrentExchange":"baseline"`) {
		t.Fatalf("app config changed despite transaction failure: doc=%+v err=%v", doc, err)
	}
	botDoc, err := store.GetBotConfigDocument(ctx, "fail-bot")
	if err != nil || botDoc != nil {
		t.Fatalf("failed bot snapshot should not be visible: doc=%+v err=%v", botDoc, err)
	}
}

func TestSaveConfigMigrationSnapshotsRollsBackAsOneTransaction(t *testing.T) {
	ctx := context.Background()
	store, err := NewSQLStorage(filepath.Join(t.TempDir(), "config_migration_atomic.db"))
	if err != nil {
		t.Fatalf("new sql storage: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.EnsureAppConfigDocumentTables(); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec(`CREATE TRIGGER reject_migrated_bot BEFORE INSERT ON bot_configs
		WHEN NEW.bot_id = 'reject-bot' BEGIN SELECT RAISE(ABORT, 'injected migration failure'); END`); err != nil {
		t.Fatalf("create failure trigger: %v", err)
	}
	bot := &config.BotConfigFile{BotID: "reject-bot", Exchange: "binance", Symbol: "BTCUSDT"}
	if _, err := SaveConfigMigrationSnapshots(ctx, store, []byte(`{"app":{"name":"imported"}}`), []*config.BotConfigFile{bot}, "test", "migration"); err == nil {
		t.Fatal("migration unexpectedly succeeded when bot snapshot insert failed")
	}
	appDoc, err := store.GetAppConfigDocument(ctx)
	if err != nil || appDoc != nil {
		t.Fatalf("app config committed despite bot failure: doc=%+v err=%v", appDoc, err)
	}
	botDoc, err := store.GetBotConfigDocument(ctx, "reject-bot")
	if err != nil || botDoc != nil {
		t.Fatalf("bot config committed despite migration failure: doc=%+v err=%v", botDoc, err)
	}
}

type assertStorageErr string

func (e assertStorageErr) Error() string {
	return string(e)
}
