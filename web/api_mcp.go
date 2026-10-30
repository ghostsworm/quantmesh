package web

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"quantmesh/logger"
	"quantmesh/mcp"
)

var (
	mcpServer   *mcp.Server
	mcpServerMu sync.RWMutex
	mcpReloader func()
)

// RegisterMCPReloader main 注入：设置变化时重建 MCP server。
// 写权限现在由每张令牌决定，重建不再用来增删写工具。
func RegisterMCPReloader(fn func()) {
	mcpReloader = fn
}

// SetMCPServer 由 main 把构建好的 server 注入；为 nil 则停用对外接口。
func SetMCPServer(s *mcp.Server) {
	mcpServerMu.Lock()
	defer mcpServerMu.Unlock()
	mcpServer = s
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

type mcpTokenView struct {
	ID           string          `json:"id"`
	Name         string          `json:"name"`
	Mode         string          `json:"mode"`
	Notify       *mcp.NotifySpec `json:"notify,omitempty"`
	EndpointURL  string          `json:"endpoint_url"`
	CreatedAt    string          `json:"created_at"`
	Legacy       bool            `json:"legacy,omitempty"`
	SecretCopied bool            `json:"secret_available"`
}

type mcpConfigResponse struct {
	Enabled     bool           `json:"enabled"`
	HasToken    bool           `json:"has_token"`
	TokenMask   string         `json:"token_mask"`
	AllowWrite  bool           `json:"allow_write"`
	ToolCount   int            `json:"tool_count"`
	MountPath   string         `json:"mount_path"`
	EndpointURL string         `json:"endpoint_url"`
	Tokens      []mcpTokenView `json:"tokens"`
}

// GET /api/mcp/config
func getMCPConfig(c *gin.Context) {
	if systemSettingsProvider == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "设置提供者未初始化"})
		return
	}
	ctx := context.Background()
	resp := mcpConfigResponse{MountPath: "/mcp", EndpointURL: inferHostFromRequest(c) + "/mcp", Tokens: []mcpTokenView{}}

	if hasLegacy, mask := legacyMCPTokenStatus(); hasLegacy {
		resp.HasToken = true
		resp.TokenMask = mask
	}
	if enabled, err := systemSettingsProvider.GetSystemSettingBool(ctx, settingKeyMCPEnabled, true); err == nil {
		resp.Enabled = enabled
	}
	if aw, err := systemSettingsProvider.GetSystemSettingBool(ctx, settingKeyMCPAllowWrite, false); err == nil {
		resp.AllowWrite = aw
	}
	if records, err := activeMCPTokens(); err == nil {
		for _, record := range records {
			resp.Tokens = append(resp.Tokens, mcpTokenView{
				ID: record.ID, Name: record.Name, Mode: record.Mode, Notify: cloneNotify(record.Notify),
				CreatedAt: record.CreatedAt.Format(time.RFC3339),
			})
		}
		if len(resp.Tokens) > 0 {
			resp.HasToken = true
		}
	}
	mcpServerMu.RLock()
	if mcpServer != nil {
		resp.ToolCount = mcpServer.ToolCount()
	}
	mcpServerMu.RUnlock()

	c.JSON(http.StatusOK, resp)
}

type mcpIssueRequest struct {
	Name    string          `json:"name"`
	Mode    string          `json:"mode"`
	Notify  *mcp.NotifySpec `json:"notify"`
	Channel string          `json:"notify_channel"`
	Target  string          `json:"notify_target"`
}

// POST /api/mcp/tokens
// 颁发一张新令牌。明文密钥和带密钥的地址只在这次响应里出现。
func postMCPIssueToken(c *gin.Context) {
	if systemSettingsProvider == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": mcpErrorStoreUnavailable})
		return
	}
	var req mcpIssueRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "无效的请求", err)
		return
	}
	notifySpec := req.Notify
	if notifySpec == nil && (req.Channel != "" || req.Target != "") {
		notifySpec = &mcp.NotifySpec{Channel: req.Channel, Target: req.Target}
	}
	record, secret, err := issueMCPToken(mcpTokenInput{Name: req.Name, Mode: req.Mode, Notify: notifySpec})
	if err != nil {
		respondError(c, mcpIssueStatus(err), err.Error(), err)
		return
	}
	endpoint := mcpEndpointURL(inferHostFromRequest(c), secret)
	c.JSON(http.StatusOK, gin.H{
		"ok":           true,
		"id":           record.ID,
		"name":         record.Name,
		"mode":         record.Mode,
		"token":        secret,
		"endpoint_url": endpoint,
		"notify":       record.Notify,
	})
}

// POST /api/mcp/token/rotate 保留给旧界面：等价于颁发一张只读令牌。
func postMCPRotateToken(c *gin.Context) {
	var req mcpIssueRequest
	_ = c.ShouldBindJSON(&req)
	if strings.TrimSpace(req.Name) == "" {
		req.Name = "只读令牌"
	}
	if req.Mode == "" {
		req.Mode = mcp.AccessReadOnly
	}
	postMCPIssueToken(c)
}

// DELETE /api/mcp/tokens/:id 吊销一张令牌，立即失效。
func deleteMCPTokenByID(c *gin.Context) {
	if systemSettingsProvider == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": mcpErrorStoreUnavailable})
		return
	}
	if err := revokeMCPToken(c.Param("id")); err != nil {
		respondError(c, http.StatusNotFound, err.Error(), err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// DELETE /api/mcp/token 清空旧的全局令牌。不影响按张颁发的令牌。
func deleteMCPToken(c *gin.Context) {
	if systemSettingsProvider == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": mcpErrorStoreUnavailable})
		return
	}
	if err := clearLegacyMCPToken(); err != nil {
		respondError(c, http.StatusInternalServerError, "清空 token 失败", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func mcpIssueStatus(err error) int {
	switch err.Error() {
	case mcpErrorNameRequired, mcpErrorModeInvalid, mcpErrorNotifyRequired,
		mcpErrorNotifyInvalid, mcpErrorEmailInvalid, mcpErrorURLInvalid:
		return http.StatusBadRequest
	case mcpErrorLimitReached:
		return http.StatusConflict
	default:
		return http.StatusInternalServerError
	}
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

	tok := strings.TrimSpace(c.Query("token"))
	if tok == "" && systemSettingsProvider != nil {
		if v, err := systemSettingsProvider.GetSystemSetting(context.Background(), settingKeyMCPToken); err == nil && v != nil {
			tok = v.Value
		}
	}
	if tok == "" {
		tok = "<颁发令牌后此处会自动填充>"
	}

	url := mcpEndpointURL(host, tok)
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

// mcpEndpointURL 把密钥放进 query。Streamable HTTP 客户端只填一个 URL 就能连上，
// 请求头方式仍然可用。密钥必须做转义，避免特殊字符把 URL 截断。
func mcpEndpointURL(host, token string) string {
	base := strings.TrimRight(host, "/") + "/mcp"
	if token == "" || strings.HasPrefix(token, "<") {
		return base
	}
	return base + "?token=" + url.QueryEscape(token)
}
