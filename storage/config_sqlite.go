package storage

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// NewConfigStorageByType 根据数据库类型创建配置存储
func NewConfigStorageByType(dbType, dsn string) (ConfigStorage, error) {
	switch dbType {
	case "mysql":
		return NewMySQLConfigStorage(dsn)
	case "postgres", "postgresql":
		return NewGormConfigStorage(dbType, dsn)
	case "sqlite", "":
		return NewConfigStorage(dsn)
	default:
		return nil, fmt.Errorf("不支持的数据库类型: %s", dbType)
	}
}

// NewConfigStorage 创建 SQLite 配置存储
func NewConfigStorage(dbPath string) (ConfigStorage, error) {
	// 使用配置数据库路径
	configDBPath := dbPath
	if configDBPath == "" {
		configDBPath = "./data/config.db"
	}

	db, err := sql.Open("sqlite3", configDBPath+"?_journal_mode=WAL&_synchronous=NORMAL")
	if err != nil {
		return nil, fmt.Errorf("打开配置数据库失败: %w", err)
	}

	// 设置连接池
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	// 创建配置表
	if err := migrateConfigTables(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("创建配置表失败: %w", err)
	}

	return &SQLStorage{db: db}, nil
}

// GetConfig 获取单个配置
func (s *SQLStorage) GetConfig(ctx context.Context, scope ConfigScope, scopeID, key string) (*ConfigEntry, error) {
	var entry ConfigEntry
	var enabled, required int

	err := s.db.QueryRowContext(ctx, `
		SELECT id, key, scope, scope_id, type, value, number_value, bool_value,
		       json_value, default_value, description, category, display_name,
		       editable, required, validate_regexp, created_at, updated_at, updated_by, version
		FROM config_entries
		WHERE key = ? AND scope = ? AND scope_id = ?`,
		key, scope, scopeID).Scan(
		&entry.ID, &entry.Key, &entry.Scope, &entry.ScopeID, &entry.Type,
		&entry.Value, &entry.NumberValue, &entry.BoolValue, &entry.JSONValue,
		&entry.DefaultValue, &entry.Description, &entry.Category, &entry.DisplayName,
		&enabled, &required, &entry.ValidateRegexp, &entry.CreatedAt, &entry.UpdatedAt,
		&entry.UpdatedBy, &entry.Version,
	)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("查询配置失败: %w", err)
	}

	entry.Editable = enabled == 1
	entry.Required = required == 1

	return &entry, nil
}

// GetConfigByKeys 批量获取配置
func (s *SQLStorage) GetConfigByKeys(ctx context.Context, scope ConfigScope, scopeID string, keys []string) ([]*ConfigEntry, error) {
	if len(keys) == 0 {
		return nil, nil
	}

	query := `SELECT id, key, scope, scope_id, type, value, number_value, bool_value,
		       json_value, default_value, description, category, display_name,
		       editable, required, validate_regexp, created_at, updated_at, updated_by, version
		FROM config_entries
		WHERE scope = ? AND scope_id = ? AND key IN (`
	args := []interface{}{scope, scopeID}
	for i := range keys {
		if i > 0 {
			query += ","
		}
		query += "?"
		args = append(args, keys[i])
	}
	query += ")"

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("批量查询配置失败: %w", err)
	}
	defer rows.Close()

	var entries []*ConfigEntry
	for rows.Next() {
		var entry ConfigEntry
		var enabled, required int

		if err := rows.Scan(
			&entry.ID, &entry.Key, &entry.Scope, &entry.ScopeID, &entry.Type,
			&entry.Value, &entry.NumberValue, &entry.BoolValue, &entry.JSONValue,
			&entry.DefaultValue, &entry.Description, &entry.Category, &entry.DisplayName,
			&enabled, &required, &entry.ValidateRegexp, &entry.CreatedAt, &entry.UpdatedAt,
			&entry.UpdatedBy, &entry.Version,
		); err != nil {
			continue
		}

		entry.Editable = enabled == 1
		entry.Required = required == 1
		entries = append(entries, &entry)
	}

	return entries, rows.Err()
}

