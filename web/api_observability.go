package web

import (
	"context"
	"net/http"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"

	"quantmesh/logger"
	"quantmesh/notify/observability"
)

var (
	observabilityReloaderMu sync.RWMutex
	observabilityReloader   func()
)

// RegisterObservabilityReloader 由 main 包注入，设置更新后触发 PostHog / Sentry 重载。
func RegisterObservabilityReloader(fn func()) {
	observabilityReloaderMu.Lock()
	defer observabilityReloaderMu.Unlock()
	observabilityReloader = fn
}

func triggerObservabilityReload() {
	observabilityReloaderMu.RLock()
	fn := observabilityReloader
	observabilityReloaderMu.RUnlock()
	if fn != nil {
		fn()
	}
}

type observabilityConfigResponse struct {
	PostHogEnabled        bool   `json:"posthog_enabled"`
	PostHogHasProjectKey  bool   `json:"posthog_has_project_key"`
	PostHogProjectKeyMask string `json:"posthog_project_key_mask"`
	PostHogHost           string `json:"posthog_host"`
	PostHogDefaultHost    string `json:"posthog_default_host"`
	SentryEnabled         bool   `json:"sentry_enabled"`
	SentryHasDSN          bool   `json:"sentry_has_dsn"`
	SentryDSNMask         string `json:"sentry_dsn_mask"`
	Environment           string `json:"environment"`
	DefaultEnvironment    string `json:"default_environment"`
}

type observabilityUpdateRequest struct {
	PostHogProjectKey *string `json:"posthog_project_key"`
	PostHogHost       *string `json:"posthog_host"`
	PostHogEnabled    *bool   `json:"posthog_enabled"`
	SentryDSN         *string `json:"sentry_dsn"`
	SentryEnabled     *bool   `json:"sentry_enabled"`
	Environment       *string `json:"environment"`
}

func getObservabilityConfig(c *gin.Context) {
	if systemSettingsProvider == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "设置提供者未初始化"})
		return
	}
	c.JSON(http.StatusOK, loadObservabilityConfigResponse(context.Background()))
}

func putObservabilityConfig(c *gin.Context) {
	if systemSettingsProvider == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "设置提供者未初始化"})
		return
	}
	var req observabilityUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "无效的请求", err)
		return
	}
	ctx := context.Background()
	if req.PostHogProjectKey != nil {
		val := trimClearable(*req.PostHogProjectKey)
		if err := systemSettingsProvider.SetSystemSettingString(ctx, observability.SettingKeyPostHogProjectKey, val); err != nil {
			respondError(c, http.StatusInternalServerError, "保存 PostHog Project API Key 失败", err)
			return
		}
	}
	if req.PostHogHost != nil {
		val := strings.TrimSpace(*req.PostHogHost)
		if err := systemSettingsProvider.SetSystemSettingString(ctx, observability.SettingKeyPostHogHost, val); err != nil {
			respondError(c, http.StatusInternalServerError, "保存 PostHog Host 失败", err)
			return
		}
	}
	if req.PostHogEnabled != nil {
		if err := systemSettingsProvider.SetSystemSettingBool(ctx, observability.SettingKeyPostHogEnabled, *req.PostHogEnabled); err != nil {
			respondError(c, http.StatusInternalServerError, "保存 PostHog 开关失败", err)
			return
		}
	}
	if req.SentryDSN != nil {
		val := trimClearable(*req.SentryDSN)
		if err := systemSettingsProvider.SetSystemSettingString(ctx, observability.SettingKeySentryDSN, val); err != nil {
			respondError(c, http.StatusInternalServerError, "保存 Sentry DSN 失败", err)
			return
		}
	}
	if req.SentryEnabled != nil {
		if err := systemSettingsProvider.SetSystemSettingBool(ctx, observability.SettingKeySentryEnabled, *req.SentryEnabled); err != nil {
			respondError(c, http.StatusInternalServerError, "保存 Sentry 开关失败", err)
			return
		}
	}
	if req.Environment != nil {
		val := strings.TrimSpace(*req.Environment)
		if err := systemSettingsProvider.SetSystemSettingString(ctx, observability.SettingKeyEnvironment, val); err != nil {
			respondError(c, http.StatusInternalServerError, "保存环境名失败", err)
			return
		}
	}

	triggerObservabilityReload()
	logger.Info("PostHog/Sentry 可观测性配置已更新，已触发重载")
	c.JSON(http.StatusOK, loadObservabilityConfigResponse(ctx))
}

type observabilityTestRequest struct {
	Provider          string `json:"provider"`
	PostHogProjectKey string `json:"posthog_project_key"`
	PostHogHost       string `json:"posthog_host"`
	SentryDSN         string `json:"sentry_dsn"`
	Environment       string `json:"environment"`
}

