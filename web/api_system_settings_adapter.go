package web

import (
	"context"
	"encoding/json"
	"fmt"

	"quantmesh/storage"
)

// systemSettingsStorage 系统设置存储接口（*storage.SQLiteStorage 已实现）
type systemSettingsStorage interface {
	GetSystemSetting(ctx context.Context, key string) (*storage.SystemSetting, error)
	GetSystemSettings(ctx context.Context, filter *storage.SystemSettingFilter) ([]*storage.SystemSetting, error)
	SaveSystemSetting(ctx context.Context, setting *storage.SystemSetting) error
	DeleteSystemSetting(ctx context.Context, key string) error
	GetSystemSettingBool(ctx context.Context, key string, defaultValue bool) (bool, error)
	SetSystemSettingBool(ctx context.Context, key string, value bool) error
	GetSystemSettingString(ctx context.Context, key string, defaultValue string) (string, error)
	SetSystemSettingString(ctx context.Context, key, value string) error
	GetSystemSettingJSON(ctx context.Context, key string, target interface{}) error
	SetSystemSettingJSON(ctx context.Context, key string, value interface{}) error
}

// storageSystemSettingsAdapter 将 storage.Storage 适配为 SystemSettingsProvider
type storageSystemSettingsAdapter struct {
	storage storage.Storage
}

// NewStorageSystemSettingsAdapter 创建系统设置适配器，若 storage 不支持则返回 nil
func NewStorageSystemSettingsAdapter(s storage.Storage) SystemSettingsProvider {
	if s == nil {
		return nil
	}
	_, ok := s.(systemSettingsStorage)
	if !ok {
		return nil
	}
	return &storageSystemSettingsAdapter{storage: s}
}

func (a *storageSystemSettingsAdapter) getStore() systemSettingsStorage {
	return a.storage.(systemSettingsStorage)
}

func (a *storageSystemSettingsAdapter) GetSystemSettingBool(ctx context.Context, key string, defaultValue bool) (bool, error) {
	return a.getStore().GetSystemSettingBool(ctx, key, defaultValue)
}

func (a *storageSystemSettingsAdapter) GetSystemSettings(ctx context.Context, filter *storage.SystemSettingFilter) ([]*storage.SystemSetting, error) {
	return a.getStore().GetSystemSettings(ctx, filter)
}

func (a *storageSystemSettingsAdapter) GetSystemSetting(ctx context.Context, key string) (*storage.SystemSetting, error) {
	return a.getStore().GetSystemSetting(ctx, key)
}

func (a *storageSystemSettingsAdapter) SetSystemSettingBool(ctx context.Context, key string, value bool) error {
	return a.getStore().SetSystemSettingBool(ctx, key, value)
}

func (a *storageSystemSettingsAdapter) SetSystemSettingString(ctx context.Context, key, value string) error {
	return a.getStore().SetSystemSettingString(ctx, key, value)
}

func (a *storageSystemSettingsAdapter) SaveSystemSetting(ctx context.Context, key, value, settingType string) error {
	setting := &storage.SystemSetting{
		Key:   key,
		Value: value,
		Type:  settingType,
	}
	return a.getStore().SaveSystemSetting(ctx, setting)
}

func (a *storageSystemSettingsAdapter) DeleteSystemSetting(ctx context.Context, key string) error {
	return a.getStore().DeleteSystemSetting(ctx, key)
}

// GetSystemSettingJSON 获取 JSON 类型系统设置（SystemSettingsProvider 扩展，供 basis config 等使用）
func (a *storageSystemSettingsAdapter) GetSystemSettingJSON(ctx context.Context, key string, target interface{}) error {
	return a.getStore().GetSystemSettingJSON(ctx, key, target)
}

// SetSystemSettingJSON 设置 JSON 类型系统设置
func (a *storageSystemSettingsAdapter) SetSystemSettingJSON(ctx context.Context, key string, value interface{}) error {
	return a.getStore().SetSystemSettingJSON(ctx, key, value)
}

// GetSystemSettingJSONProvider 支持从 provider 读取 JSON 的接口
type GetSystemSettingJSONProvider interface {
	GetSystemSettingJSON(ctx context.Context, key string, target interface{}) error
}

// SetSystemSettingJSONProvider 支持向 provider 写入 JSON 的接口
type SetSystemSettingJSONProvider interface {
	SetSystemSettingJSON(ctx context.Context, key string, value interface{}) error
}

// GetSystemSettingJSONFromProvider 若 provider 支持 GetSystemSettingJSON 则调用，否则用 GetSystemSetting + json.Unmarshal
func GetSystemSettingJSONFromProvider(ctx context.Context, p SystemSettingsProvider, key string, target interface{}) error {
	if jsonProv, ok := p.(GetSystemSettingJSONProvider); ok {
		return jsonProv.GetSystemSettingJSON(ctx, key, target)
	}
	setting, err := p.GetSystemSetting(ctx, key)
	if err != nil {
		return err
	}
	if setting == nil {
		return fmt.Errorf("设置不存在: %s", key)
	}
	return json.Unmarshal([]byte(setting.Value), target)
}

// SetSystemSettingJSONToProvider 若 provider 支持 SetSystemSettingJSON 则调用，否则用 SaveSystemSetting
func SetSystemSettingJSONToProvider(ctx context.Context, p SystemSettingsProvider, key string, value interface{}) error {
	if jsonProv, ok := p.(SetSystemSettingJSONProvider); ok {
		return jsonProv.SetSystemSettingJSON(ctx, key, value)
	}
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return p.SaveSystemSetting(ctx, key, string(data), "json")
}