// GetConfigsByScope 按作用域获取配置
func (s *SQLStorage) GetConfigsByScope(ctx context.Context, scope ConfigScope, scopeID string) ([]*ConfigEntry, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, key, scope, scope_id, type, value, number_value, bool_value,
		       json_value, default_value, description, category, display_name,
		       editable, required, validate_regexp, created_at, updated_at, updated_by, version
		FROM config_entries
		WHERE scope = ? AND scope_id = ?
		ORDER BY category, key`,
		scope, scopeID)
	if err != nil {
		return nil, fmt.Errorf("按作用域查询配置失败: %w", err)
	}
	defer rows.Close()

	var entries []*ConfigEntry
	for rows.Next() {
		var entry ConfigEntry
		var enabled, required int

		if err := rows.Scan(
			&entry.ID, &entry.Key, &entry.Scope, &entry.ScopeID, &entry.Type,
			&entry.Value, &entry.NumberValue, &entry.BoolValue, &entry.JSONValue,
			&entry.DefaultValue, &entry.Description, &entry.Category, &entry.DisplayName,
			&enabled, &required, &entry.ValidateRegexp, &entry.CreatedAt, &entry.UpdatedAt,
			&entry.UpdatedBy, &entry.Version,
		); err != nil {
			continue
		}

		entry.Editable = enabled == 1
		entry.Required = required == 1
		entries = append(entries, &entry)
	}

	return entries, rows.Err()
}

// GetConfigsByCategory 按分类获取配置
func (s *SQLStorage) GetConfigsByCategory(ctx context.Context, scope ConfigScope, category string) ([]*ConfigEntry, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, key, scope, scope_id, type, value, number_value, bool_value,
		       json_value, default_value, description, category, display_name,
		       editable, required, validate_regexp, created_at, updated_at, updated_by, version
		FROM config_entries
		WHERE scope = ? AND category = ?
		ORDER BY key`,
		scope, category)
	if err != nil {
		return nil, fmt.Errorf("按分类查询配置失败: %w", err)
	}
	defer rows.Close()

	var entries []*ConfigEntry
	for rows.Next() {
		var entry ConfigEntry
		var enabled, required int

		if err := rows.Scan(
			&entry.ID, &entry.Key, &entry.Scope, &entry.ScopeID, &entry.Type,
			&entry.Value, &entry.NumberValue, &entry.BoolValue, &entry.JSONValue,
			&entry.DefaultValue, &entry.Description, &entry.Category, &entry.DisplayName,
			&enabled, &required, &entry.ValidateRegexp, &entry.CreatedAt, &entry.UpdatedAt,
			&entry.UpdatedBy, &entry.Version,
		); err != nil {
			continue
		}

		entry.Editable = enabled == 1
		entry.Required = required == 1
		entries = append(entries, &entry)
	}

	return entries, rows.Err()
}