func postObservabilityTest(c *gin.Context) {
	var req observabilityTestRequest
	_ = c.ShouldBindJSON(&req)
	cfg := loadObservabilityConfigForTest(context.Background(), req)

	switch strings.ToLower(strings.TrimSpace(req.Provider)) {
	case "posthog":
		if err := observability.TestPostHog(cfg); err != nil {
			c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true, "message": "已发送 PostHog 测试事件"})
	case "sentry":
		if err := observability.TestSentry(cfg); err != nil {
			c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true, "message": "已发送 Sentry 测试事件"})
	default:
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "未知的测试目标"})
	}
}

func loadObservabilityConfigResponse(ctx context.Context) observabilityConfigResponse {
	resp := observabilityConfigResponse{
		PostHogHost:        observability.DefaultPostHogHost,
		PostHogDefaultHost: observability.DefaultPostHogHost,
		Environment:        observability.DefaultEnvironment,
		DefaultEnvironment: observability.DefaultEnvironment,
	}
	if v, err := systemSettingsProvider.GetSystemSetting(ctx, observability.SettingKeyPostHogProjectKey); err == nil && v != nil && v.Value != "" {
		resp.PostHogHasProjectKey = true
		resp.PostHogProjectKeyMask = maskAPIKey(v.Value)
	}
	if v, err := systemSettingsProvider.GetSystemSetting(ctx, observability.SettingKeyPostHogHost); err == nil && v != nil && v.Value != "" {
		resp.PostHogHost = v.Value
	}
	if enabled, err := systemSettingsProvider.GetSystemSettingBool(ctx, observability.SettingKeyPostHogEnabled, false); err == nil {
		resp.PostHogEnabled = enabled
	}
	if v, err := systemSettingsProvider.GetSystemSetting(ctx, observability.SettingKeySentryDSN); err == nil && v != nil && v.Value != "" {
		resp.SentryHasDSN = true
		resp.SentryDSNMask = maskDSN(v.Value)
	}
	if enabled, err := systemSettingsProvider.GetSystemSettingBool(ctx, observability.SettingKeySentryEnabled, false); err == nil {
		resp.SentryEnabled = enabled
	}
	if v, err := systemSettingsProvider.GetSystemSetting(ctx, observability.SettingKeyEnvironment); err == nil && v != nil && v.Value != "" {
		resp.Environment = v.Value
	}
	return resp
}

func loadObservabilityConfigForTest(ctx context.Context, req observabilityTestRequest) observability.Config {
	cfg := observability.Config{
		PostHogHost: observability.DefaultPostHogHost,
		Environment: observability.DefaultEnvironment,
		Release:     appVersion,
		DistinctID:  "quantmesh-server",
	}
	if systemSettingsProvider != nil {
		if v, err := systemSettingsProvider.GetSystemSetting(ctx, observability.SettingKeyPostHogProjectKey); err == nil && v != nil {
			cfg.PostHogProjectKey = v.Value
		}
		if v, err := systemSettingsProvider.GetSystemSetting(ctx, observability.SettingKeyPostHogHost); err == nil && v != nil && v.Value != "" {
			cfg.PostHogHost = v.Value
		}
		if v, err := systemSettingsProvider.GetSystemSetting(ctx, observability.SettingKeySentryDSN); err == nil && v != nil {
			cfg.SentryDSN = v.Value
		}
		if v, err := systemSettingsProvider.GetSystemSetting(ctx, observability.SettingKeyEnvironment); err == nil && v != nil && v.Value != "" {
			cfg.Environment = v.Value
		}
	}
	if req.PostHogProjectKey != "" {
		cfg.PostHogProjectKey = strings.TrimSpace(req.PostHogProjectKey)
	}
	if req.PostHogHost != "" {
		cfg.PostHogHost = strings.TrimSpace(req.PostHogHost)
	}
	if req.SentryDSN != "" {
		cfg.SentryDSN = strings.TrimSpace(req.SentryDSN)
	}
	if req.Environment != "" {
		cfg.Environment = strings.TrimSpace(req.Environment)
	}
	cfg.PostHogEnabled = true
	cfg.SentryEnabled = true
	return cfg
}

func trimClearable(value string) string {
	value = strings.TrimSpace(value)
	if value == "__clear__" {
		return ""
	}
	return value
}

func maskDSN(dsn string) string {
	before, _, found := strings.Cut(dsn, "@")
	if !found {
		return maskAPIKey(dsn)
	}
	if len(before) <= 16 {
		return strings.Repeat("*", len(before)) + "@***"
	}
	return before[:12] + "..." + before[len(before)-4:] + "@***"
}

// maskAPIKey 脱敏 API Key，仅保留头尾少量字符用于人工核对。
// 先前定义在已移除的 api_aipipe.go 中，因 MCP 与可观测性配置接口都依赖它，
// 随文件删除会导致这两处编译失败，故迁移至此。
func maskAPIKey(k string) string {
	if len(k) <= 16 {
		return strings.Repeat("*", len(k))
	}
	return k[:12] + "..." + k[len(k)-4:]
}
