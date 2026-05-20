package storage

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// GormConfigStorage 為配置中心提供跨 SQLite/PostgreSQL 的 GORM 實現。
// 目前主要用於 PostgreSQL/Supabase，避免手寫 SQL 佔位符與方言差異污染啟動鏈路。
type GormConfigStorage struct {
	db *gorm.DB
}

func NewGormConfigStorage(dbType, dsn string) (*GormConfigStorage, error) {
	dbType = strings.ToLower(strings.TrimSpace(dbType))
	if dbType == "postgresql" {
		dbType = "postgres"
	}
	if strings.TrimSpace(dsn) == "" {
		return nil, fmt.Errorf("%s 配置存储 DSN 不能为空", dbType)
	}

	var dialector gorm.Dialector
	switch dbType {
	case "postgres":
		dialector = postgres.Open(dsn)
	case "sqlite":
		dialector = sqlite.Open(dsn)
	default:
		return nil, fmt.Errorf("GORM 配置存储不支持数据库类型: %s", dbType)
	}

	db, err := gorm.Open(dialector, &gorm.Config{})
	if err != nil {
		return nil, fmt.Errorf("打开 %s 配置存储失败: %w", dbType, err)
	}
	if err := db.AutoMigrate(&ConfigEntry{}, &ConfigHistory{}); err != nil {
		return nil, fmt.Errorf("迁移 %s 配置表失败: %w", dbType, err)
	}
	return &GormConfigStorage{db: db}, nil
}

func (s *GormConfigStorage) GetConfig(ctx context.Context, scope ConfigScope, scopeID, key string) (*ConfigEntry, error) {
	var entry ConfigEntry
	err := s.db.WithContext(ctx).
		Where("key = ? AND scope = ? AND scope_id = ?", key, scope, scopeID).
		First(&entry).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("查询配置失败: %w", err)
	}
	return &entry, nil
}

func (s *GormConfigStorage) GetConfigByKeys(ctx context.Context, scope ConfigScope, scopeID string, keys []string) ([]*ConfigEntry, error) {
	if len(keys) == 0 {
		return nil, nil
	}
	var entries []*ConfigEntry
	err := s.db.WithContext(ctx).
		Where("scope = ? AND scope_id = ? AND key IN ?", scope, scopeID, keys).
		Order("category, key").
		Find(&entries).Error
	return entries, err
}

func (s *GormConfigStorage) GetConfigsByScope(ctx context.Context, scope ConfigScope, scopeID string) ([]*ConfigEntry, error) {
	var entries []*ConfigEntry
	err := s.db.WithContext(ctx).
		Where("scope = ? AND scope_id = ?", scope, scopeID).
		Order("category, key").
		Find(&entries).Error
	return entries, err
}

func (s *GormConfigStorage) GetConfigsByCategory(ctx context.Context, scope ConfigScope, category string) ([]*ConfigEntry, error) {
	var entries []*ConfigEntry
	err := s.db.WithContext(ctx).
		Where("scope = ? AND category = ?", scope, category).
		Order("key").
		Find(&entries).Error
	return entries, err
}

func (s *GormConfigStorage) GetAllConfigs(ctx context.Context) ([]*ConfigEntry, error) {
	var entries []*ConfigEntry
	err := s.db.WithContext(ctx).
		Order("scope, scope_id, category, key").
		Find(&entries).Error
	return entries, err
}

