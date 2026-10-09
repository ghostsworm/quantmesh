package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"quantmesh/config"
)

const appConfigCredentialEncryptionVersion = 1

// protectAppConfigSecrets encrypts credential fields before they enter either
// app_config or its append-only history. Unknown top-level JSON fields are
// preserved because app_config also carries sections not modeled by Config.
func protectAppConfigSecrets(content string, allowKeyGeneration bool) (string, error) {
	var document map[string]json.RawMessage
	if err := json.Unmarshal([]byte(content), &document); err != nil {
		return "", fmt.Errorf("decode app config before credential protection: %w", err)
	}
	if document == nil {
		return "", fmt.Errorf("app config must be a JSON object")
	}

	fields, err := appConfigSecretFields(document)
	if err != nil {
		return "", err
	}
	needsKey := false
	needsEncryption := false
	for _, field := range fields {
		if field.value == "" {
			continue
		}
		needsKey = true
		if !config.IsEncrypted(field.value) {
			needsEncryption = true
		}
	}
	if !needsKey {
		return content, nil
	}

	key, err := config.LoadMasterKey("")
	if err != nil {
		return "", fmt.Errorf("load app config credential key: %w", err)
	}
	if key == nil && needsEncryption {
		if !allowKeyGeneration {
			return "", fmt.Errorf("MySQL app config encryption requires a pre-provisioned shared master key")
		}
		key, err = config.LoadOrGenerateMasterKey("")
		if err != nil {
			return "", fmt.Errorf("initialize app config credential key: %w", err)
		}
	}
	if key == nil {
		return "", fmt.Errorf("app config contains encrypted credentials but the master key is unavailable; configure %s or the shared master-key file", config.MasterKeyEnvVar)
	}

	for _, field := range fields {
		if field.value == "" {
			continue
		}
		if config.IsEncrypted(field.value) {
			if _, err := config.DecryptAPIKey(field.value, key); err != nil {
				return "", fmt.Errorf("verify encrypted app config credential %s: %w", field.name, err)
			}
			continue
		}
		ciphertext, err := config.EncryptAPIKey(field.value, key)
		if err != nil {
			return "", fmt.Errorf("encrypt app config credential %s: %w", field.name, err)
		}
		encoded, err := json.Marshal(ciphertext)
		if err != nil {
			return "", fmt.Errorf("encode protected app config credential %s: %w", field.name, err)
		}
		if err := setAppConfigRawPath(document, field.path, encoded); err != nil {
			return "", fmt.Errorf("write protected app config credential %s: %w", field.name, err)
		}
	}

	encoded, err := json.Marshal(document)
	if err != nil {
		return "", fmt.Errorf("encode protected app config: %w", err)
	}
	return string(encoded), nil
}

type appConfigSecretField struct {
	name  string
	path  []string
	value string
}

