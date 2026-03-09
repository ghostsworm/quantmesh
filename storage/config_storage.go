package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// ConfigScope 配置作用域
type ConfigScope string

const (
	ScopeGlobal  ConfigScope = "global"  // 全局配置
	ScopeBot     ConfigScope = "bot"     // 机器人配置
	ScopeStrategy ConfigScope = "strategy" // 策略配置
	ScopeExchange ConfigScope = "exchange" // 交易所配置
)

// ConfigType 配置值类型
type ConfigType string

const (
	TypeString  ConfigType = "string"
	TypeNumber  ConfigType = "number"
	TypeBoolean ConfigType = "boolean"
	TypeJSON    ConfigType = "json"
	TypeArray   ConfigType = "array"
)

// ConfigEntry 数据库配置条目
type ConfigEntry struct {
	ID          int64      `gorm:"primaryKey;autoIncrement" json:"id"`
	Key         string     `gorm:"uniqueIndex:idx_key_scope;size:200" json:"key"`          // 配置键，如 "notifications.enabled"
	Scope       ConfigScope `gorm:"uniqueIndex:idx_key_scope;size:20" json:"scope"`        // 作用域
	ScopeID     string     `gorm:"uniqueIndex:idx_key_scope;size:100" json:"scope_id"`     // 作用域ID，如机器人ID
	Type        ConfigType `gorm:"size:20" json:"type"`                                   // 值类型
	Value       string     `gorm:"type:text" json:"value"`                                // 字符串值
	NumberValue float64    `json:"number_value,omitempty"`                                // 数值（用于快速查询）
	BoolValue   bool       `json:"bool_value,omitempty"`                                  // 布尔值（用于快速查询）
	JSONValue   string     `gorm:"type:text" json:"json_value,omitempty"`                 // JSON值
	DefaultValue string    `gorm:"type:text" json:"default_value,omitempty"`              // 默认值（从配置文件读取）
	Description string     `gorm:"type:text" json:"description,omitempty"`                // 配置描述
	Category    string     `gorm:"size:100" json:"category,omitempty"`                    // 分类，如 "notifications", "trading"
	DisplayName string     `gorm:"size:200" json:"display_name,omitempty"`                 // 显示名称
	Editable    bool       `gorm:"default:true" json:"editable"`                           // 是否可编辑
	Required    bool       `gorm:"default:false" json:"required"`                          // 是否必填
	ValidateRegexp string   `gorm:"size:200" json:"validate_regexp,omitempty"`              // 验证正则表达式
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	UpdatedBy   string     `gorm:"size:100" json:"updated_by"`                             // 更新人
	Version     int64      `gorm:"default:0" json:"version"`                               // 版本号（用于乐观锁）
}

// TableName 指定表名
func (ConfigEntry) TableName() string {
	return "config_entries"
}

// ConfigHistory 配置变更历史
type ConfigHistory struct {
	ID        int64      `gorm:"primaryKey;autoIncrement" json:"id"`
	ConfigID  int64      `gorm:"index:idx_config_id" json:"config_id"`
	Key       string     `gorm:"size:200" json:"key"`
	Scope     ConfigScope `gorm:"size:20" json:"scope"`
	ScopeID   string     `gorm:"size:100" json:"scope_id"`
	OldValue  string     `gorm:"type:text" json:"old_value"`
	NewValue  string     `gorm:"type:text" json:"new_value"`
	OldNumber float64    `json:"old_number,omitempty"`
	NewNumber float64    `json:"new_number,omitempty"`
	OldBool   bool       `json:"old_bool,omitempty"`
	NewBool   bool       `json:"new_bool,omitempty"`
	Reason    string     `gorm:"type:text" json:"reason,omitempty"`  // 变更原因
	ChangedBy string     `gorm:"size:100" json:"changed_by"`         // 变更人
	ChangedAt time.Time  `json:"changed_at"`
}

// TableName 指定表名
func (ConfigHistory) TableName() string {
	return "config_history"
}