// GetAllConfigs 获取所有配置
func (s *SQLStorage) GetAllConfigs(ctx context.Context) ([]*ConfigEntry, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, key, scope, scope_id, type, value, number_value, bool_value,
		       json_value, default_value, description, category, display_name,
		       editable, required, validate_regexp, created_at, updated_at, updated_by, version
		FROM config_entries
		ORDER BY scope, scope_id, category, key`)
	if err != nil {
		return nil, fmt.Errorf("查询所有配置失败: %w", err)
	}
	defer rows.Close()

	var entries []*ConfigEntry
	for rows.Next() {
		var entry ConfigEntry
		var enabled, required int

		if err := rows.Scan(
			&entry.ID, &entry.Key, &entry.Scope, &entry.ScopeID, &entry.Type,
			&entry.Value, &entry.NumberValue, &entry.BoolValue, &entry.JSONValue,
			&entry.DefaultValue, &entry.Description, &entry.Category, &entry.DisplayName,
			&enabled, &required, &entry.ValidateRegexp, &entry.CreatedAt, &entry.UpdatedAt,
			&entry.UpdatedBy, &entry.Version,
		); err != nil {
			continue
		}

		entry.Editable = enabled == 1
		entry.Required = required == 1
		entries = append(entries, &entry)
	}

	return entries, rows.Err()
}

// SetConfig 设置配置
func (s *SQLStorage) SetConfig(ctx context.Context, entry *ConfigEntry, updatedBy string) error {
	// 验证配置
	if err := s.ValidateConfig(entry); err != nil {
		return err
	}

	// 获取旧配置用于历史记录
	oldEntry, _ := s.GetConfig(ctx, entry.Scope, entry.ScopeID, entry.Key)

	now := time.Now()
	enabled := 0
	if entry.Editable {
		enabled = 1
	}
	required := 0
	if entry.Required {
		required = 1
	}

	var result sql.Result
	var err error

	if oldEntry != nil {
		// 更新现有配置
		result, err = s.db.ExecContext(ctx, `
			UPDATE config_entries
			SET type = ?, value = ?, number_value = ?, bool_value = ?, json_value = ?,
			    description = ?, category = ?, display_name = ?, editable = ?, required = ?,
			    validate_regexp = ?, updated_at = ?, updated_by = ?, version = version + 1
			WHERE key = ? AND scope = ? AND scope_id = ?`,
			entry.Type, entry.Value, entry.NumberValue, entry.BoolValue, entry.JSONValue,
			entry.Description, entry.Category, entry.DisplayName, enabled, required,
			entry.ValidateRegexp, now.Format(time.RFC3339), updatedBy,
			entry.Key, entry.Scope, entry.ScopeID)
	} else {
		// 插入新配置
		result, err = s.db.ExecContext(ctx, `
			INSERT INTO config_entries
			(key, scope, scope_id, type, value, number_value, bool_value, json_value,
			 default_value, description, category, display_name, editable, required,
			 validate_regexp, created_at, updated_at, updated_by, version)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0)`,
			entry.Key, entry.Scope, entry.ScopeID, entry.Type, entry.Value,
			entry.NumberValue, entry.BoolValue, entry.JSONValue,
			entry.DefaultValue, entry.Description, entry.Category, entry.DisplayName,
			enabled, required, entry.ValidateRegexp,
			now.Format(time.RFC3339), now.Format(time.RFC3339), updatedBy)
	}

	if err != nil {
		return fmt.Errorf("保存配置失败: %w", err)
	}

	// 获取配置ID
	configID, _ := result.LastInsertId()

	// 记录历史
	if oldEntry != nil {
		_, err = s.db.ExecContext(ctx, `
			INSERT INTO config_history
			(config_id, key, scope, scope_id, old_value, new_value,
			 old_number, new_number, old_bool, new_bool, changed_by, changed_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			configID, entry.Key, entry.Scope, entry.ScopeID,
			oldEntry.Value, entry.Value,
			oldEntry.NumberValue, entry.NumberValue,
			oldEntry.BoolValue, entry.BoolValue,
			updatedBy, now.Format(time.RFC3339))
	} else {
		_, err = s.db.ExecContext(ctx, `
			INSERT INTO config_history
			(config_id, key, scope, scope_id, new_value, new_number, new_bool, changed_by, changed_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			configID, entry.Key, entry.Scope, entry.ScopeID,
			entry.Value, entry.NumberValue, entry.BoolValue,
			updatedBy, now.Format(time.RFC3339))
	}

	if err != nil {
		// 历史记录失败不影响主流程
		return fmt.Errorf("记录配置历史失败: %w", err)
	}

	return nil
}

// SetConfigs 批量设置配置
func (s *SQLStorage) SetConfigs(ctx context.Context, entries []*ConfigEntry, updatedBy string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("开始事务失败: %w", err)
	}
	defer tx.Rollback()

	for _, entry := range entries {
		if err := s.ValidateConfig(entry); err != nil {
			return fmt.Errorf("配置 %s 验证失败: %w", entry.Key, err)
		}

		oldEntry, _ := s.GetConfig(ctx, entry.Scope, entry.ScopeID, entry.Key)

		now := time.Now()
		enabled := 0
		if entry.Editable {
			enabled = 1
		}
		required := 0
		if entry.Required {
			required = 1
		}

		var result sql.Result

		if oldEntry != nil {
			result, err = tx.Exec(`
				UPDATE config_entries
				SET type = ?, value = ?, number_value = ?, bool_value = ?, json_value = ?,
				    description = ?, category = ?, display_name = ?, editable = ?, required = ?,
				    validate_regexp = ?, updated_at = ?, updated_by = ?, version = version + 1
				WHERE key = ? AND scope = ? AND scope_id = ?`,
				entry.Type, entry.Value, entry.NumberValue, entry.BoolValue, entry.JSONValue,
				entry.Description, entry.Category, entry.DisplayName, enabled, required,
				entry.ValidateRegexp, now.Format(time.RFC3339), updatedBy,
				entry.Key, entry.Scope, entry.ScopeID)
		} else {
			result, err = tx.Exec(`
				INSERT INTO config_entries
				(key, scope, scope_id, type, value, number_value, bool_value, json_value,
				 default_value, description, category, display_name, editable, required,
				 validate_regexp, created_at, updated_at, updated_by, version)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0)`,
				entry.Key, entry.Scope, entry.ScopeID, entry.Type, entry.Value,
				entry.NumberValue, entry.BoolValue, entry.JSONValue,
				entry.DefaultValue, entry.Description, entry.Category, entry.DisplayName,
				enabled, required, entry.ValidateRegexp,
				now.Format(time.RFC3339), now.Format(time.RFC3339), updatedBy)
		}

		if err != nil {
			return fmt.Errorf("保存配置 %s 失败: %w", entry.Key, err)
		}

		configID, _ := result.LastInsertId()

		// 记录历史（简化处理，实际项目中可能需要更复杂的事务处理）
		if oldEntry != nil {
			tx.Exec(`
				INSERT INTO config_history
				(config_id, key, scope, scope_id, old_value, new_value,
				 old_number, new_number, old_bool, new_bool, changed_by, changed_at)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				configID, entry.Key, entry.Scope, entry.ScopeID,
				oldEntry.Value, entry.Value,
				oldEntry.NumberValue, entry.NumberValue,
				oldEntry.BoolValue, entry.BoolValue,
				updatedBy, now.Format(time.RFC3339))
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("提交事务失败: %w", err)
	}

	return nil
}