func appConfigSecretFields(document map[string]json.RawMessage) ([]appConfigSecretField, error) {
	fields := make([]appConfigSecretField, 0, 8)
	appendFields := func(sectionName string, path []string, object map[string]json.RawMessage, names ...string) error {
		for _, name := range names {
			secret, ok := object[name]
			if !ok || string(secret) == "null" {
				continue
			}
			var value string
			if err := json.Unmarshal(secret, &value); err != nil {
				return fmt.Errorf("app config %s has invalid %s", sectionName, name)
			}
			fieldPath := append(append([]string(nil), path...), name)
			fields = append(fields, appConfigSecretField{name: sectionName + "." + name, path: fieldPath, value: value})
		}
		return nil
	}
	appendMapFields := func(sectionName string, path []string, section json.RawMessage, names ...string) error {
		if len(section) == 0 || string(section) == "null" {
			return nil
		}
		var objects map[string]json.RawMessage
		if err := json.Unmarshal(section, &objects); err != nil || objects == nil {
			return fmt.Errorf("app config %s section must be an object", sectionName)
		}
		for objectName, raw := range objects {
			var object map[string]json.RawMessage
			if err := json.Unmarshal(raw, &object); err != nil || object == nil {
				return fmt.Errorf("app config %s entry %q must be an object", sectionName, objectName)
			}
			entryPath := append(append([]string(nil), path...), objectName)
			if err := appendFields(sectionName+"."+objectName, entryPath, object, names...); err != nil {
				return err
			}
		}
		return nil
	}

	exchangesKey := "exchanges"
	if len(document[exchangesKey]) == 0 {
		exchangesKey = "Exchanges"
	}
	if err := appendMapFields("exchanges", []string{exchangesKey}, document[exchangesKey], "api_key", "secret_key", "passphrase"); err != nil {
		return nil, err
	}
	aiKey := "ai"
	if len(document[aiKey]) == 0 {
		aiKey = "AI"
	}
	var ai map[string]json.RawMessage
	if raw := document[aiKey]; len(raw) > 0 && string(raw) != "null" {
		if err := json.Unmarshal(raw, &ai); err != nil || ai == nil {
			return nil, fmt.Errorf("app config ai section must be an object")
		}
		if err := appendFields("ai", []string{aiKey}, ai, "api_key", "gemini_api_key", "APIKey", "GeminiAPIKey"); err != nil {
			return nil, err
		}
		upstreamsKey := "upstreams"
		if len(ai[upstreamsKey]) == 0 {
			upstreamsKey = "Upstreams"
		}
		if upstreamsRaw := ai[upstreamsKey]; len(upstreamsRaw) > 0 && string(upstreamsRaw) != "null" {
			if err := appendMapFields("ai.upstreams", []string{aiKey, upstreamsKey}, upstreamsRaw, "api_key", "APIKey"); err != nil {
				return nil, err
			}
		}
	}
	return fields, nil
}

func setAppConfigRawPath(object map[string]json.RawMessage, path []string, value json.RawMessage) error {
	if len(path) == 0 {
		return fmt.Errorf("empty credential path")
	}
	if len(path) == 1 {
		object[path[0]] = value
		return nil
	}
	child := make(map[string]json.RawMessage)
	if err := json.Unmarshal(object[path[0]], &child); err != nil || child == nil {
		return fmt.Errorf("credential parent %q is not an object", path[0])
	}
	if err := setAppConfigRawPath(child, path[1:], value); err != nil {
		return err
	}
	encoded, err := json.Marshal(child)
	if err != nil {
		return fmt.Errorf("encode credential parent %q: %w", path[0], err)
	}
	object[path[0]] = encoded
	return nil
}

