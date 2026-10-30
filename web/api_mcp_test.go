package web

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"quantmesh/config"
	"quantmesh/mcp"
	"quantmesh/storage"
)

type mcpTokenCheckSettingsProvider struct {
	enabled    bool
	token      string
	allowWrite bool
	values     map[string]string
}

func (p *mcpTokenCheckSettingsProvider) GetSystemSettingBool(_ context.Context, key string, defaultValue bool) (bool, error) {
	switch key {
	case settingKeyMCPEnabled:
		return p.enabled, nil
	case settingKeyMCPAllowWrite:
		return p.allowWrite, nil
	default:
		return defaultValue, nil
	}
}

func (p *mcpTokenCheckSettingsProvider) GetSystemSettings(_ context.Context, _ *storage.SystemSettingFilter) ([]*storage.SystemSetting, error) {
	return nil, nil
}

func (p *mcpTokenCheckSettingsProvider) GetSystemSetting(_ context.Context, key string) (*storage.SystemSetting, error) {
	if key == settingKeyMCPToken && p.token != "" {
		return &storage.SystemSetting{Key: key, Value: p.token}, nil
	}
	if p.values != nil {
		if value, ok := p.values[key]; ok {
			return &storage.SystemSetting{Key: key, Value: value}, nil
		}
	}
	return nil, nil
}

func (p *mcpTokenCheckSettingsProvider) SetSystemSettingBool(_ context.Context, key string, value bool) error {
	if key == settingKeyMCPEnabled {
		p.enabled = value
	}
	return nil
}

func (p *mcpTokenCheckSettingsProvider) SetSystemSettingString(_ context.Context, key, value string) error {
	if p.values == nil {
		p.values = map[string]string{}
	}
	p.values[key] = value
	if key == settingKeyMCPToken {
		p.token = value
	}
	return nil
}

func (p *mcpTokenCheckSettingsProvider) SaveSystemSetting(_ context.Context, key, value, _ string) error {
	if key == settingKeyMCPToken {
		p.token = value
	}
	return nil
}

func (p *mcpTokenCheckSettingsProvider) DeleteSystemSetting(_ context.Context, key string) error {
	if key == settingKeyMCPToken {
		p.token = ""
	}
	return nil
}

func TestMCPTokenCheckHonorsEnabledSwitch(t *testing.T) {
	orig := systemSettingsProvider
	t.Cleanup(func() {
		systemSettingsProvider = orig
		invalidateMCPTokenCache()
	})

	systemSettingsProvider = &mcpTokenCheckSettingsProvider{enabled: false, token: "tok"}
	invalidateMCPTokenCache()
	if _, ok := MCPResolveToken("tok"); ok {
		t.Fatal("disabled MCP should reject a valid token")
	}

	systemSettingsProvider = &mcpTokenCheckSettingsProvider{enabled: true, token: "tok"}
	invalidateMCPTokenCache()
	access, ok := MCPResolveToken("tok")
	if !ok || !access.Legacy || access.AllowWrite {
		t.Fatalf("enabled MCP should accept the legacy read-only token, got %+v ok=%v", access, ok)
	}
}

