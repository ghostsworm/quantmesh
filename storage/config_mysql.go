package storage

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

// MySQLConfigStorage MySQL 配置存储实现
type MySQLConfigStorage struct {
	db     *sql.DB
	closed bool
}

// NewMySQLConfigStorage 创建 MySQL 配置存储
func NewMySQLConfigStorage(dsn string) (*MySQLConfigStorage, error) {
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("打开 MySQL 数据库失败: %w", err)
	}

	// 设置连接池
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)

	// 测试连接
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("连接 MySQL 数据库失败: %w", err)
	}

	// 创建配置表
	if err := migrateMySQLConfigTables(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("创建配置表失败: %w", err)
	}

	return &MySQLConfigStorage{db: db}, nil
}

// migrateMySQLConfigTables 创建 MySQL 配置表
func migrateMySQLConfigTables(db *sql.DB) error {
	// 创建配置条目表
	configTableSQL := `
	CREATE TABLE IF NOT EXISTS config_entries (
		id BIGINT AUTO_INCREMENT PRIMARY KEY,
		config_key VARCHAR(200) NOT NULL,
		scope VARCHAR(20) NOT NULL,
		scope_id VARCHAR(100) NOT NULL,
		type VARCHAR(20) NOT NULL,
		value TEXT,
		number_value DOUBLE,
		bool_value TINYINT(1) DEFAULT 0,
		json_value TEXT,
		default_value TEXT,
		description TEXT,
		category VARCHAR(100),
		display_name VARCHAR(200),
		editable TINYINT(1) DEFAULT 1,
		required TINYINT(1) DEFAULT 0,
		validate_regexp VARCHAR(200),
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
		updated_by VARCHAR(100),
		version BIGINT DEFAULT 1,
		UNIQUE KEY idx_key_scope (config_key, scope, scope_id),
		KEY idx_scope_category (scope, category),
		KEY idx_updated_at (updated_at)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;`

	if _, err := db.Exec(configTableSQL); err != nil {
		return fmt.Errorf("创建 config_entries 表失败: %w", err)
	}

	// 创建配置历史表
	historyTableSQL := `
	CREATE TABLE IF NOT EXISTS config_history (
		id BIGINT AUTO_INCREMENT PRIMARY KEY,
		config_id BIGINT NOT NULL,
		config_key VARCHAR(200) NOT NULL,
		scope VARCHAR(20) NOT NULL,
		scope_id VARCHAR(100) NOT NULL,
		old_value TEXT,
		new_value TEXT,
		old_number DOUBLE,
		new_number DOUBLE,
		old_bool TINYINT(1),
		new_bool TINYINT(1),
		reason TEXT,
		changed_by VARCHAR(100),
		changed_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		KEY idx_config_id (config_id),
		KEY idx_scope_key (scope, config_key),
		KEY idx_changed_at (changed_at)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;`

	if _, err := db.Exec(historyTableSQL); err != nil {
		return fmt.Errorf("创建 config_history 表失败: %w", err)
	}

	return nil
}

// GetConfig 获取单个配置
func (s *MySQLConfigStorage) GetConfig(ctx context.Context, scope ConfigScope, scopeID, key string) (*ConfigEntry, error) {
	var entry ConfigEntry
	var enabled, required int
	var jsonValue, defaultValue, description, category, displayName, validateRegexp, updatedBy sql.NullString
	var createdAt, updatedAt sql.NullTime

	err := s.db.QueryRowContext(ctx, `
		SELECT id, config_key, scope, scope_id, type, value, number_value, bool_value,
		       json_value, default_value, description, category, display_name,
		       editable, required, validate_regexp, created_at, updated_at, updated_by, version
		FROM config_entries
		WHERE config_key = ? AND scope = ? AND scope_id = ?`,
		key, scope, scopeID).Scan(
		&entry.ID, &entry.Key, &entry.Scope, &entry.ScopeID, &entry.Type,
		&entry.Value, &entry.NumberValue, &entry.BoolValue,
		&jsonValue, &defaultValue, &description, &category, &displayName,
		&enabled, &required, &validateRegexp, &createdAt, &updatedAt,
		&updatedBy, &entry.Version,
	)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("查询配置失败: %w", err)
	}

	// 处理可空字段
	if jsonValue.Valid {
		entry.JSONValue = jsonValue.String
	}
	if defaultValue.Valid {
		entry.DefaultValue = defaultValue.String
	}
	if description.Valid {
		entry.Description = description.String
	}
	if category.Valid {
		entry.Category = category.String
	}
	if displayName.Valid {
		entry.DisplayName = displayName.String
	}
	if validateRegexp.Valid {
		entry.ValidateRegexp = validateRegexp.String
	}
	if updatedBy.Valid {
		entry.UpdatedBy = updatedBy.String
	}
	if createdAt.Valid {
		entry.CreatedAt = createdAt.Time
	}
	if updatedAt.Valid {
		entry.UpdatedAt = updatedAt.Time
	}
	entry.Editable = enabled == 1
	entry.Required = required == 1

	return &entry, nil
}

