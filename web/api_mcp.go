package web

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"

	"quantmesh/logger"
	"quantmesh/mcp"
)

const (
	settingKeyMCPToken      = "mcp_token"
	settingKeyMCPAllowWrite = "mcp_allow_write"
	settingKeyMCPEnabled    = "mcp_enabled"
)

var (
	mcpServer       *mcp.Server
	mcpServerMu     sync.RWMutex
	mcpTokenCache   string
	mcpTokenCacheMu sync.RWMutex
	mcpReloader     func()
)

// RegisterMCPReloader main 注入：当 mcp_allow_write 改变时调用，让 main 重建
// server 并重新注册工具（写工具的有无要重置）。
func RegisterMCPReloader(fn func()) {
	mcpReloader = fn
}

// SetMCPServer 由 main 把构建好的 server 注入；为 nil 则停用对外接口。
func SetMCPServer(s *mcp.Server) {
	mcpServerMu.Lock()
	defer mcpServerMu.Unlock()
	mcpServer = s
}

// MCPTokenCheck 给 mcp.Server 用的鉴权回调：缓存 token，避免每个请求都读 DB。
func MCPTokenCheck(token string) bool {
	mcpTokenCacheMu.RLock()
	cached := mcpTokenCache
	mcpTokenCacheMu.RUnlock()
	if cached != "" && cached == token {
		return true
	}
	// 缓存未命中或不匹配 → 从 settings 取一次
	if systemSettingsProvider == nil {
		return false
	}
	v, err := systemSettingsProvider.GetSystemSetting(context.Background(), settingKeyMCPToken)
	if err != nil || v == nil || v.Value == "" {
		return false
	}
	mcpTokenCacheMu.Lock()
	mcpTokenCache = v.Value
	mcpTokenCacheMu.Unlock()
	return v.Value == token
}

// invalidateMCPTokenCache token 改了之后调一下。
func invalidateMCPTokenCache() {
	mcpTokenCacheMu.Lock()
	mcpTokenCache = ""
	mcpTokenCacheMu.Unlock()
}

// mountMCPIfReady 由路由初始化阶段调用：始终在 /mcp 挂代理 handler，
// 实际转发到运行时通过 SetMCPServer 注入的 mcp.Server。
//
// 为啥不直接挂 server.Mount？因为 main 的存储/设置初始化时序比 NewWebServer
// 晚得多，路由阶段 mcpServer 还是 nil。我们用一个间接层来解决：
// gin 拒绝运行时加路由，但代理 handler 可以读 atomic-like 状态。
func mountMCPIfReady(r *gin.Engine) {
	if r == nil {
		return
	}
	handler := func(c *gin.Context) {
		mcpServerMu.RLock()
		s := mcpServer
		mcpServerMu.RUnlock()
		if s == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "MCP 服务未初始化（系统刚启动？请稍候）"})
			return
		}
		s.ServeOne(c)
	}
	r.GET("/mcp", handler)
	r.POST("/mcp", handler)
	r.OPTIONS("/mcp", handler)
	logger.Info("✅ MCP 路由已注册: /mcp (server 注入后即可用)")
}

// —————————————————————————— HTTP API ——————————————————————————

type mcpConfigResponse struct {
	Enabled    bool   `json:"enabled"`
	HasToken   bool   `json:"has_token"`
	TokenMask  string `json:"token_mask"`
	AllowWrite bool   `json:"allow_write"`
	ToolCount  int    `json:"tool_count"`
	MountPath  string `json:"mount_path"`
}

// GET /api/mcp/config
func getMCPConfig(c *gin.Context) {
	if systemSettingsProvider == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "设置提供者未初始化"})
		return
	}
	ctx := context.Background()
	resp := mcpConfigResponse{MountPath: "/mcp"}

	if v, err := systemSettingsProvider.GetSystemSetting(ctx, settingKeyMCPToken); err == nil && v != nil && v.Value != "" {
		resp.HasToken = true
		resp.TokenMask = maskAPIKey(v.Value)
	}
	if enabled, err := systemSettingsProvider.GetSystemSettingBool(ctx, settingKeyMCPEnabled, true); err == nil {
		resp.Enabled = enabled
	}
	if aw, err := systemSettingsProvider.GetSystemSettingBool(ctx, settingKeyMCPAllowWrite, false); err == nil {
		resp.AllowWrite = aw
	}
	mcpServerMu.RLock()
	if mcpServer != nil {
		resp.ToolCount = mcpServer.ToolCount()
	}
	mcpServerMu.RUnlock()

	c.JSON(http.StatusOK, resp)
}