func TestIssueMCPTokenReturnsCopyableEndpointAndEnforcesMode(t *testing.T) {
	gin.SetMode(gin.TestMode)
	orig := systemSettingsProvider
	origNotifier := mcpNotifier
	origConfig := mcpConfigReader
	t.Cleanup(func() {
		systemSettingsProvider = orig
		mcpNotifier = origNotifier
		mcpConfigReader = origConfig
		invalidateMCPTokenCache()
	})
	provider := &mcpTokenCheckSettingsProvider{enabled: true, allowWrite: true, token: "legacy-global-token"}
	systemSettingsProvider = provider
	invalidateMCPTokenCache()

	r := gin.New()
	r.POST("/api/mcp/tokens", postMCPIssueToken)
	r.GET("/api/mcp/config", getMCPConfig)

	writeBody := issueToken(t, r, map[string]any{
		"name": "外部助手", "mode": "readwrite",
		"notify": map[string]string{"channel": "email", "target": "ops@example.com"},
	})
	if !strings.Contains(writeBody.EndpointURL, "/mcp?token=") || !strings.Contains(writeBody.EndpointURL, writeBody.Token) {
		t.Fatalf("读写令牌没有返回带密钥的地址: %+v", writeBody)
	}
	readBody := issueToken(t, r, map[string]any{"name": "观察者", "mode": "readonly"})
	if strings.Contains(readBody.EndpointURL, "notify") {
		t.Fatal("只读地址不应带通知配置")
	}

	req := httptest.NewRequest(http.MethodGet, "/api/mcp/config", nil)
	req.Host = "quantmesh.example"
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if !strings.Contains(w.Body.String(), `"mode":"readwrite"`) || strings.Contains(w.Body.String(), writeBody.Token) {
		t.Fatalf("配置列表泄露了密钥或缺少令牌: %s", w.Body.String())
	}

	writeAccess, ok := MCPResolveToken(writeBody.Token)
	if !ok || !writeAccess.AllowWrite || writeAccess.Notify == nil || writeAccess.Notify.Target != "ops@example.com" {
		t.Fatalf("读写令牌解析错误: %+v ok=%v", writeAccess, ok)
	}
	readAccess, ok := MCPResolveToken(readBody.Token)
	if !ok || readAccess.AllowWrite || readAccess.Mode != mcp.AccessReadOnly {
		t.Fatalf("只读令牌解析错误: %+v", readAccess)
	}
	if _, ok := MCPResolveToken(writeBody.Token + "x"); ok {
		t.Fatal("错误密钥不应通过")
	}

	var notified []string
	mcpNotifier = func(_ *config.Config, channel, target, _, body string) error {
		notified = append(notified, channel+"|"+target+"|"+body)
		return nil
	}
	NotifyMCPWrites(context.Background(), mcp.WriteNotice{
		Access: writeAccess,
		Calls:  []mcp.WriteCall{{Tool: "qm_bot_set_enabled", Detail: "ok"}},
	})
	if len(notified) != 1 || !strings.Contains(notified[0], "email|ops@example.com") || strings.Contains(notified[0], writeBody.Token) {
		t.Fatalf("写操作通知不正确: %+v", notified)
	}
	NotifyMCPWrites(context.Background(), mcp.WriteNotice{Access: readAccess, Calls: []mcp.WriteCall{{Tool: "qm_get_bots"}}})
	if len(notified) != 1 {
		t.Fatal("只读令牌不应发通知")
	}
}

func TestIssueMCPTokenRejectsIncompleteWriteToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	orig := systemSettingsProvider
	t.Cleanup(func() {
		systemSettingsProvider = orig
		invalidateMCPTokenCache()
	})
	systemSettingsProvider = &mcpTokenCheckSettingsProvider{enabled: true}
	r := gin.New()
	r.POST("/api/mcp/tokens", postMCPIssueToken)

	for _, body := range []string{
		`{"name":"助手","mode":"readwrite"}`,
		`{"name":"助手","mode":"readwrite","notify":{"channel":"email","target":"not-an-email"}}`,
		`{"name":"","mode":"readonly"}`,
		`{"name":"助手","mode":"admin"}`,
	} {
		req := httptest.NewRequest(http.MethodPost, "/api/mcp/tokens", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("body %s status=%d response=%s", body, w.Code, w.Body.String())
		}
	}
}

type issuedMCPToken struct {
	Token       string `json:"token"`
	EndpointURL string `json:"endpoint_url"`
	Mode        string `json:"mode"`
}

func issueToken(t *testing.T, h http.Handler, payload map[string]any) issuedMCPToken {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/mcp/tokens", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Host = "quantmesh.example"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("issue status=%d body=%s", w.Code, w.Body.String())
	}
	var out issuedMCPToken
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out
}