// GetConfigsByScope 按作用域获取配置
func (s *MySQLConfigStorage) GetConfigsByScope(ctx context.Context, scope ConfigScope, scopeID string) ([]*ConfigEntry, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, config_key, scope, scope_id, type, value, number_value, bool_value,
		       json_value, default_value, description, category, display_name,
		       editable, required, validate_regexp, created_at, updated_at, updated_by, version
		FROM config_entries
		WHERE scope = ? AND scope_id = ?
		ORDER BY category, config_key`,
		scope, scopeID)
	if err != nil {
		return nil, fmt.Errorf("查询配置失败: %w", err)
	}
	defer rows.Close()

	return s.scanConfigEntries(rows)
}

// GetConfigsByCategory 按分类获取配置
func (s *MySQLConfigStorage) GetConfigsByCategory(ctx context.Context, scope ConfigScope, category string) ([]*ConfigEntry, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, config_key, scope, scope_id, type, value, number_value, bool_value,
		       json_value, default_value, description, category, display_name,
		       editable, required, validate_regexp, created_at, updated_at, updated_by, version
		FROM config_entries
		WHERE scope = ? AND category = ?
		ORDER BY config_key`,
		scope, category)
	if err != nil {
		return nil, fmt.Errorf("查询配置失败: %w", err)
	}
	defer rows.Close()

	return s.scanConfigEntries(rows)
}

