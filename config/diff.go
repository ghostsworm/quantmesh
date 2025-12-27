package config

import (
	"fmt"
	"reflect"
	"strings"
)

// ChangeType 变更类型
type ChangeType string

const (
	ChangeTypeAdded   ChangeType = "added"   // 新增
	ChangeTypeModified ChangeType = "modified" // 修改
	ChangeTypeDeleted ChangeType = "deleted"  // 删除
)

// ConfigChange 配置变更
type ConfigChange struct {
	Path          string      `json:"path"`           // 配置路径（如 "trading.price_interval"）
	Type          ChangeType  `json:"type"`           // 变更类型
	OldValue      interface{} `json:"old_value"`      // 旧值
	NewValue      interface{} `json:"new_value"`      // 新值
	RequiresRestart bool      `json:"requires_restart"` // 是否需要重启
}

// ConfigDiff 配置差异
type ConfigDiff struct {
	Changes      []ConfigChange `json:"changes"`       // 变更列表
	RequiresRestart bool        `json:"requires_restart"` // 是否有需要重启的变更
}

// DiffConfig 对比两个配置，生成差异
func DiffConfig(oldConfig, newConfig *Config) *ConfigDiff {
	diff := &ConfigDiff{
		Changes: []ConfigChange{},
	}

	// 对比各个配置段
	diff.compareConfig(oldConfig, newConfig, "")

	// 检查是否有需要重启的变更
	for _, change := range diff.Changes {
		if change.RequiresRestart {
			diff.RequiresRestart = true
			break
		}
	}

	return diff
}

// compareConfig 递归对比配置
func (d *ConfigDiff) compareConfig(old, new interface{}, path string) {
	oldVal := reflect.ValueOf(old)
	newVal := reflect.ValueOf(new)

	// 处理指针
	if oldVal.Kind() == reflect.Ptr {
		if oldVal.IsNil() {
			oldVal = reflect.ValueOf(nil)
		} else {
			oldVal = oldVal.Elem()
		}
	}
	if newVal.Kind() == reflect.Ptr {
		if newVal.IsNil() {
			newVal = reflect.ValueOf(nil)
		} else {
			newVal = newVal.Elem()
		}
	}

	// 处理nil值
	if !oldVal.IsValid() && !newVal.IsValid() {
		return
	}

	// 旧值存在，新值不存在：删除
	if oldVal.IsValid() && !newVal.IsValid() {
		d.addChange(path, ChangeTypeDeleted, oldVal.Interface(), nil, path)
		return
	}

	// 旧值不存在，新值存在：新增
	if !oldVal.IsValid() && newVal.IsValid() {
		d.addChange(path, ChangeTypeAdded, nil, newVal.Interface(), path)
		return
	}

	// 类型不同，视为修改
	if oldVal.Type() != newVal.Type() {
		d.addChange(path, ChangeTypeModified, oldVal.Interface(), newVal.Interface(), path)
		return
	}

	// 根据类型处理
	switch oldVal.Kind() {
	case reflect.Struct:
		d.compareStruct(oldVal, newVal, path)
	case reflect.Map:
		d.compareMap(oldVal, newVal, path)
	case reflect.Slice, reflect.Array:
		d.compareSlice(oldVal, newVal, path)
	default:
		// 基本类型，直接比较
		if !reflect.DeepEqual(oldVal.Interface(), newVal.Interface()) {
			d.addChange(path, ChangeTypeModified, oldVal.Interface(), newVal.Interface(), path)
		}
	}
}

// compareStruct 对比结构体
func (d *ConfigDiff) compareStruct(oldVal, newVal reflect.Value, basePath string) {
	typ := oldVal.Type()
	
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		
		// 获取yaml标签
		yamlTag := field.Tag.Get("yaml")
		if yamlTag == "" || yamlTag == "-" {
			continue
		}

		// 解析yaml标签（可能有选项如 "omitempty"）
		yamlName := strings.Split(yamlTag, ",")[0]
		if yamlName == "" {
			yamlName = strings.ToLower(field.Name)
		}

		fieldPath := basePath
		if fieldPath != "" {
			fieldPath += "." + yamlName
		} else {
			fieldPath = yamlName
		}

		oldField := oldVal.Field(i)
		newField := newVal.Field(i)

		d.compareConfig(oldField.Interface(), newField.Interface(), fieldPath)
	}
}

// compareMap 对比Map
func (d *ConfigDiff) compareMap(oldVal, newVal reflect.Value, basePath string) {
	// 检查旧Map中的所有键
	for _, key := range oldVal.MapKeys() {
		keyStr := fmt.Sprintf("%v", key.Interface())
		path := basePath
		if path != "" {
			path += "." + keyStr
		} else {
			path = keyStr
		}

		oldValue := oldVal.MapIndex(key)
		newValue := newVal.MapIndex(key)

		if !newValue.IsValid() {
			// 键被删除
			d.addChange(path, ChangeTypeDeleted, oldValue.Interface(), nil, path)
		} else {
			// 对比值
			d.compareConfig(oldValue.Interface(), newValue.Interface(), path)
		}
	}

	// 检查新Map中的新键
	for _, key := range newVal.MapKeys() {
		keyStr := fmt.Sprintf("%v", key.Interface())
		path := basePath
		if path != "" {
			path += "." + keyStr
		} else {
			path = keyStr
		}

		oldValue := oldVal.MapIndex(key)
		if !oldValue.IsValid() {
			// 新键
			newValue := newVal.MapIndex(key)
			d.addChange(path, ChangeTypeAdded, nil, newValue.Interface(), path)
		}
	}
}

// compareSlice 对比切片
func (d *ConfigDiff) compareSlice(oldVal, newVal reflect.Value, basePath string) {
	oldLen := oldVal.Len()
	newLen := newVal.Len()

	// 简单比较：如果长度不同，视为整体修改
	if oldLen != newLen {
		d.addChange(basePath, ChangeTypeModified, oldVal.Interface(), newVal.Interface(), basePath)
		return
	}

	// 逐个元素对比
	for i := 0; i < oldLen; i++ {
		path := fmt.Sprintf("%s[%d]", basePath, i)
		d.compareConfig(oldVal.Index(i).Interface(), newVal.Index(i).Interface(), path)
	}
}

// addChange 添加变更记录
func (d *ConfigDiff) addChange(path string, changeType ChangeType, oldValue, newValue interface{}, fullPath string) {
	change := ConfigChange{
		Path:            path,
		Type:            changeType,
		OldValue:        oldValue,
		NewValue:        newValue,
		RequiresRestart: requiresRestart(fullPath),
	}

	d.Changes = append(d.Changes, change)
}

// requiresRestart 判断配置路径是否需要重启
func requiresRestart(path string) bool {
	// 需要重启的配置路径
	restartPaths := []string{
		"app.current_exchange",      // 交易所切换
		"web.host",                  // Web服务地址
		"web.port",                  // Web服务端口
		"system.log_level",          // 日志级别（虽然可以热更新，但通常建议重启）
		"system.timezone",           // 系统时区
		"storage.path",              // 存储路径
		"storage.type",              // 存储类型
		"ai.enabled",                // AI功能开关
		"ai.provider",               // AI服务提供商
		"ai.api_key",                // AI API密钥
		"ai.base_url",               // AI基础URL
		"notifications.enabled",     // 通知总开关（可能影响初始化）
		"risk_control.enabled",      // 风控总开关（可能影响初始化）
	}

	for _, restartPath := range restartPaths {
		if path == restartPath || strings.HasPrefix(path, restartPath+".") {
			return true
		}
	}

	return false
}

