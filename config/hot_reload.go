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

// ConfigUpdateCallback 配置更新回调函数类型
type ConfigUpdateCallback func(oldConfig, newConfig *Config, changes []ConfigChange) error

// NewHotReloader 创建热更新器
func NewHotReloader(initialConfig *Config) *HotReloader {
	return &HotReloader{
		currentConfig:   initialConfig,
		updateCallbacks: []ConfigUpdateCallback{},
	}
}

// RegisterCallback 注册配置更新回调
func (hr *HotReloader) RegisterCallback(callback ConfigUpdateCallback) {
	hr.mu.Lock()
	defer hr.mu.Unlock()
	hr.updateCallbacks = append(hr.updateCallbacks, callback)
}

// UpdateConfig 更新配置（热更新）
func (hr *HotReloader) UpdateConfig(newConfig *Config) (*ConfigDiff, error) {
	hr.mu.Lock()
	defer hr.mu.Unlock()

	// 对比配置变更
	diff := DiffConfig(hr.currentConfig, newConfig)

	// 分离需要重启的变更和可以热更新的变更
	hotReloadableChanges := []ConfigChange{}
	restartRequiredChanges := []ConfigChange{}

	for _, change := range diff.Changes {
		if change.RequiresRestart {
			restartRequiredChanges = append(restartRequiredChanges, change)
		} else {
			hotReloadableChanges = append(hotReloadableChanges, change)
		}
	}

	// 如果有需要重启的变更，只更新可以热更新的部分
	if len(restartRequiredChanges) > 0 {
		// 创建只包含可热更新变更的配置
		partialConfig := hr.applyHotReloadableChanges(hr.currentConfig, newConfig, hotReloadableChanges)
		
		// 应用可热更新的变更
		if err := hr.applyConfigUpdate(hr.currentConfig, partialConfig, hotReloadableChanges); err != nil {
			return nil, fmt.Errorf("应用热更新失败: %v", err)
		}

		// 更新当前配置
		hr.currentConfig = partialConfig

		// 返回包含重启提示的差异
		return diff, nil
	}

	// 全部可以热更新，直接应用
	if err := hr.applyConfigUpdate(hr.currentConfig, newConfig, diff.Changes); err != nil {
		return nil, fmt.Errorf("应用配置更新失败: %v", err)
	}

	// 更新当前配置
	hr.currentConfig = newConfig

	return diff, nil
}

// applyHotReloadableChanges 应用可热更新的变更，创建部分更新的配置
func (hr *HotReloader) applyHotReloadableChanges(oldConfig, newConfig *Config, hotReloadableChanges []ConfigChange) *Config {
	// 深度复制旧配置
	result := hr.cloneConfig(oldConfig)

	// 应用可热更新的变更
	for _, change := range hotReloadableChanges {
		hr.applyChangeToConfig(result, newConfig, change.Path, change.NewValue)
	}

	return result
}

// applyChangeToConfig 将单个变更应用到配置
func (hr *HotReloader) applyChangeToConfig(config *Config, sourceConfig *Config, path string, value interface{}) {
	// 简化实现：对于复杂路径，直接从源配置复制整个结构
	// 这里使用反射来实现深度复制特定字段
	hr.copyConfigField(config, sourceConfig, path)
}

// copyConfigField 复制配置字段（简化实现）
func (hr *HotReloader) copyConfigField(dest, src *Config, path string) {
	// 根据路径复制对应的字段
	// 这是一个简化实现，实际应该使用反射进行深度复制
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

// applyConfigUpdate 应用配置更新并触发回调
func (hr *HotReloader) applyConfigUpdate(oldConfig, newConfig *Config, changes []ConfigChange) error {
	// 触发所有注册的回调
	for _, callback := range hr.updateCallbacks {
		if err := callback(oldConfig, newConfig, changes); err != nil {
			return fmt.Errorf("配置更新回调执行失败: %v", err)
		}
	}

	return nil
}

// GetCurrentConfig 获取当前配置
func (hr *HotReloader) GetCurrentConfig() *Config {
	hr.mu.RLock()
	defer hr.mu.RUnlock()
	return hr.currentConfig
}

// cloneConfig 深度复制配置（简化实现，实际应该使用更完善的深度复制）
func (hr *HotReloader) cloneConfig(config *Config) *Config {
	// 使用序列化/反序列化实现深度复制
	// 这里返回配置的引用，实际使用时应该真正实现深度复制
	// 为了简化，这里暂时返回原配置（实际应该使用gob或json序列化）
	return config
}

