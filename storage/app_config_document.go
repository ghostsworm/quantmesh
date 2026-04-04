package storage

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"quantmesh/config"

	"gopkg.in/yaml.v3"
)

const appConfigSingletonID = 1

// AppConfigDocument 主配置快照（app_config 表）
type AppConfigDocument struct {
	ID             int64
	SchemaVersion  int
	Content        string
	Revision       int
	ContentHash    string
	UpdatedAt      time.Time
}

// migrateAppConfigDocumentTables 創建 app_config / app_config_history / bot_configs / bot_config_history（SQLite）
func migrateAppConfigDocumentTables(db *sql.DB) error {
	ddl := `
CREATE TABLE IF NOT EXISTS app_config (
	id INTEGER PRIMARY KEY CHECK (id = 1),
	schema_version INTEGER NOT NULL DEFAULT 1,
	content TEXT NOT NULL DEFAULT '',
	revision INTEGER NOT NULL DEFAULT 0,
	content_hash TEXT,
	updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE TABLE IF NOT EXISTS app_config_history (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	revision INTEGER NOT NULL,
	content TEXT NOT NULL,
	content_hash TEXT,
	operator TEXT,
	source TEXT,
	created_at TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_app_config_history_created ON app_config_history(created_at);
CREATE TABLE IF NOT EXISTS bot_configs (
	bot_id TEXT PRIMARY KEY,
	schema_version INTEGER NOT NULL DEFAULT 1,
	content TEXT NOT NULL,
	revision INTEGER NOT NULL DEFAULT 0,
	content_hash TEXT,
	updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE TABLE IF NOT EXISTS bot_config_history (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	bot_id TEXT NOT NULL,
	revision INTEGER NOT NULL,
	content TEXT NOT NULL,
	content_hash TEXT,
	operator TEXT,
	source TEXT,
	created_at TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_bot_config_history_bot ON bot_config_history(bot_id, created_at);
`
	if _, err := db.Exec(ddl); err != nil {
		return fmt.Errorf("创建 app_config 相关表失败: %w", err)
	}
	return nil
}