// GetAllConfigs 获取所有配置
func (s *MySQLConfigStorage) GetAllConfigs(ctx context.Context) ([]*ConfigEntry, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, config_key, scope, scope_id, type, value, number_value, bool_value,
		       json_value, default_value, description, category, display_name,
		       editable, required, validate_regexp, created_at, updated_at, updated_by, version
		FROM config_entries
		ORDER BY scope, scope_id, category, config_key`)
	if err != nil {
		return nil, fmt.Errorf("查询配置失败: %w", err)
	}
	defer rows.Close()

	return s.scanConfigEntries(rows)
}

// GetConfigByKeys 批量获取配置
func (s *MySQLConfigStorage) GetConfigByKeys(ctx context.Context, scope ConfigScope, scopeID string, keys []string) ([]*ConfigEntry, error) {
	if len(keys) == 0 {
		return []*ConfigEntry{}, nil
	}

	// 构建占位符和参数
	placeholders := ""
	args := make([]interface{}, 0, len(keys)+2)
	args = append(args, scope, scopeID)
	for i, key := range keys {
		if i > 0 {
			placeholders += ", "
		}
		placeholders += "?"
		args = append(args, key)
	}

	query := fmt.Sprintf(`
		SELECT id, config_key, scope, scope_id, type, value, number_value, bool_value,
		       json_value, default_value, description, category, display_name,
		       editable, required, validate_regexp, created_at, updated_at, updated_by, version
		FROM config_entries
		WHERE scope = ? AND scope_id = ? AND config_key IN (%s)
		ORDER BY config_key`, placeholders)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("批量查询配置失败: %w", err)
	}
	defer rows.Close()

	return s.scanConfigEntries(rows)
}

// SetConfig 设置配置
func (s *MySQLConfigStorage) SetConfig(ctx context.Context, entry *ConfigEntry, updatedBy string) error {
	// 验证配置
	if err := s.validateConfig(entry); err != nil {
		return err
	}

	// 检查是否已存在
	existing, err := s.GetConfig(ctx, entry.Scope, entry.ScopeID, entry.Key)
	if err != nil {
		return fmt.Errorf("检查配置是否存在失败: %w", err)
	}

	if existing != nil {
		// 更新现有配置
		return s.updateConfig(ctx, entry, existing, updatedBy)
	}

	// 插入新配置
	return s.insertConfig(ctx, entry, updatedBy)
}

// insertConfig 插入新配置
func (s *MySQLConfigStorage) insertConfig(ctx context.Context, entry *ConfigEntry, updatedBy string) error {
	query := `
	INSERT INTO config_entries (
		config_key, scope, scope_id, type, value, number_value, bool_value,
		json_value, default_value, description, category, display_name,
		editable, required, validate_regexp, updated_by
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	_, err := s.db.ExecContext(ctx, query,
		entry.Key, entry.Scope, entry.ScopeID, entry.Type, entry.Value,
		entry.NumberValue, entry.BoolValue, nullString(entry.JSONValue),
		nullString(entry.DefaultValue), nullString(entry.Description),
		nullString(entry.Category), nullString(entry.DisplayName),
		boolToInt(entry.Editable), boolToInt(entry.Required),
		nullString(entry.ValidateRegexp), updatedBy)

	if err != nil {
		return fmt.Errorf("插入配置失败: %w", err)
	}

	return nil
}

// updateConfig 更新现有配置
func (s *MySQLConfigStorage) updateConfig(ctx context.Context, newEntry *ConfigEntry, oldEntry *ConfigEntry, updatedBy string) error {
	query := `
	UPDATE config_entries
	SET type = ?, value = ?, number_value = ?, bool_value = ?,
	    json_value = ?, description = ?, display_name = ?,
	    editable = ?, required = ?, validate_regexp = ?,
	    updated_by = ?, version = version + 1
	WHERE id = ?`

	result, err := s.db.ExecContext(ctx, query,
		newEntry.Type, newEntry.Value, newEntry.NumberValue, newEntry.BoolValue,
		nullString(newEntry.JSONValue), nullString(newEntry.Description),
		nullString(newEntry.DisplayName),
		boolToInt(newEntry.Editable), boolToInt(newEntry.Required),
		nullString(newEntry.ValidateRegexp), updatedBy, oldEntry.ID)

	if err != nil {
		return fmt.Errorf("更新配置失败: %w", err)
	}

	// 保存历史
	if err := s.saveHistory(ctx, oldEntry.ID, oldEntry, newEntry, updatedBy, ""); err != nil {
		// 历史保存失败不影响主流程
		return fmt.Errorf("保存配置历史失败: %w", err)
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("未找到要更新的配置")
	}

	return nil
}

// SetConfigs 批量设置配置
func (s *MySQLConfigStorage) SetConfigs(ctx context.Context, entries []*ConfigEntry, updatedBy string) error {
	// 使用事务确保原子性
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("开始事务失败: %w", err)
	}
	defer tx.Rollback()

	for _, entry := range entries {
		if err := s.validateConfig(entry); err != nil {
			return fmt.Errorf("配置验证失败 [%s]: %w", entry.Key, err)
		}

		// 检查是否已存在
		existing, err := s.GetConfig(ctx, entry.Scope, entry.ScopeID, entry.Key)
		if err != nil {
			return fmt.Errorf("检查配置失败 [%s]: %w", entry.Key, err)
		}

		if existing != nil {
			// 更新
			query := `
			UPDATE config_entries
			SET type = ?, value = ?, number_value = ?, bool_value = ?,
			    json_value = ?, description = ?, display_name = ?,
			    editable = ?, required = ?, validate_regexp = ?,
			    updated_by = ?, version = version + 1
			WHERE id = ?`

			_, err = tx.ExecContext(ctx, query,
				entry.Type, entry.Value, entry.NumberValue, entry.BoolValue,
				nullString(entry.JSONValue), nullString(entry.Description),
				nullString(entry.DisplayName),
				boolToInt(entry.Editable), boolToInt(entry.Required),
				nullString(entry.ValidateRegexp), updatedBy, existing.ID)

			if err != nil {
				return fmt.Errorf("更新配置失败 [%s]: %w", entry.Key, err)
			}

			// 保存历史
			s.saveHistory(ctx, existing.ID, existing, entry, updatedBy, "")
		} else {
			// 插入
			query := `
			INSERT INTO config_entries (
				key, scope, scope_id, type, value, number_value, bool_value,
				json_value, default_value, description, category, display_name,
				editable, required, validate_regexp, updated_by
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

			_, err = tx.ExecContext(ctx, query,
				entry.Key, entry.Scope, entry.ScopeID, entry.Type, entry.Value,
				entry.NumberValue, entry.BoolValue, nullString(entry.JSONValue),
				nullString(entry.DefaultValue), nullString(entry.Description),
				nullString(entry.Category), nullString(entry.DisplayName),
				boolToInt(entry.Editable), boolToInt(entry.Required),
				nullString(entry.ValidateRegexp), updatedBy)

			if err != nil {
				return fmt.Errorf("插入配置失败 [%s]: %w", entry.Key, err)
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("提交事务失败: %w", err)
	}

	return nil
}

// DeleteConfig 删除配置
func (s *MySQLConfigStorage) DeleteConfig(ctx context.Context, scope ConfigScope, scopeID, key string) error {
	// 先获取旧配置用于历史
	oldEntry, err := s.GetConfig(ctx, scope, scopeID, key)
	if err != nil {
		return fmt.Errorf("查询配置失败: %w", err)
	}

	if oldEntry == nil {
		return fmt.Errorf("配置不存在")
	}

	// 删除配置
	_, err = s.db.ExecContext(ctx, "DELETE FROM config_entries WHERE config_key = ? AND scope = ? AND scope_id = ?", key, scope, scopeID)
	if err != nil {
		return fmt.Errorf("删除配置失败: %w", err)
	}

	// 保存历史
	return s.saveHistory(ctx, oldEntry.ID, oldEntry, nil, "system", "配置已删除")
}

// InitializeConfigs 初始化默认配置
func (s *MySQLConfigStorage) InitializeConfigs(ctx context.Context, entries []*ConfigEntry) error {
	// 使用事务确保原子性
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("开始事务失败: %w", err)
	}
	defer tx.Rollback()

	for _, entry := range entries {
		// 检查是否已存在
		var exists bool
		err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM config_entries WHERE config_key = ? AND scope = ? AND scope_id = ?",
			entry.Key, entry.Scope, entry.ScopeID).Scan(&exists)
		if err != nil {
			return fmt.Errorf("检查配置失败 [%s]: %w", entry.Key, err)
		}

		if !exists {
			// 插入新配置
			query := `
			INSERT INTO config_entries (
				config_key, scope, scope_id, type, value, number_value, bool_value,
				json_value, default_value, description, category, display_name,
				editable, required, validate_regexp, updated_by
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

			_, err = tx.ExecContext(ctx, query,
				entry.Key, entry.Scope, entry.ScopeID, entry.Type, entry.Value,
				entry.NumberValue, entry.BoolValue, nullString(entry.JSONValue),
				nullString(entry.DefaultValue), nullString(entry.Description),
				nullString(entry.Category), nullString(entry.DisplayName),
				boolToInt(entry.Editable), boolToInt(entry.Required),
				nullString(entry.ValidateRegexp), "system")

			if err != nil {
				return fmt.Errorf("插入配置失败 [%s]: %w", entry.Key, err)
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("提交事务失败: %w", err)
	}

	return nil
}

// GetConfigHistory 获取配置历史
func (s *MySQLConfigStorage) GetConfigHistory(ctx context.Context, configID int64, limit int) ([]*ConfigHistory, error) {
	query := `
	SELECT id, config_id, config_key, scope, scope_id, old_value, new_value,
	       changed_by, reason, changed_at
	FROM config_history
	WHERE config_id = ?
	ORDER BY changed_at DESC
	LIMIT ?`

	rows, err := s.db.QueryContext(ctx, query, configID, limit)
	if err != nil {
		return nil, fmt.Errorf("查询配置历史失败: %w", err)
	}
	defer rows.Close()

	var history []*ConfigHistory
	for rows.Next() {
		var h ConfigHistory
		var oldValue, newValue, reason sql.NullString

		err := rows.Scan(&h.ID, &h.ConfigID, &h.Key, &h.Scope, &h.ScopeID,
			&oldValue, &newValue, &h.ChangedBy, &reason, &h.ChangedAt)
		if err != nil {
			return nil, fmt.Errorf("扫描配置历史失败: %w", err)
		}

		if oldValue.Valid {
			h.OldValue = oldValue.String
		}
		if newValue.Valid {
			h.NewValue = newValue.String
		}
		if reason.Valid {
			h.Reason = reason.String
		}

		history = append(history, &h)
	}

	return history, nil
}

// GetConfigHistoryByKey 通过配置键获取历史
func (s *MySQLConfigStorage) GetConfigHistoryByKey(ctx context.Context, scope ConfigScope, scopeID, key string, limit int) ([]*ConfigHistory, error) {
	// 先获取配置ID
	var configID int64
	err := s.db.QueryRowContext(ctx, "SELECT id FROM config_entries WHERE config_key = ? AND scope = ? AND scope_id = ?",
		key, scope, scopeID).Scan(&configID)
	if err != nil {
		if err == sql.ErrNoRows {
			return []*ConfigHistory{}, nil
		}
		return nil, fmt.Errorf("查询配置ID失败: %w", err)
	}

	return s.GetConfigHistory(ctx, configID, limit)
}

// Close 关闭数据库连接
func (s *MySQLConfigStorage) Close() error {
	s.closed = true
	return s.db.Close()
}

// ValidateConfig 验证配置（公开方法）
func (s *MySQLConfigStorage) ValidateConfig(entry *ConfigEntry) error {
	return s.validateConfig(entry)
}

// validateConfig 验证配置（内部方法）
func (s *MySQLConfigStorage) validateConfig(entry *ConfigEntry) error {
	if entry.Key == "" {
		return fmt.Errorf("配置键不能为空")
	}
	if entry.Scope == "" {
		return fmt.Errorf("配置作用域不能为空")
	}
	if entry.Type == "" {
		return fmt.Errorf("配置类型不能为空")
	}

	// 验证必填字段
	if entry.Required {
		if entry.Value == "" && entry.JSONValue == "" {
			return fmt.Errorf("配置 %s 为必填项", entry.Key)
		}
	}

	// 正则验证
	if entry.ValidateRegexp != "" {
		matched, err := regexp.MatchString(entry.ValidateRegexp, entry.Value)
		if err != nil {
			return fmt.Errorf("正则表达式验证失败: %w", err)
		}
		if !matched {
			return fmt.Errorf("配置值不匹配正则表达式: %s", entry.ValidateRegexp)
		}
	}

	return nil
}

// saveHistory 保存配置变更历史
func (s *MySQLConfigStorage) saveHistory(ctx context.Context, configID int64, oldEntry, newEntry *ConfigEntry, changedBy, reason string) error {
	var oldValue, newValue string
	if oldEntry != nil {
		oldValue = oldEntry.Value
	}
	if newEntry != nil {
		newValue = newEntry.Value
	}

	query := `
	INSERT INTO config_history (config_id, config_key, scope, scope_id, old_value, new_value, changed_by, reason)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?)`

	_, err := s.db.ExecContext(ctx, query, configID, oldEntry.Key, oldEntry.Scope, oldEntry.ScopeID,
		oldValue, newValue, changedBy, reason)
	return err
}

// scanConfigEntries 扫描配置条目
func (s *MySQLConfigStorage) scanConfigEntries(rows *sql.Rows) ([]*ConfigEntry, error) {
	var entries []*ConfigEntry

	for rows.Next() {
		var entry ConfigEntry
		var jsonValue, defaultValue, description, category, displayName, validateRegexp, updatedBy sql.NullString
		var createdAt, updatedAt sql.NullTime
		var enabled, required int

		err := rows.Scan(&entry.ID, &entry.Key, &entry.Scope, &entry.ScopeID, &entry.Type,
			&entry.Value, &entry.NumberValue, &entry.BoolValue, &jsonValue, &defaultValue,
			&description, &category, &displayName, &enabled, &required, &validateRegexp,
			&createdAt, &updatedAt, &updatedBy, &entry.Version)
		if err != nil {
			return nil, fmt.Errorf("扫描配置行失败: %w", err)
		}

		// 处理可空字段
		if jsonValue.Valid {
			entry.JSONValue = jsonValue.String
		}
		if defaultValue.Valid {
			entry.DefaultValue = defaultValue.String
		}
		if description.Valid {
			entry.Description = description.String
		}
		if category.Valid {
			entry.Category = category.String
		}
		if displayName.Valid {
			entry.DisplayName = displayName.String
		}
		if validateRegexp.Valid {
			entry.ValidateRegexp = validateRegexp.String
		}
		if updatedBy.Valid {
			entry.UpdatedBy = updatedBy.String
		}
		if createdAt.Valid {
			entry.CreatedAt = createdAt.Time
		}
		if updatedAt.Valid {
			entry.UpdatedAt = updatedAt.Time
		}
		entry.Editable = enabled == 1
		entry.Required = required == 1

		entries = append(entries, &entry)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("遍历配置行失败: %w", err)
	}

	return entries, nil
}

// nullString 转换字符串为 sql.NullString
func nullString(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}

// boolToInt 转换布尔值为整数
func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
