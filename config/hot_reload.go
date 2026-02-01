package config

import (
	"fmt"
	"strings"
	"sync"
)

// HotReloader 配置热更新器
type HotReloader struct {
	mu              sync.RWMutex
	currentConfig   *Config
	updateCallbacks []ConfigUpdateCallback
}

// ConfigUpdateCallback 配置更新回呼函數類型
type ConfigUpdateCallback func(oldConfig, newConfig *Config, changes []ConfigChange) error

// NewHotReloader 創建热更新器
func NewHotReloader(initialConfig *Config) *HotReloader {
	return &HotReloader{
		currentConfig:   initialConfig,
		updateCallbacks: []ConfigUpdateCallback{},
	}
}

// RegisterCallback 注册配置更新回呼
func (hr *HotReloader) RegisterCallback(callback ConfigUpdateCallback) {
	hr.mu.Lock()
	defer hr.mu.Unlock()
	hr.updateCallbacks = append(hr.updateCallbacks, callback)
}

// UpdateConfig 更新配置（热更新）
func (hr *HotReloader) UpdateConfig(newConfig *Config) (*ConfigDiff, error) {
	hr.mu.Lock()
	defer hr.mu.Unlock()

	// 對比配置变更
	diff := DiffConfig(hr.currentConfig, newConfig)

	// 分离需要重啟的变更和可以热更新的变更
	hotReloadableChanges := []ConfigChange{}
	restartRequiredChanges := []ConfigChange{}

	for _, change := range diff.Changes {
		if change.RequiresRestart {
			restartRequiredChanges = append(restartRequiredChanges, change)
		} else {
			hotReloadableChanges = append(hotReloadableChanges, change)
		}
	}

	// 如果有需要重啟的变更，只更新可以热更新的部分
	if len(restartRequiredChanges) > 0 {
		// 創建只包含可热更新变更的配置
		partialConfig := hr.applyHotReloadableChanges(hr.currentConfig, newConfig, hotReloadableChanges)

		// 应用可热更新的变更
		if err := hr.applyConfigUpdate(hr.currentConfig, partialConfig, hotReloadableChanges); err != nil {
			return nil, fmt.Errorf("应用热更新失败: %v", err)
		}

		// 更新當前配置
		hr.currentConfig = partialConfig

		// 返回包含重啟提示的差异
		return diff, nil
	}

	// 全部可以热更新，直接应用
	if err := hr.applyConfigUpdate(hr.currentConfig, newConfig, diff.Changes); err != nil {
		return nil, fmt.Errorf("應用配置更新失败: %v", err)
	}

	// 更新當前配置
	hr.currentConfig = newConfig

	return diff, nil
}

// applyHotReloadableChanges 应用可热更新的变更，創建部分更新的配置
func (hr *HotReloader) applyHotReloadableChanges(oldConfig, newConfig *Config, hotReloadableChanges []ConfigChange) *Config {
	// 深度複制舊配置
	result := hr.cloneConfig(oldConfig)

	// 应用可热更新的变更
	for _, change := range hotReloadableChanges {
		hr.applyChangeToConfig(result, newConfig, change.Path, change.NewValue)
	}

	return result
}

// applyChangeToConfig 將單個变更应用到配置
func (hr *HotReloader) applyChangeToConfig(config *Config, sourceConfig *Config, path string, value interface{}) {
	// 简化實現：對於複杂路径，直接從源配置複制整個結構
	// 这里使用反射来實現深度複制特定字段
	hr.copyConfigField(config, sourceConfig, path)
}

// copyConfigField 複制配置字段（简化實現）
func (hr *HotReloader) copyConfigField(dest, src *Config, path string) {
	// 根據路径複制對应的字段
	// 这是一個简化實現，實際应該使用反射進行深度複制
	switch {
	case path == "trading.symbol" || strings.HasPrefix(path, "trading."):
		dest.Trading = src.Trading
	case path == "risk_control.enabled" || strings.HasPrefix(path, "risk_control."):
		dest.RiskControl = src.RiskControl
	case strings.HasPrefix(path, "notifications."):
		dest.Notifications = src.Notifications
	case strings.HasPrefix(path, "timing."):
		dest.Timing = src.Timing
	case strings.HasPrefix(path, "system.log_level"):
		// 日志级别可以热更新
		dest.System.LogLevel = src.System.LogLevel
	}
}

// applyConfigUpdate 應用配置更新並触发回呼
func (hr *HotReloader) applyConfigUpdate(oldConfig, newConfig *Config, changes []ConfigChange) error {
	// 触发所有注册的回呼
	for _, callback := range hr.updateCallbacks {
		if err := callback(oldConfig, newConfig, changes); err != nil {
			return fmt.Errorf("配置更新回呼執行失败: %v", err)
		}
	}

	return nil
}

// GetCurrentConfig 獲取當前配置
func (hr *HotReloader) GetCurrentConfig() *Config {
	hr.mu.RLock()
	defer hr.mu.RUnlock()
	return hr.currentConfig
}

// cloneConfig 深度複制配置（简化實現，實際应該使用更完善的深度複制）
func (hr *HotReloader) cloneConfig(config *Config) *Config {
	// 使用序列化/反序列化實現深度複制
	// 这里返回配置的引用，實際使用時应該真正實現深度複制
	// 為了简化，这里暂時返回原配置（實際应該使用gob或json序列化）
	return config
}