// ConfigStorage 配置存储接口
type ConfigStorage interface {
	// 获取配置值
	GetConfig(ctx context.Context, scope ConfigScope, scopeID, key string) (*ConfigEntry, error)
	GetConfigByKeys(ctx context.Context, scope ConfigScope, scopeID string, keys []string) ([]*ConfigEntry, error)
	GetConfigsByScope(ctx context.Context, scope ConfigScope, scopeID string) ([]*ConfigEntry, error)
	GetConfigsByCategory(ctx context.Context, scope ConfigScope, category string) ([]*ConfigEntry, error)
	GetAllConfigs(ctx context.Context) ([]*ConfigEntry, error)

	// 设置配置值
	SetConfig(ctx context.Context, entry *ConfigEntry, updatedBy string) error
	SetConfigs(ctx context.Context, entries []*ConfigEntry, updatedBy string) error

	// 删除配置（恢复默认值）
	DeleteConfig(ctx context.Context, scope ConfigScope, scopeID, key string) error

	// 获取配置历史
	GetConfigHistory(ctx context.Context, configID int64, limit int) ([]*ConfigHistory, error)
	GetConfigHistoryByKey(ctx context.Context, scope ConfigScope, scopeID, key string, limit int) ([]*ConfigHistory, error)

	// 批量初始化配置（从配置文件导入）
	InitializeConfigs(ctx context.Context, entries []*ConfigEntry) error

	// 配置验证
	ValidateConfig(entry *ConfigEntry) error
}

// GetTypedValue 获取类型化的值
func (e *ConfigEntry) GetTypedValue() (interface{}, error) {
	switch e.Type {
	case TypeString:
		return e.Value, nil
	case TypeNumber:
		return e.NumberValue, nil
	case TypeBoolean:
		return e.BoolValue, nil
	case TypeJSON:
		var result interface{}
		if err := json.Unmarshal([]byte(e.JSONValue), &result); err != nil {
			return nil, fmt.Errorf("解析 JSON 失败: %w", err)
		}
		return result, nil
	case TypeArray:
		var result []interface{}
		if err := json.Unmarshal([]byte(e.JSONValue), &result); err != nil {
			return nil, fmt.Errorf("解析数组失败: %w", err)
		}
		return result, nil
	default:
		return nil, fmt.Errorf("未知的配置类型: %s", e.Type)
	}
}

// SetTypedValue 设置类型化的值
func (e *ConfigEntry) SetTypedValue(value interface{}) error {
	switch v := value.(type) {
	case string:
		e.Type = TypeString
		e.Value = v
	case int, int32, int64, float32, float64:
		e.Type = TypeNumber
		switch val := v.(type) {
		case int:
			e.NumberValue = float64(val)
		case int32:
			e.NumberValue = float64(val)
		case int64:
			e.NumberValue = float64(val)
		case float32:
			e.NumberValue = float64(val)
		case float64:
			e.NumberValue = val
		}
		e.Value = fmt.Sprintf("%v", v)
	case bool:
		e.Type = TypeBoolean
		e.BoolValue = v
		e.Value = fmt.Sprintf("%v", v)
	default:
		// 尝试序列化为 JSON
		jsonBytes, err := json.Marshal(value)
		if err != nil {
			return fmt.Errorf("无法序列化配置值: %w", err)
		}
		e.JSONValue = string(jsonBytes)
		e.Value = string(jsonBytes)

		// 判断是 JSON 对象还是数组
		if _, ok := value.(map[string]interface{}); ok {
			e.Type = TypeJSON
		} else if _, ok := value.([]interface{}); ok {
			e.Type = TypeArray
		} else {
			e.Type = TypeJSON
		}
	}
	return nil
}

// NewConfigEntry 创建新的配置条目
func NewConfigEntry(scope ConfigScope, scopeID, key string, value interface{}, category, displayName, description string) (*ConfigEntry, error) {
	entry := &ConfigEntry{
		Key:         key,
		Scope:       scope,
		ScopeID:     scopeID,
		Category:    category,
		DisplayName: displayName,
		Description: description,
		Editable:    true,
	}
	if err := entry.SetTypedValue(value); err != nil {
		return nil, err
	}
	return entry, nil
}
