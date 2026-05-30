package web

import (
	"context"
	"net/http"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"

	"quantmesh/logger"
	"quantmesh/notify/aipipe"
)

var (
	aipipeReloaderMu sync.RWMutex
	aipipeReloader   func()
)

// RegisterAipipeReloader 由 main 包注入：用户改完 aipipe 设置后调用，触发 reporter 重载。
// 这样 web 包不需要直接拥有 SystemSettingsProvider 的具体加载逻辑。
func RegisterAipipeReloader(fn func()) {
	aipipeReloaderMu.Lock()
	defer aipipeReloaderMu.Unlock()
	aipipeReloader = fn
}

func triggerAipipeReload() {
	aipipeReloaderMu.RLock()
	fn := aipipeReloader
	aipipeReloaderMu.RUnlock()
	if fn != nil {
		fn()
	}
}

// aipipeConfigResponse 给前端的响应（不返回 key 全文，只返回脱敏后的）。
type aipipeConfigResponse struct {
	Enabled     bool   `json:"enabled"`
	APIKeyMask  string `json:"api_key_mask"` // 仅展示前 12 + 末 4，方便用户识别
	HasAPIKey   bool   `json:"has_api_key"`
	Endpoint    string `json:"endpoint"`
	DefaultEnpd string `json:"default_endpoint"`
}

// GET /api/aipipe/config
func getAipipeConfig(c *gin.Context) {
	if systemSettingsProvider == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "设置提供者未初始化"})
		return
	}
	ctx := context.Background()
	resp := aipipeConfigResponse{
		Endpoint:    aipipe.DefaultEndpoint,
		DefaultEnpd: aipipe.DefaultEndpoint,
	}
	if v, err := systemSettingsProvider.GetSystemSetting(ctx, aipipe.SettingKeyAPIKey); err == nil && v != nil && v.Value != "" {
		resp.HasAPIKey = true
		resp.APIKeyMask = maskAPIKey(v.Value)
	}
	if v, err := systemSettingsProvider.GetSystemSetting(ctx, aipipe.SettingKeyEndpoint); err == nil && v != nil && v.Value != "" {
		resp.Endpoint = v.Value
	}
	if enabled, err := systemSettingsProvider.GetSystemSettingBool(ctx, aipipe.SettingKeyEnabled, false); err == nil {
		resp.Enabled = enabled
	}
	c.JSON(http.StatusOK, resp)
}

// aipipeUpdateRequest 前端提交的更新请求。
//   - api_key 留空 → 不修改已有 key（用于只改开关或 endpoint 时）
//   - api_key="__clear__" → 清空 key（且禁用上报）
type aipipeUpdateRequest struct {
	APIKey   *string `json:"api_key"`
	Endpoint *string `json:"endpoint"`
	Enabled  *bool   `json:"enabled"`
}

// PUT /api/aipipe/config
func putAipipeConfig(c *gin.Context) {
	if systemSettingsProvider == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "设置提供者未初始化"})
		return
	}
	var req aipipeUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "无效的请求", err)
		return
	}
	ctx := context.Background()

	if req.APIKey != nil {
		val := strings.TrimSpace(*req.APIKey)
		if val == "__clear__" {
			val = ""
		}
		if err := systemSettingsProvider.SetSystemSettingString(ctx, aipipe.SettingKeyAPIKey, val); err != nil {
			respondError(c, http.StatusInternalServerError, "保存 API Key 失败", err)
			return
		}
	}
	if req.Endpoint != nil {
		val := strings.TrimSpace(*req.Endpoint)
		if err := systemSettingsProvider.SetSystemSettingString(ctx, aipipe.SettingKeyEndpoint, val); err != nil {
			respondError(c, http.StatusInternalServerError, "保存 Endpoint 失败", err)
			return
		}
	}
	if req.Enabled != nil {
		if err := systemSettingsProvider.SetSystemSettingBool(ctx, aipipe.SettingKeyEnabled, *req.Enabled); err != nil {
			respondError(c, http.StatusInternalServerError, "保存开关失败", err)
			return
		}
	}

	triggerAipipeReload()
	logger.Info("aipipe 配置已更新，已触发 reporter 重载")
	getAipipeConfig(c)
}

// POST /api/aipipe/test
// body: { api_key?, endpoint? } —— 留空则使用当前已存的值
type aipipeTestRequest struct {
	APIKey   string `json:"api_key"`
	Endpoint string `json:"endpoint"`
}

func postAipipeTest(c *gin.Context) {
	var req aipipeTestRequest
	_ = c.ShouldBindJSON(&req)

	ctx := context.Background()
	apiKey := strings.TrimSpace(req.APIKey)
	endpoint := strings.TrimSpace(req.Endpoint)

	if apiKey == "" && systemSettingsProvider != nil {
		if v, err := systemSettingsProvider.GetSystemSetting(ctx, aipipe.SettingKeyAPIKey); err == nil && v != nil {
			apiKey = v.Value
		}
	}
	if endpoint == "" && systemSettingsProvider != nil {
		if v, err := systemSettingsProvider.GetSystemSetting(ctx, aipipe.SettingKeyEndpoint); err == nil && v != nil {
			endpoint = v.Value
		}
	}
	if endpoint == "" {
		endpoint = aipipe.DefaultEndpoint
	}
	if apiKey == "" {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "API Key 为空"})
		return
	}
	if err := aipipe.TestConfig(aipipe.Config{APIKey: apiKey, Endpoint: endpoint, Enabled: true}); err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "message": "已发送测试事件，请在 17push 控制台查看"})
}

func maskAPIKey(k string) string {
	if len(k) <= 16 {
		return strings.Repeat("*", len(k))
	}
	return k[:12] + "..." + k[len(k)-4:]
}
