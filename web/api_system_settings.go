package web

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
)

var systemSettingsProvider SystemSettingsProvider

// SetSystemSettingsProvider 设置系统设置提供者
func SetSystemSettingsProvider(provider SystemSettingsProvider) {
	systemSettingsProvider = provider
	// 同时设置给 auth_middleware 使用
	SetStorageProvider(provider)
	// 刷新缓存
	refreshLocalDevModeCache()
}

// SystemSettingRequest 系统设置请求
type SystemSettingRequest struct {
	Key   string `json:"key" binding:"required"`
	Value string `json:"value" binding:"required"`
	Type  string `json:"type"` // string, boolean, number, json
}

// SystemSettingResponse 系统设置响应
type SystemSettingResponse struct {
	Key       string `json:"key"`
	Value     string `json:"value"`
	Type      string `json:"type"`
	CreatedAt string `json:"created_at,omitempty"`
	UpdatedAt string `json:"updated_at,omitempty"`
}

// getSystemSettings 获取系统设置列表
// GET /api/system/settings
func getSystemSettings(c *gin.Context) {
	if systemSettingsProvider == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "设置提供者未初始化"})
		return
	}

	ctx := context.Background()

	// 获取所有设置
	settings, err := systemSettingsProvider.GetSystemSettings(ctx, nil)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "获取设置失败", err)
		return
	}

	// 转换为响应格式
	response := make([]SystemSettingResponse, 0, len(settings))
	for _, s := range settings {
		response = append(response, SystemSettingResponse{
			Key:       s.Key,
			Value:     s.Value,
			Type:      s.Type,
			CreatedAt: s.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
			UpdatedAt: s.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
		})
	}

	c.JSON(http.StatusOK, gin.H{"settings": response})
}

// getSystemSetting 获取单个系统设置
// GET /api/system/settings/:key
func getSystemSetting(c *gin.Context) {
	if systemSettingsProvider == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "设置提供者未初始化"})
		return
	}

	key := c.Param("key")
	if key == "" {
		respondError(c, http.StatusBadRequest, "缺少 key 参数")
		return
	}

	ctx := context.Background()
	setting, err := systemSettingsProvider.GetSystemSetting(ctx, key)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "获取设置失败", err)
		return
	}

	if setting == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "设置不存在"})
		return
	}

	c.JSON(http.StatusOK, SystemSettingResponse{
		Key:       setting.Key,
		Value:     setting.Value,
		Type:      setting.Type,
		CreatedAt: setting.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		UpdatedAt: setting.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
	})
}

// setSystemSetting 设置系统设置
// POST /api/system/settings
func setSystemSetting(c *gin.Context) {
	if systemSettingsProvider == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "设置提供者未初始化"})
		return
	}

	var req SystemSettingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "无效的请求", err)
		return
	}

	// 如果没有指定类型，默认为 string
	if req.Type == "" {
		req.Type = "string"
	}

	ctx := context.Background()

	// 根据类型设置值
	switch req.Type {
	case "boolean":
		var boolValue bool
		if req.Value == "true" || req.Value == "1" {
			boolValue = true
		}
		if err := systemSettingsProvider.SetSystemSettingBool(ctx, req.Key, boolValue); err != nil {
			respondError(c, http.StatusInternalServerError, "保存设置失败", err)
			return
		}
		// 如果是 local_dev_mode，刷新缓存
		if req.Key == "local_dev_mode" {
			refreshLocalDevModeCache()
		}
	case "string":
		if err := systemSettingsProvider.SetSystemSettingString(ctx, req.Key, req.Value); err != nil {
			respondError(c, http.StatusInternalServerError, "保存设置失败", err)
			return
		}
	default:
		// JSON 类型直接保存
		setting := &SystemSettingStruct{
			Key:   req.Key,
			Value: req.Value,
			Type:  req.Type,
		}
		if err := systemSettingsProvider.SaveSystemSetting(ctx, setting); err != nil {
			respondError(c, http.StatusInternalServerError, "保存设置失败", err)
			return
		}
	}

	// 重新获取设置
	setting, err := systemSettingsProvider.GetSystemSetting(ctx, req.Key)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "获取设置失败", err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"ok":      true,
		"setting": SystemSettingResponse{
			Key:       setting.Key,
			Value:     setting.Value,
			Type:      setting.Type,
			CreatedAt: setting.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
			UpdatedAt: setting.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
		},
	})
}

// deleteSystemSetting 删除系统设置
// DELETE /api/system/settings/:key
func deleteSystemSetting(c *gin.Context) {
	if systemSettingsProvider == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "设置提供者未初始化"})
		return
	}

	key := c.Param("key")
	if key == "" {
		respondError(c, http.StatusBadRequest, "缺少 key 参数")
		return
	}

	// 禁止删除某些关键设置
	if key == "local_dev_mode" {
		c.JSON(http.StatusForbidden, gin.H{"error": "不能删除此设置，请使用 POST 修改值"})
		return
	}

	ctx := context.Background()
	if err := systemSettingsProvider.DeleteSystemSetting(ctx, key); err != nil {
		respondError(c, http.StatusInternalServerError, "删除设置失败", err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"ok": true, "message": "设置已删除"})
}

// getLocalDevMode 获取本地开发模式状态
// GET /api/system/local-dev-mode
func getLocalDevMode(c *gin.Context) {
	if systemSettingsProvider == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "设置提供者未初始化"})
		return
	}

	ctx := context.Background()
	enabled, err := systemSettingsProvider.GetSystemSettingBool(ctx, "local_dev_mode", false)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "获取设置失败", err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"enabled": enabled,
	})
}

// setLocalDevMode 设置本地开发模式
// POST /api/system/local-dev-mode
func setLocalDevMode(c *gin.Context) {
	if systemSettingsProvider == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "设置提供者未初始化"})
		return
	}

	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "无效的请求", err)
		return
	}

	ctx := context.Background()
	if err := systemSettingsProvider.SetSystemSettingBool(ctx, "local_dev_mode", req.Enabled); err != nil {
		respondError(c, http.StatusInternalServerError, "保存设置失败", err)
		return
	}

	// 刷新缓存
	refreshLocalDevModeCache()

	logger.WriteWebLog("[SYSTEM] Local dev mode 设置为: %v", req.Enabled)
	c.JSON(http.StatusOK, gin.H{
		"ok":      true,
		"enabled": req.Enabled,
		"message": map[bool]string{
			true:  "本地开发模式已启用（无需登录）",
			false: "本地开发模式已禁用（需要登录）",
		}[req.Enabled],
	})
}

// SystemSettingStruct 系统设置结构（避免命名冲突）
type SystemSettingStruct struct {
	ID        int64
	Key       string
	Value     string
	Type      string
	CreatedAt string
	UpdatedAt string
}