// DeleteConfig 删除配置（恢复默认值）
func (s *SQLStorage) DeleteConfig(ctx context.Context, scope ConfigScope, scopeID, key string) error {
	_, err := s.db.ExecContext(ctx, `
		DELETE FROM config_entries
		WHERE key = ? AND scope = ? AND scope_id = ?`,
		key, scope, scopeID)

	if err != nil {
		return fmt.Errorf("删除配置失败: %w", err)
	}
	return nil
}

// GetConfigHistory 获取配置历史
func (s *SQLStorage) GetConfigHistory(ctx context.Context, configID int64, limit int) ([]*ConfigHistory, error) {
	query := `
		SELECT id, config_id, key, scope, scope_id, old_value, new_value,
		       old_number, new_number, old_bool, new_bool, reason, changed_by, changed_at
		FROM config_history
		WHERE config_id = ?
		ORDER BY changed_at DESC`

	if limit > 0 {
		query += " LIMIT " + fmt.Sprint(limit)
	}

	rows, err := s.db.QueryContext(ctx, query, configID)
	if err != nil {
		return nil, fmt.Errorf("查询配置历史失败: %w", err)
	}
	defer rows.Close()

	var history []*ConfigHistory
	for rows.Next() {
		var h ConfigHistory
		var oldBool, newBool int

		if err := rows.Scan(
			&h.ID, &h.ConfigID, &h.Key, &h.Scope, &h.ScopeID,
			&h.OldValue, &h.NewValue,
			&h.OldNumber, &h.NewNumber, &oldBool, &newBool,
			&h.Reason, &h.ChangedBy, &h.ChangedAt,
		); err != nil {
			continue
		}

		h.OldBool = oldBool == 1
		h.NewBool = newBool == 1
		history = append(history, &h)
	}

	return history, rows.Err()
}

