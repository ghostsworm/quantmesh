package web

import (
	"context"
	"testing"

	"quantmesh/storage"
)

type mcpTokenCheckSettingsProvider struct {
	enabled bool
	token   string
}

func (p *mcpTokenCheckSettingsProvider) GetSystemSettingBool(_ context.Context, key string, defaultValue bool) (bool, error) {
	if key == settingKeyMCPEnabled {
		return p.enabled, nil
	}
	return defaultValue, nil
}

func (p *mcpTokenCheckSettingsProvider) GetSystemSettings(_ context.Context, _ *storage.SystemSettingFilter) ([]*storage.SystemSetting, error) {
	return nil, nil
}

func (p *mcpTokenCheckSettingsProvider) GetSystemSetting(_ context.Context, key string) (*storage.SystemSetting, error) {
	if key == settingKeyMCPToken && p.token != "" {
		return &storage.SystemSetting{Key: key, Value: p.token}, nil
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
	origCache := mcpTokenCache
	t.Cleanup(func() {
		systemSettingsProvider = orig
		mcpTokenCache = origCache
	})

	systemSettingsProvider = &mcpTokenCheckSettingsProvider{enabled: false, token: "tok"}
	invalidateMCPTokenCache()
	if MCPTokenCheck("tok") {
		t.Fatal("disabled MCP should reject a valid token")
	}

	systemSettingsProvider = &mcpTokenCheckSettingsProvider{enabled: true, token: "tok"}
	invalidateMCPTokenCache()
	if !MCPTokenCheck("tok") {
		t.Fatal("enabled MCP should accept a valid token")
	}
}