// migrateAppConfigDocumentTablesMySQL 創建主庫文檔表（MySQL）
func migrateAppConfigDocumentTablesMySQL(db *sql.DB) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS app_config (
			id TINYINT UNSIGNED NOT NULL PRIMARY KEY DEFAULT 1,
			schema_version INT NOT NULL DEFAULT 1,
			content LONGTEXT NOT NULL,
			revision INT NOT NULL DEFAULT 0,
			content_hash VARCHAR(128) NULL,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;`,
		`CREATE TABLE IF NOT EXISTS app_config_history (
			id BIGINT AUTO_INCREMENT PRIMARY KEY,
			revision INT NOT NULL,
			content LONGTEXT NOT NULL,
			content_hash VARCHAR(128) NULL,
			operator VARCHAR(255) NULL,
			source VARCHAR(64) NULL,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			KEY idx_app_config_history_created (created_at)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;`,
		`CREATE TABLE IF NOT EXISTS bot_configs (
			bot_id VARCHAR(128) NOT NULL PRIMARY KEY,
			schema_version INT NOT NULL DEFAULT 1,
			content LONGTEXT NOT NULL,
			revision INT NOT NULL DEFAULT 0,
			content_hash VARCHAR(128) NULL,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;`,
		`CREATE TABLE IF NOT EXISTS bot_config_history (
			id BIGINT AUTO_INCREMENT PRIMARY KEY,
			bot_id VARCHAR(128) NOT NULL,
			revision INT NOT NULL,
			content LONGTEXT NOT NULL,
			content_hash VARCHAR(128) NULL,
			operator VARCHAR(255) NULL,
			source VARCHAR(64) NULL,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			KEY idx_bot_config_history_bot (bot_id, created_at)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			return fmt.Errorf("MySQL 建表失败: %w", err)
		}
	}
	return nil
}

func sha256Hex(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// isAppConfigTableMissing 判斷是否為「文檔表尚未創建」類錯誤（舊庫或中斷遷移）
func isAppConfigTableMissing(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "no such table") {
		return true
	}
	// MySQL: Error 1146: Table 'db.app_config' doesn't exist
	if strings.Contains(msg, "doesn't exist") && strings.Contains(msg, "app_config") {
		return true
	}
	if strings.Contains(msg, "1146") {
		return true
	}
	return false
}

// EnsureAppConfigDocumentTables 幂等創建 app_config、app_config_history、bot_configs、bot_config_history（SQLite 與 MySQL）。
// 用於舊部署補表、CLI 修復，以及啟動時與 NewStorage 內遷移雙重保險。
func (s *SQLStorage) EnsureAppConfigDocumentTables() error {
	if s == nil || s.db == nil {
		return fmt.Errorf("storage 未初始化")
	}
	switch s.dbType {
	case "mysql":
		return migrateAppConfigDocumentTablesMySQL(s.db)
	case "sqlite":
		return migrateAppConfigDocumentTables(s.db)
	default:
		return fmt.Errorf("不支援的數據庫類型: %q", s.dbType)
	}
}

// GetAppConfigDocument 讀取主配置文檔；無行或空內容時返回 nil, nil
func (s *SQLStorage) GetAppConfigDocument(ctx context.Context) (*AppConfigDocument, error) {
	if s == nil || s.db == nil {
		return nil, nil
	}
	var doc AppConfigDocument
	var contentHash sql.NullString
	var updatedAt sql.NullString
	err := s.db.QueryRowContext(ctx, `
		SELECT id, schema_version, content, revision, content_hash, updated_at
		FROM app_config WHERE id = ?`, appConfigSingletonID).Scan(
		&doc.ID, &doc.SchemaVersion, &doc.Content, &doc.Revision, &contentHash, &updatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil && isAppConfigTableMissing(err) {
		if e2 := s.EnsureAppConfigDocumentTables(); e2 != nil {
			return nil, fmt.Errorf("補建 app_config 表失敗: %w (原錯: %v)", e2, err)
		}
		err = s.db.QueryRowContext(ctx, `
		SELECT id, schema_version, content, revision, content_hash, updated_at
		FROM app_config WHERE id = ?`, appConfigSingletonID).Scan(
			&doc.ID, &doc.SchemaVersion, &doc.Content, &doc.Revision, &contentHash, &updatedAt,
		)
	}
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if contentHash.Valid {
		doc.ContentHash = contentHash.String
	}
	if updatedAt.Valid {
		doc.UpdatedAt, _ = time.ParseInLocation("2006-01-02 15:04:05", updatedAt.String, time.Local)
		if doc.UpdatedAt.IsZero() {
			doc.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt.String)
		}
	}
	return &doc, nil
}

// SaveAppConfigSnapshot 將完整主配置 JSON 寫入 app_config 並追加 app_config_history（與 MigrateYAMLToAppConfigDB 入庫一致）。
func SaveAppConfigSnapshot(ctx context.Context, st Storage, cfg *config.Config, operator, source string) (revision int, err error) {
	if st == nil || cfg == nil {
		return 0, fmt.Errorf("SaveAppConfigSnapshot: storage 或配置為空")
	}
	jsonBytes, err := json.Marshal(cfg)
	if err != nil {
		return 0, fmt.Errorf("序列化配置為 JSON: %w", err)
	}
	return SaveAppConfigSnapshotFromJSON(ctx, st, jsonBytes, operator, source)
}

// SaveAppConfigSnapshotFromJSON 將主配置 JSON 寫入 app_config（可含 config.Config 結構體未涵蓋的鍵，例如 security）。
func SaveAppConfigSnapshotFromJSON(ctx context.Context, st Storage, jsonBytes []byte, operator, source string) (revision int, err error) {
	if st == nil {
		return 0, fmt.Errorf("SaveAppConfigSnapshotFromJSON: storage 為空")
	}
	if strings.TrimSpace(string(jsonBytes)) == "" {
		return 0, fmt.Errorf("SaveAppConfigSnapshotFromJSON: JSON 為空")
	}
	ss, ok := st.(*SQLStorage)
	if !ok || ss == nil {
		return 0, fmt.Errorf("SaveAppConfigSnapshotFromJSON: 需要主庫 *SQLStorage")
	}
	if err := ss.EnsureAppConfigDocumentTables(); err != nil {
		return 0, err
	}
	tx, err := ss.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	rev, err := upsertAppConfigTx(ctx, tx, ss.dbType, 1, string(jsonBytes), operator, source)
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return rev, nil
}

// upsertAppConfigTx 寫入主配置與歷史（同一事務）
func upsertAppConfigTx(ctx context.Context, tx *sql.Tx, dbType string, schemaVersion int, contentJSON string, operator, source string) (int, error) {
	hash := sha256Hex(contentJSON)
	var curRev sql.NullInt64
	err := tx.QueryRowContext(ctx, `SELECT revision FROM app_config WHERE id = ?`, appConfigSingletonID).Scan(&curRev)
	nextRev := 1
	if err == nil && curRev.Valid {
		nextRev = int(curRev.Int64) + 1
	} else if err != nil && err != sql.ErrNoRows {
		return 0, err
	}

	histTimeExpr := `datetime('now')`
	if dbType == "mysql" {
		histTimeExpr = `CURRENT_TIMESTAMP`
	}
	qHist := fmt.Sprintf(`
		INSERT INTO app_config_history (revision, content, content_hash, operator, source, created_at)
		VALUES (?, ?, ?, ?, ?, %s)`, histTimeExpr)
	if _, err := tx.ExecContext(ctx, qHist, nextRev, contentJSON, hash, nullStr(operator), nullStr(source)); err != nil {
		return 0, err
	}

	if dbType == "mysql" {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO app_config (id, schema_version, content, revision, content_hash, updated_at)
			VALUES (?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
			ON DUPLICATE KEY UPDATE schema_version=VALUES(schema_version), content=VALUES(content), revision=VALUES(revision), content_hash=VALUES(content_hash), updated_at=CURRENT_TIMESTAMP`,
			appConfigSingletonID, schemaVersion, contentJSON, nextRev, hash); err != nil {
			return 0, err
		}
	} else {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO app_config (id, schema_version, content, revision, content_hash, updated_at)
			VALUES (?, ?, ?, ?, ?, datetime('now'))
			ON CONFLICT(id) DO UPDATE SET
				schema_version=excluded.schema_version,
				content=excluded.content,
				revision=excluded.revision,
				content_hash=excluded.content_hash,
				updated_at=datetime('now')`,
			appConfigSingletonID, schemaVersion, contentJSON, nextRev, hash); err != nil {
			return 0, err
		}
	}
	return nextRev, nil
}

func nullStr(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}

// upsertBotConfigTx 寫入單個 Bot 配置與歷史（同一事務）
func upsertBotConfigTx(ctx context.Context, tx *sql.Tx, dbType string, botID string, schemaVersion int, contentJSON string, operator, source string) (int, error) {
	hash := sha256Hex(contentJSON)
	var curRev sql.NullInt64
	err := tx.QueryRowContext(ctx, `SELECT revision FROM bot_configs WHERE bot_id = ?`, botID).Scan(&curRev)
	nextRev := 1
	if err == nil && curRev.Valid {
		nextRev = int(curRev.Int64) + 1
	} else if err != nil && err != sql.ErrNoRows {
		return 0, err
	}
	histTimeExpr := `datetime('now')`
	if dbType == "mysql" {
		histTimeExpr = `CURRENT_TIMESTAMP`
	}
	qHist := fmt.Sprintf(`
		INSERT INTO bot_config_history (bot_id, revision, content, content_hash, operator, source, created_at)
		VALUES (?, ?, ?, ?, ?, ?, %s)`, histTimeExpr)
	if _, err := tx.ExecContext(ctx, qHist, botID, nextRev, contentJSON, hash, nullStr(operator), nullStr(source)); err != nil {
		return 0, err
	}
	if dbType == "mysql" {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO bot_configs (bot_id, schema_version, content, revision, content_hash, updated_at)
			VALUES (?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
			ON DUPLICATE KEY UPDATE schema_version=VALUES(schema_version), content=VALUES(content), revision=VALUES(revision), content_hash=VALUES(content_hash), updated_at=CURRENT_TIMESTAMP`,
			botID, schemaVersion, contentJSON, nextRev, hash); err != nil {
			return 0, err
		}
	} else {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO bot_configs (bot_id, schema_version, content, revision, content_hash, updated_at)
			VALUES (?, ?, ?, ?, ?, datetime('now'))
			ON CONFLICT(bot_id) DO UPDATE SET
				schema_version=excluded.schema_version,
				content=excluded.content,
				revision=excluded.revision,
				content_hash=excluded.content_hash,
				updated_at=datetime('now')`,
			botID, schemaVersion, contentJSON, nextRev, hash); err != nil {
			return 0, err
		}
	}
	return nextRev, nil
}

// MigrateYAMLMode 主配置 YAML 入庫模式
type MigrateYAMLMode int

const (
	// MigrateYAMLModeCLI 手動遷移：已存在 app_config 時需 QUANTMESH_MIGRATE_APP_CONFIG_FORCE=1
	MigrateYAMLModeCLI MigrateYAMLMode = iota
	// MigrateYAMLModeAuto 啟動自動遷移：已存在有效快照則跳過（不報錯）
	MigrateYAMLModeAuto
)

// MigrateYAMLToAppConfigDB 將主 config.yaml 與 bots/*/config.yaml 寫入主庫文檔表。
// 返回 migrated=true 表示本次寫入了數據庫（可用於歸檔 YAML）。
func MigrateYAMLToAppConfigDB(ctx context.Context, st Storage, mainConfigPath, botsDir string, mode MigrateYAMLMode) (bool, error) {
	ss, ok := st.(*SQLStorage)
	if !ok || ss == nil {
		return false, fmt.Errorf("MigrateYAMLToAppConfigDB: 需要主庫 *SQLStorage")
	}
	if err := ss.EnsureAppConfigDocumentTables(); err != nil {
		return false, fmt.Errorf("確保 app_config 文檔表: %w", err)
	}
	doc, err := ss.GetAppConfigDocument(ctx)
	if err != nil {
		return false, err
	}
	hasExisting := doc != nil && doc.Revision > 0 && strings.TrimSpace(doc.Content) != ""
	if mode == MigrateYAMLModeAuto {
		if hasExisting {
			return false, nil
		}
	} else {
		if hasExisting && os.Getenv("QUANTMESH_MIGRATE_APP_CONFIG_FORCE") != "1" {
			return false, fmt.Errorf("app_config 已存在（revision=%d），設置環境變量 QUANTMESH_MIGRATE_APP_CONFIG_FORCE=1 後重試", doc.Revision)
		}
	}

	op := "cli"
	src := "migrate_yaml"
	if mode == MigrateYAMLModeAuto {
		op = "auto"
		src = "auto_startup"
	}

	cfg, err := config.LoadConfig(mainConfigPath)
	if err != nil {
		return false, err
	}
	jsonBytes, err := json.Marshal(cfg)
	if err != nil {
		return false, err
	}
	tx, err := ss.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()

	if _, err := upsertAppConfigTx(ctx, tx, ss.dbType, 1, string(jsonBytes), op, src); err != nil {
		return false, err
	}

	if botsDir == "" {
		botsDir = "./bots"
	}
	entries, err := os.ReadDir(botsDir)
	if err != nil && !os.IsNotExist(err) {
		return false, err
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		botID := e.Name()
		p := filepath.Join(botsDir, botID, "config.yaml")
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		var bf config.BotConfigFile
		if err := yaml.Unmarshal(data, &bf); err != nil {
			return false, fmt.Errorf("解析 bot %s 配置: %w", botID, err)
		}
		botJSON, err := json.Marshal(bf)
		if err != nil {
			return false, err
		}
		if _, err := upsertBotConfigTx(ctx, tx, ss.dbType, botID, 1, string(botJSON), op, src); err != nil {
			return false, err
		}
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

// loadConfigFromAppConfigDocument 從已打開的主庫讀取 app_config 並解析為 Config（無有效快照時返回 nil, nil）。
func loadConfigFromAppConfigDocument(st *SQLStorage) (*config.Config, error) {
	if st == nil {
		return nil, nil
	}
	doc, err := st.GetAppConfigDocument(context.Background())
	if err != nil {
		return nil, err
	}
	if doc == nil || doc.Revision < 1 || strings.TrimSpace(doc.Content) == "" {
		return nil, nil
	}
	return config.LoadConfigFromJSON([]byte(doc.Content))
}

// LoadConfigFromAppConfigDBIfExists 在磁盤無 config.yaml 時嘗試從主庫 app_config 加載快照。
// 優先嘗試 SQLite 文件路徑（歷史行為）；若無有效快照且設置了 QUANTMESH_DATABASE_DSN（MySQL 連接串），則再嘗試 MySQL（純 RDS 部署可不依賴本地 quantmesh.db）。
func LoadConfigFromAppConfigDBIfExists(sqlitePath string) (*config.Config, error) {
	trySQLite := strings.TrimSpace(sqlitePath) != ""
	if trySQLite {
		st, err := NewSQLStorage(sqlitePath)
		if err != nil {
			return nil, err
		}
		cfg, err := loadConfigFromAppConfigDocument(st)
		_ = st.Close()
		if err != nil {
			return nil, err
		}
		if cfg != nil {
			return cfg, nil
		}
	}

	dsn := strings.TrimSpace(os.Getenv("QUANTMESH_DATABASE_DSN"))
	if dsn != "" && IsMySQLStorageDSNString(dsn) {
		st, err := NewMySQLStorage(dsn)
		if err != nil {
			return nil, err
		}
		cfg, err := loadConfigFromAppConfigDocument(st)
		_ = st.Close()
		if err != nil {
			return nil, err
		}
		return cfg, nil
	}

	return nil, nil
}

// ApplyAppConfigFromDBIfPresent 若 app_config 有有效快照則覆蓋內存中的 *Config（環境變量 QUANTMESH_USE_APP_CONFIG=0 可禁用）
func ApplyAppConfigFromDBIfPresent(st Storage, cfg **config.Config) error {
	if cfg == nil || *cfg == nil {
		return nil
	}
	if os.Getenv("QUANTMESH_USE_APP_CONFIG") == "0" {
		return nil
	}
	ss, ok := st.(*SQLStorage)
	if !ok || ss == nil {
		return nil
	}
	doc, err := ss.GetAppConfigDocument(context.Background())
	if err != nil {
		return err
	}
	if doc == nil || doc.Revision < 1 || strings.TrimSpace(doc.Content) == "" {
		return nil
	}
	newCfg, err := config.LoadConfigFromJSON([]byte(doc.Content))
	if err != nil {
		return fmt.Errorf("從數據庫加載 app_config 失敗: %w", err)
	}
	*cfg = newCfg
	return nil
}