// POST /api/mcp/token/rotate
// 生成新 token，覆写 settings；返回明文 token（仅这一次）。
func postMCPRotateToken(c *gin.Context) {
	if systemSettingsProvider == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "设置提供者未初始化"})
		return
	}
	tok, err := generateMCPToken()
	if err != nil {
		respondError(c, http.StatusInternalServerError, "生成 token 失败", err)
		return
	}
	if err := systemSettingsProvider.SetSystemSettingString(context.Background(), settingKeyMCPToken, tok); err != nil {
		respondError(c, http.StatusInternalServerError, "保存 token 失败", err)
		return
	}
	invalidateMCPTokenCache()
	logger.Info("MCP token 已轮换")
	c.JSON(http.StatusOK, gin.H{"ok": true, "token": tok})
}

// DELETE /api/mcp/token
// 清空 token —— 等于禁用所有 MCP 访问。
func deleteMCPToken(c *gin.Context) {
	if systemSettingsProvider == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "设置提供者未初始化"})
		return
	}
	if err := systemSettingsProvider.SetSystemSettingString(context.Background(), settingKeyMCPToken, ""); err != nil {
		respondError(c, http.StatusInternalServerError, "清空 token 失败", err)
		return
	}
	invalidateMCPTokenCache()
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

type mcpUpdateRequest struct {
	Enabled    *bool `json:"enabled"`
	AllowWrite *bool `json:"allow_write"`
}

// PUT /api/mcp/config
func putMCPConfig(c *gin.Context) {
	if systemSettingsProvider == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "设置提供者未初始化"})
		return
	}
	var req mcpUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "无效的请求", err)
		return
	}
	ctx := context.Background()
	allowWriteChanged := false
	if req.Enabled != nil {
		if err := systemSettingsProvider.SetSystemSettingBool(ctx, settingKeyMCPEnabled, *req.Enabled); err != nil {
			respondError(c, http.StatusInternalServerError, "保存开关失败", err)
			return
		}
	}
	if req.AllowWrite != nil {
		if err := systemSettingsProvider.SetSystemSettingBool(ctx, settingKeyMCPAllowWrite, *req.AllowWrite); err != nil {
			respondError(c, http.StatusInternalServerError, "保存写权限开关失败", err)
			return
		}
		allowWriteChanged = true
	}
	if allowWriteChanged && mcpReloader != nil {
		mcpReloader()
	}
	getMCPConfig(c)
}

// GET /api/mcp/client-snippet?host=https://host[&style=claude|cursor|generic]
// 返回给 agent 客户端粘贴的 JSON 片段。
func getMCPClientSnippet(c *gin.Context) {
	host := strings.TrimSpace(c.Query("host"))
	if host == "" {
		host = inferHostFromRequest(c)
	}
	host = strings.TrimRight(host, "/")
	style := c.DefaultQuery("style", "claude")

	tok := ""
	if systemSettingsProvider != nil {
		if v, err := systemSettingsProvider.GetSystemSetting(context.Background(), settingKeyMCPToken); err == nil && v != nil {
			tok = v.Value
		}
	}
	if tok == "" {
		tok = "<在设置页生成 token 后此处会自动填充>"
	}

	url := host + "/mcp"
	switch style {
	case "cursor":
		c.JSON(http.StatusOK, gin.H{
			"format": "cursor mcp.json",
			"snippet": gin.H{
				"mcpServers": gin.H{
					"quantmesh": gin.H{
						"url":     url,
						"headers": gin.H{"Authorization": "Bearer " + tok},
					},
				},
			},
		})
	case "generic":
		c.JSON(http.StatusOK, gin.H{
			"url":     url,
			"headers": gin.H{"Authorization": "Bearer " + tok},
		})
	default:
		// Claude Desktop 风格
		c.JSON(http.StatusOK, gin.H{
			"format": "claude_desktop_config.json",
			"snippet": gin.H{
				"mcpServers": gin.H{
					"quantmesh": gin.H{
						"url":     url,
						"headers": gin.H{"Authorization": "Bearer " + tok},
					},
				},
			},
		})
	}
}

func inferHostFromRequest(c *gin.Context) string {
	scheme := "http"
	if c.Request.TLS != nil || strings.EqualFold(c.GetHeader("X-Forwarded-Proto"), "https") {
		scheme = "https"
	}
	host := c.Request.Host
	if h := c.GetHeader("X-Forwarded-Host"); h != "" {
		host = h
	}
	return fmt.Sprintf("%s://%s", scheme, host)
}

func generateMCPToken() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "qm_mcp_" + hex.EncodeToString(b), nil
}
