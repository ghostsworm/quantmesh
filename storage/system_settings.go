package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// SystemSetting 系统设置模型
type SystemSetting struct {
	ID        int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	Key       string    `gorm:"uniqueIndex;size:100" json:"key"`
	Value     string    `gorm:"type:text" json:"value"`
	Type      string    `gorm:"size:20" json:"type"` // string, number, boolean, json
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// SystemSettingFilter 系统设置过滤器
type SystemSettingFilter struct {
	Key string
}

func (s *SQLStorage) systemSettingsSelectColumns() string {
	if s != nil && s.dbType == "mysql" {
		return "id, `key`, `value`, `type`, created_at, updated_at"
	}
	return "id, key, value, type, created_at, updated_at"
}

func (s *SQLStorage) systemSettingsKeyColumn() string {
	return s.mysqlQuoteIdent("key")
}

// GetSystemSettings 获取系统设置
func (s *SQLStorage) GetSystemSettings(ctx context.Context, filter *SystemSettingFilter) ([]*SystemSetting, error) {
	query := "SELECT " + s.systemSettingsSelectColumns() + " FROM system_settings"
	args := []interface{}{}

	if filter != nil && filter.Key != "" {
		query += " WHERE " + s.systemSettingsKeyColumn() + " = ?"
		args = append(args, filter.Key)
	}

	query += " ORDER BY " + s.systemSettingsKeyColumn()

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		if err == sql.ErrNoRows {
			return []*SystemSetting{}, nil
		}
		return nil, err
	}
	defer rows.Close()

	var settings []*SystemSetting
	for rows.Next() {
		var setting SystemSetting
		err := rows.Scan(&setting.ID, &setting.Key, &setting.Value, &setting.Type, &setting.CreatedAt, &setting.UpdatedAt)
		if err != nil {
			return nil, err
		}
		settings = append(settings, &setting)
	}

	return settings, nil
}

// GetSystemSetting 获取单个系统设置
func (s *SQLStorage) GetSystemSetting(ctx context.Context, key string) (*SystemSetting, error) {
	query := "SELECT " + s.systemSettingsSelectColumns() + " FROM system_settings WHERE " + s.systemSettingsKeyColumn() + " = ?"

	var setting SystemSetting
	err := s.db.QueryRowContext(ctx, query, key).Scan(
		&setting.ID, &setting.Key, &setting.Value, &setting.Type, &setting.CreatedAt, &setting.UpdatedAt,
	)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	return &setting, nil
}

// SaveSystemSetting 保存系统设置
func (s *SQLStorage) SaveSystemSetting(ctx context.Context, setting *SystemSetting) error {
	now := time.Now()

	// 检查是否已存在
	existing, err := s.GetSystemSetting(ctx, setting.Key)
	if err != nil {
		return fmt.Errorf("检查设置失败: %w", err)
	}

	if existing != nil {
		// 更新（MySQL 中 value/type/key 可能與保留字衝突）
		var query string
		if s.dbType == "mysql" {
			query = fmt.Sprintf(
				"UPDATE system_settings SET %s = ?, %s = ?, updated_at = ? WHERE %s = ?",
				s.mysqlQuoteIdent("value"), s.mysqlQuoteIdent("type"), s.systemSettingsKeyColumn(),
			)
		} else {
			query = `UPDATE system_settings SET value = ?, type = ?, updated_at = ? WHERE key = ?`
		}
		_, err = s.db.ExecContext(ctx, query, setting.Value, setting.Type, now, setting.Key)
		if err != nil {
			return fmt.Errorf("更新设置失败: %w", err)
		}
		setting.ID = existing.ID
		setting.CreatedAt = existing.CreatedAt
		setting.UpdatedAt = now
	} else {
		// 插入
		setting.CreatedAt = now
		setting.UpdatedAt = now
		var query string
		if s.dbType == "mysql" {
			query = fmt.Sprintf(
				"INSERT INTO system_settings (%s, %s, %s, created_at, updated_at) VALUES (?, ?, ?, ?, ?)",
				s.mysqlQuoteIdent("key"), s.mysqlQuoteIdent("value"), s.mysqlQuoteIdent("type"),
			)
		} else {
			query = `INSERT INTO system_settings (key, value, type, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`
		}
		result, err := s.db.ExecContext(ctx, query, setting.Key, setting.Value, setting.Type, setting.CreatedAt, setting.UpdatedAt)
		if err != nil {
			return fmt.Errorf("插入设置失败: %w", err)
		}
		id, _ := result.LastInsertId()
		setting.ID = id
	}

	return nil
}

