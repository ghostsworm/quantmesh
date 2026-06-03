package main

import (
	"context"
	"testing"

	"quantmesh/notify/aipipe"
	"quantmesh/notify/observability"
	"quantmesh/storage"
)

func TestLoadAipipeAndObservabilityConfigFromSettings(t *testing.T) {
	provider := &mainSettingsProvider{values: map[string]*storage.SystemSetting{
		aipipe.SettingKeyAPIKey:                   {Key: aipipe.SettingKeyAPIKey, Value: "api-key"},
		aipipe.SettingKeyEndpoint:                 {Key: aipipe.SettingKeyEndpoint, Value: "https://push.example/api"},
		aipipe.SettingKeyEnabled:                  {Key: aipipe.SettingKeyEnabled, Value: "true", Type: "boolean"},
		observability.SettingKeyPostHogProjectKey: {Key: observability.SettingKeyPostHogProjectKey, Value: "ph-key"},
		observability.SettingKeyPostHogHost:       {Key: observability.SettingKeyPostHogHost, Value: "https://posthog.example"},
		observability.SettingKeyPostHogEnabled:    {Key: observability.SettingKeyPostHogEnabled, Value: "true", Type: "boolean"},
		observability.SettingKeySentryDSN:         {Key: observability.SettingKeySentryDSN, Value: "https://public@sentry.example/1"},
		observability.SettingKeySentryEnabled:     {Key: observability.SettingKeySentryEnabled, Value: "true", Type: "boolean"},
		observability.SettingKeyEnvironment:       {Key: observability.SettingKeyEnvironment, Value: "staging"},
	}}

	pipeCfg := loadAipipeConfigFromSettings(provider)
	if pipeCfg.APIKey != "api-key" || pipeCfg.Endpoint != "https://push.example/api" || !pipeCfg.Enabled {
		t.Fatalf("aipipe config=%#v", pipeCfg)
	}

	obsCfg := loadObservabilityConfigFromSettings("9.8.7-rc1", provider)
	if obsCfg.PostHogProjectKey != "ph-key" || obsCfg.PostHogHost != "https://posthog.example" || !obsCfg.PostHogEnabled {
		t.Fatalf("posthog config=%#v", obsCfg)
	}
	if obsCfg.SentryDSN != "https://public@sentry.example/1" || !obsCfg.SentryEnabled {
		t.Fatalf("sentry config=%#v", obsCfg)
	}
	if obsCfg.Environment != "staging" || obsCfg.Release != "9.8.7-rc1" || obsCfg.DistinctID != "quantmesh-server" {
		t.Fatalf("observability metadata=%#v", obsCfg)
	}
}

func TestLoadConfigFromSettingsFallsBackOnMissingOrErrors(t *testing.T) {
	provider := &mainSettingsProvider{
		values: map[string]*storage.SystemSetting{
			aipipe.SettingKeyEndpoint:           {Key: aipipe.SettingKeyEndpoint, Value: ""},
			observability.SettingKeyPostHogHost: {Key: observability.SettingKeyPostHogHost, Value: ""},
		},
		errKeys: map[string]bool{
			aipipe.SettingKeyEnabled:              true,
			observability.SettingKeySentryEnabled: true,
		},
	}

	pipeCfg := loadAipipeConfigFromSettings(provider)
	if pipeCfg.Endpoint != aipipe.DefaultEndpoint || pipeCfg.Enabled {
		t.Fatalf("fallback aipipe config=%#v", pipeCfg)
	}

	obsCfg := loadObservabilityConfigFromSettings("1.0.0", provider)
	if obsCfg.PostHogHost != observability.DefaultPostHogHost {
		t.Fatalf("fallback posthog host=%s", obsCfg.PostHogHost)
	}
	if obsCfg.Environment != observability.DefaultEnvironment || obsCfg.SentryEnabled {
		t.Fatalf("fallback observability config=%#v", obsCfg)
	}
}

type mainSettingsProvider struct {
	values  map[string]*storage.SystemSetting
	errKeys map[string]bool
}

func (p *mainSettingsProvider) GetSystemSettingBool(_ context.Context, key string, defaultValue bool) (bool, error) {
	if p.errKeys[key] {
		return defaultValue, assertErr(key)
	}
	if setting, ok := p.values[key]; ok {
		return setting.Value == "true", nil
	}
	return defaultValue, nil
}

func (p *mainSettingsProvider) GetSystemSettings(context.Context, *storage.SystemSettingFilter) ([]*storage.SystemSetting, error) {
	result := make([]*storage.SystemSetting, 0, len(p.values))
	for _, setting := range p.values {
		result = append(result, setting)
	}
	return result, nil
}

func (p *mainSettingsProvider) GetSystemSetting(_ context.Context, key string) (*storage.SystemSetting, error) {
	if p.errKeys[key] {
		return nil, assertErr(key)
	}
	return p.values[key], nil
}

func (p *mainSettingsProvider) SetSystemSettingBool(_ context.Context, key string, value bool) error {
	p.values[key] = &storage.SystemSetting{Key: key, Value: map[bool]string{true: "true", false: "false"}[value], Type: "boolean"}
	return nil
}

func (p *mainSettingsProvider) SetSystemSettingString(_ context.Context, key, value string) error {
	p.values[key] = &storage.SystemSetting{Key: key, Value: value, Type: "string"}
	return nil
}

func (p *mainSettingsProvider) SaveSystemSetting(_ context.Context, key, value, settingType string) error {
	p.values[key] = &storage.SystemSetting{Key: key, Value: value, Type: settingType}
	return nil
}

func (p *mainSettingsProvider) DeleteSystemSetting(_ context.Context, key string) error {
	delete(p.values, key)
	return nil
}

type assertErr string

func (e assertErr) Error() string {
	return string(e)
}