func (s *GormConfigStorage) SetConfig(ctx context.Context, entry *ConfigEntry, updatedBy string) error {
	if err := s.ValidateConfig(entry); err != nil {
		return err
	}
	now := time.Now()
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var old ConfigEntry
		err := tx.Where("key = ? AND scope = ? AND scope_id = ?", entry.Key, entry.Scope, entry.ScopeID).First(&old).Error
		if err != nil && err != gorm.ErrRecordNotFound {
			return fmt.Errorf("查询旧配置失败: %w", err)
		}

		isCreate := err == gorm.ErrRecordNotFound
		if isCreate {
			entry.CreatedAt = now
			entry.Version = 0
		} else {
			entry.ID = old.ID
			entry.CreatedAt = old.CreatedAt
			entry.Version = old.Version + 1
		}
		entry.UpdatedAt = now
		entry.UpdatedBy = updatedBy

		if err := tx.Save(entry).Error; err != nil {
			return fmt.Errorf("保存配置失败: %w", err)
		}

		history := ConfigHistory{
			ConfigID:  entry.ID,
			Key:       entry.Key,
			Scope:     entry.Scope,
			ScopeID:   entry.ScopeID,
			NewValue:  entry.Value,
			NewNumber: entry.NumberValue,
			NewBool:   entry.BoolValue,
			ChangedBy: updatedBy,
			ChangedAt: now,
		}
		if !isCreate {
			history.OldValue = old.Value
			history.OldNumber = old.NumberValue
			history.OldBool = old.BoolValue
		}
		if err := tx.Create(&history).Error; err != nil {
			return fmt.Errorf("记录配置历史失败: %w", err)
		}
		return nil
	})
}

func (s *GormConfigStorage) SetConfigs(ctx context.Context, entries []*ConfigEntry, updatedBy string) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		child := &GormConfigStorage{db: tx}
		for _, entry := range entries {
			if err := child.SetConfig(ctx, entry, updatedBy); err != nil {
				return fmt.Errorf("保存配置 %s 失败: %w", entry.Key, err)
			}
		}
		return nil
	})
}

func (s *GormConfigStorage) DeleteConfig(ctx context.Context, scope ConfigScope, scopeID, key string) error {
	return s.db.WithContext(ctx).
		Where("key = ? AND scope = ? AND scope_id = ?", key, scope, scopeID).
		Delete(&ConfigEntry{}).Error
}

func (s *GormConfigStorage) GetConfigHistory(ctx context.Context, configID int64, limit int) ([]*ConfigHistory, error) {
	var history []*ConfigHistory
	q := s.db.WithContext(ctx).Where("config_id = ?", configID).Order("changed_at DESC")
	if limit > 0 {
		q = q.Limit(limit)
	}
	err := q.Find(&history).Error
	return history, err
}

func (s *GormConfigStorage) GetConfigHistoryByKey(ctx context.Context, scope ConfigScope, scopeID, key string, limit int) ([]*ConfigHistory, error) {
	var history []*ConfigHistory
	q := s.db.WithContext(ctx).
		Where("key = ? AND scope = ? AND scope_id = ?", key, scope, scopeID).
		Order("changed_at DESC")
	if limit > 0 {
		q = q.Limit(limit)
	}
	err := q.Find(&history).Error
	return history, err
}

func (s *GormConfigStorage) InitializeConfigs(ctx context.Context, entries []*ConfigEntry) error {
	for _, entry := range entries {
		existing, err := s.GetConfig(ctx, entry.Scope, entry.ScopeID, entry.Key)
		if err != nil {
			return err
		}
		if existing != nil {
			continue
		}
		if err := s.SetConfig(ctx, entry, "system"); err != nil {
			return fmt.Errorf("初始化配置 %s 失败: %w", entry.Key, err)
		}
	}
	return nil
}

func (s *GormConfigStorage) ValidateConfig(entry *ConfigEntry) error {
	if entry == nil {
		return fmt.Errorf("配置不能为空")
	}
	if entry.Key == "" {
		return fmt.Errorf("配置键不能为空")
	}
	switch entry.Type {
	case TypeString, TypeNumber, TypeBoolean, TypeJSON, TypeArray:
	default:
		return fmt.Errorf("未知的配置类型: %s", entry.Type)
	}
	if entry.Required && entry.Value == "" && entry.JSONValue == "" {
		return fmt.Errorf("配置 %s 为必填项", entry.Key)
	}
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