// GetConfigHistoryByKey 按键获取配置历史
func (s *SQLStorage) GetConfigHistoryByKey(ctx context.Context, scope ConfigScope, scopeID, key string, limit int) ([]*ConfigHistory, error) {
	query := `
		SELECT id, config_id, key, scope, scope_id, old_value, new_value,
		       old_number, new_number, old_bool, new_bool, reason, changed_by, changed_at
		FROM config_history
		WHERE key = ? AND scope = ? AND scope_id = ?
		ORDER BY changed_at DESC`

	if limit > 0 {
		query += " LIMIT " + fmt.Sprint(limit)
	}

	rows, err := s.db.QueryContext(ctx, query, key, scope, scopeID)
	if err != nil {
		return nil, fmt.Errorf("按键查询配置历史失败: %w", err)
	}
	defer rows.Close()

	var history []*ConfigHistory
	for rows.Next() {
		var h ConfigHistory
		var oldBool, newBool int

		if err := rows.Scan(
			&h.ID, &h.ConfigID, &h.Key, &h.Scope, &h.ScopeID,
			&h.OldValue, &h.NewValue,
			&h.OldNumber, &h.NewNumber, &oldBool, &newBool,
			&h.Reason, &h.ChangedBy, &h.ChangedAt,
		); err != nil {
			continue
		}

		h.OldBool = oldBool == 1
		h.NewBool = newBool == 1
		history = append(history, &h)
	}

	return history, rows.Err()
}

// InitializeConfigs 批量初始化配置
func (s *SQLStorage) InitializeConfigs(ctx context.Context, entries []*ConfigEntry) error {
	for _, entry := range entries {
		// 检查是否已存在
		existing, _ := s.GetConfig(ctx, entry.Scope, entry.ScopeID, entry.Key)
		if existing != nil {
			continue // 跳过已存在的配置
		}

		if err := s.SetConfig(ctx, entry, "system"); err != nil {
			return fmt.Errorf("初始化配置 %s 失败: %w", entry.Key, err)
		}
	}
	return nil
}

// ValidateConfig 验证配置
func (s *SQLStorage) ValidateConfig(entry *ConfigEntry) error {
	// 基本验证
	if entry.Key == "" {
		return fmt.Errorf("配置键不能为空")
	}

	// 类型验证
	switch entry.Type {
	case TypeString, TypeNumber, TypeBoolean, TypeJSON, TypeArray:
		// 有效类型
	default:
		return fmt.Errorf("未知的配置类型: %s", entry.Type)
	}

	// 必填验证
	if entry.Required && entry.Value == "" && entry.JSONValue == "" {
		return fmt.Errorf("配置 %s 为必填项", entry.Key)
	}

	// 正则验证
	if entry.ValidateRegexp != "" && entry.Value != "" {
		matched, err := regexp.MatchString(entry.ValidateRegexp, entry.Value)
		if err != nil {
			return fmt.Errorf("正则表达式错误: %w", err)
		}
		if !matched {
			return fmt.Errorf("配置值不符合正则表达式: %s", entry.ValidateRegexp)
		}
	}

	return nil
}
