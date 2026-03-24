-- QuantMesh：主配置 / Bot 文檔表（與 storage/migrateAppConfigDocumentTables 一致）
-- 手動修復：sqlite3 /path/to/quantmesh.db < sqlite_app_config_document_tables.sql
-- 推薦優先使用：./quantmesh --repair-app-config-tables config.yaml

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