// protectAppConfigHistory migrates existing plaintext snapshots in the same
// transaction as a new write, so history cannot remain a credential side door.
func protectAppConfigHistory(ctx context.Context, tx *sql.Tx, dbType string) error {
	query := `SELECT id, content FROM app_config_history ORDER BY id`
	if dbType == "mysql" {
		query += ` FOR UPDATE`
	}
	rows, err := tx.QueryContext(ctx, query)
	if err != nil {
		return fmt.Errorf("read app config history for credential migration: %w", err)
	}
	type row struct {
		id      int64
		content string
	}
	var pending []row
	for rows.Next() {
		var item row
		if err := rows.Scan(&item.id, &item.content); err != nil {
			rows.Close()
			return fmt.Errorf("scan app config history for credential migration: %w", err)
		}
		protected, err := protectAppConfigSecrets(item.content, dbType != "mysql")
		if err != nil {
			rows.Close()
			return fmt.Errorf("protect app config history revision %d: %w", item.id, err)
		}
		if protected != item.content {
			pending = append(pending, row{id: item.id, content: protected})
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("iterate app config history for credential migration: %w", err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close app config history credential migration rows: %w", err)
	}
	for _, item := range pending {
		if _, err := tx.ExecContext(ctx, `UPDATE app_config_history SET content = ?, content_hash = ? WHERE id = ?`,
			item.content, sha256Hex(item.content), item.id); err != nil {
			return fmt.Errorf("update protected app config history revision %d: %w", item.id, err)
		}
	}
	return nil
}

// ProtectAppConfigCredentials migrates existing app_config and history rows
// atomically before they are exposed to the runtime. Revisions are preserved;
// only content and its integrity hash change during this format migration.
func ProtectAppConfigCredentials(ctx context.Context, st Storage) error {
	if ctx == nil || st == nil {
		return fmt.Errorf("app config credential migration requires context and storage")
	}
	ss, ok := st.(*SQLStorage)
	if !ok || ss == nil {
		return fmt.Errorf("app config credential migration requires SQL storage")
	}
	if err := ss.EnsureAppConfigDocumentTables(); err != nil {
		return err
	}
	tx, err := ss.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin app config credential migration: %w", err)
	}
	defer tx.Rollback()
	version, err := ensureAppConfigEncryptionMigrationRow(ctx, tx, ss.dbType)
	if err != nil {
		return err
	}
	if version >= appConfigCredentialEncryptionVersion {
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("finish app config credential migration check: %w", err)
		}
		return nil
	}
	query := `SELECT content, revision FROM app_config WHERE id = ?`
	if ss.dbType == "mysql" {
		query += ` FOR UPDATE`
	}
	var content string
	var revision int64
	err = tx.QueryRowContext(ctx, query, appConfigSingletonID).Scan(&content, &revision)
	if err != nil && err != sql.ErrNoRows {
		return fmt.Errorf("read app config for credential migration: %w", err)
	}
	if err == nil {
		protected, protectErr := protectAppConfigSecrets(content, ss.dbType != "mysql")
		if protectErr != nil {
			return fmt.Errorf("protect current app config credentials: %w", protectErr)
		}
		if protected != content {
			result, err := tx.ExecContext(ctx, `UPDATE app_config SET content = ?, content_hash = ? WHERE id = ? AND revision = ?`,
				protected, sha256Hex(protected), appConfigSingletonID, revision)
			if err != nil {
				return fmt.Errorf("update protected app config: %w", err)
			}
			if affected, err := result.RowsAffected(); err != nil || affected != 1 {
				return fmt.Errorf("app config changed during credential migration (rows=%d err=%v)", affected, err)
			}
		}
	}
	if err := protectAppConfigHistory(ctx, tx, ss.dbType); err != nil {
		return err
	}
	if err := markAppConfigEncryptionMigrationComplete(ctx, tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit app config credential migration: %w", err)
	}
	return nil
}

func ensureAppConfigEncryptionMigrationRow(ctx context.Context, tx *sql.Tx, dbType string) (int, error) {
	if dbType == "mysql" {
		if _, err := tx.ExecContext(ctx, `INSERT IGNORE INTO app_config_security_migrations (id, credential_encryption_version) VALUES (1, 0)`); err != nil {
			return 0, fmt.Errorf("initialize app config security migration marker: %w", err)
		}
	} else {
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO app_config_security_migrations (id, credential_encryption_version) VALUES (1, 0)`); err != nil {
			return 0, fmt.Errorf("initialize app config security migration marker: %w", err)
		}
	}
	query := `SELECT credential_encryption_version FROM app_config_security_migrations WHERE id = 1`
	if dbType == "mysql" {
		query += ` FOR UPDATE`
	}
	var version int
	if err := tx.QueryRowContext(ctx, query).Scan(&version); err != nil {
		return 0, fmt.Errorf("read app config security migration marker: %w", err)
	}
	return version, nil
}

func markAppConfigEncryptionMigrationComplete(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx, `UPDATE app_config_security_migrations SET credential_encryption_version = ? WHERE id = 1`, appConfigCredentialEncryptionVersion); err != nil {
		return fmt.Errorf("mark app config credential migration complete: %w", err)
	}
	return nil
}
