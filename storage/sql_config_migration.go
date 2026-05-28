package storage

import (
	"database/sql"
	"fmt"
)

// ========== 配置管理表迁移（SQLite） ==========

// migrateConfigTables 迁移配置管理相关表
func migrateConfigTables(db *sql.DB) error {
	configSQL := `
	CREATE TABLE IF NOT EXISTS config_entries (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		key TEXT NOT NULL,
		scope TEXT NOT NULL,
		scope_id TEXT NOT NULL DEFAULT '',
		type TEXT NOT NULL,
		value TEXT,
		number_value REAL DEFAULT 0,
		bool_value INTEGER DEFAULT 0,
		json_value TEXT,
		default_value TEXT,
		description TEXT,
		category TEXT,
		display_name TEXT,
		editable INTEGER DEFAULT 1,
		required INTEGER DEFAULT 0,
		validate_regexp TEXT,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		updated_by TEXT,
		version INTEGER DEFAULT 0,
		UNIQUE(key, scope, scope_id)
	);
	CREATE INDEX IF NOT EXISTS idx_config_entries_scope ON config_entries(scope, scope_id);
	CREATE INDEX IF NOT EXISTS idx_config_entries_category ON config_entries(category);
	CREATE INDEX IF NOT EXISTS idx_config_entries_updated_at ON config_entries(updated_at);
	`

	historySQL := `
	CREATE TABLE IF NOT EXISTS config_history (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		config_id INTEGER NOT NULL,
		key TEXT NOT NULL,
		scope TEXT NOT NULL,
		scope_id TEXT NOT NULL,
		old_value TEXT,
		new_value TEXT,
		old_number REAL DEFAULT 0,
		new_number REAL DEFAULT 0,
		old_bool INTEGER DEFAULT 0,
		new_bool INTEGER DEFAULT 0,
		reason TEXT,
		changed_by TEXT,
		changed_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY(config_id) REFERENCES config_entries(id) ON DELETE CASCADE
	);
	CREATE INDEX IF NOT EXISTS idx_config_history_config_id ON config_history(config_id);
	CREATE INDEX IF NOT EXISTS idx_config_history_key ON config_history(key, scope, scope_id);
	CREATE INDEX IF NOT EXISTS idx_config_history_changed_at ON config_history(changed_at);
	`

	if _, err := db.Exec(configSQL); err != nil {
		return fmt.Errorf("创建 config_entries 表失败: %w", err)
	}

	if _, err := db.Exec(historySQL); err != nil {
		return fmt.Errorf("创建 config_history 表失败: %w", err)
	}

	return nil
}