// DeleteSystemSetting 删除系统设置
func (s *SQLStorage) DeleteSystemSetting(ctx context.Context, key string) error {
	query := "DELETE FROM system_settings WHERE " + s.systemSettingsKeyColumn() + " = ?"
	_, err := s.db.ExecContext(ctx, query, key)
	if err != nil {
		return fmt.Errorf("删除设置失败: %w", err)
	}
	return nil
}

// GetSystemSettingBool 获取布尔类型的系统设置
func (s *SQLStorage) GetSystemSettingBool(ctx context.Context, key string, defaultValue bool) (bool, error) {
	setting, err := s.GetSystemSetting(ctx, key)
	if err != nil {
		return defaultValue, err
	}
	if setting == nil {
		return defaultValue, nil
	}

	if setting.Type == "boolean" {
		return setting.Value == "true", nil
	}

	// 尝试解析 JSON
	var value bool
	if err := json.Unmarshal([]byte(setting.Value), &value); err != nil {
		return defaultValue, fmt.Errorf("解析布尔值失败: %w", err)
	}
	return value, nil
}

// GetSystemSettingString 获取字符串类型的系统设置
func (s *SQLStorage) GetSystemSettingString(ctx context.Context, key string, defaultValue string) (string, error) {
	setting, err := s.GetSystemSetting(ctx, key)
	if err != nil {
		return defaultValue, err
	}
	if setting == nil {
		return defaultValue, nil
	}

	if setting.Type == "string" {
		return setting.Value, nil
	}

	return defaultValue, nil
}

// GetSystemSettingJSON 获取 JSON 类型的系统设置
func (s *SQLStorage) GetSystemSettingJSON(ctx context.Context, key string, target interface{}) error {
	setting, err := s.GetSystemSetting(ctx, key)
	if err != nil {
		return err
	}
	if setting == nil {
		return fmt.Errorf("设置不存在: %s", key)
	}

	if setting.Type != "json" {
		return fmt.Errorf("设置类型不是 JSON: %s", key)
	}

	return json.Unmarshal([]byte(setting.Value), target)
}

// SetSystemSettingBool 设置布尔类型的系统设置
func (s *SQLStorage) SetSystemSettingBool(ctx context.Context, key string, value bool) error {
	setting := &SystemSetting{
		Key:   key,
		Value: fmt.Sprintf("%v", value),
		Type:  "boolean",
	}
	return s.SaveSystemSetting(ctx, setting)
}

// SetSystemSettingString 设置字符串类型的系统设置
func (s *SQLStorage) SetSystemSettingString(ctx context.Context, key, value string) error {
	setting := &SystemSetting{
		Key:   key,
		Value: value,
		Type:  "string",
	}
	return s.SaveSystemSetting(ctx, setting)
}

// SetSystemSettingJSON 设置 JSON 类型的系统设置
func (s *SQLStorage) SetSystemSettingJSON(ctx context.Context, key string, value interface{}) error {
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("序列化 JSON 失败: %w", err)
	}

	setting := &SystemSetting{
		Key:   key,
		Value: string(data),
		Type:  "json",
	}
	return s.SaveSystemSetting(ctx, setting)
}

// migrateSystemSettingsTable 创建或迁移系统设置表
func migrateSystemSettingsTable(db *sql.DB) error {
	query := `
	CREATE TABLE IF NOT EXISTS system_settings (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		key TEXT UNIQUE NOT NULL,
		value TEXT,
		type TEXT DEFAULT 'string',
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);

	CREATE INDEX IF NOT EXISTS idx_system_settings_key ON system_settings(key);
	`

	_, err := db.Exec(query)
	if err != nil {
		return fmt.Errorf("创建 system_settings 表失败: %w", err)
	}

	return nil
}
