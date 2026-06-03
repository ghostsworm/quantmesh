package web

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"

	"quantmesh/storage"
)

func TestStorageSystemSettingsAdapterDelegatesAndJSONFallbacks(t *testing.T) {
	ctx := context.Background()
	store, err := storage.NewSQLStorage(filepath.Join(t.TempDir(), "settings.db"))
	if err != nil {
		t.Fatalf("new sql storage: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	if NewStorageSystemSettingsAdapter(nil) != nil {
		t.Fatalf("nil storage should not create adapter")
	}
	provider := NewStorageSystemSettingsAdapter(store)
	if provider == nil {
		t.Fatalf("sql storage should create adapter")
	}

	if err := provider.SetSystemSettingBool(ctx, "local_dev_mode", true); err != nil {
		t.Fatalf("set bool: %v", err)
	}
	if got, err := provider.GetSystemSettingBool(ctx, "local_dev_mode", false); err != nil || !got {
		t.Fatalf("get bool=%v err=%v", got, err)
	}
	if err := provider.SetSystemSettingString(ctx, "site_name", "QuantMesh"); err != nil {
		t.Fatalf("set string: %v", err)
	}
	if err := provider.SaveSystemSetting(ctx, "raw_json", `{"enabled":true,"limit":3}`, "json"); err != nil {
		t.Fatalf("save setting: %v", err)
	}
	var payload map[string]interface{}
	if err := GetSystemSettingJSONFromProvider(ctx, provider, "raw_json", &payload); err != nil {
		t.Fatalf("get json through provider extension: %v", err)
	}
	if payload["enabled"] != true || payload["limit"].(float64) != 3 {
		t.Fatalf("unexpected json payload: %#v", payload)
	}
	if err := SetSystemSettingJSONToProvider(ctx, provider, "typed_json", map[string]interface{}{"mode": "safe"}); err != nil {
		t.Fatalf("set json through provider extension: %v", err)
	}
	var typed map[string]string
	if err := GetSystemSettingJSONFromProvider(ctx, provider, "typed_json", &typed); err != nil {
		t.Fatalf("get typed json: %v", err)
	}
	if typed["mode"] != "safe" {
		t.Fatalf("typed json = %#v", typed)
	}

	list, err := provider.GetSystemSettings(ctx, nil)
	if err != nil || len(list) < 3 {
		t.Fatalf("settings list len=%d err=%v", len(list), err)
	}
	one, err := provider.GetSystemSetting(ctx, "site_name")
	if err != nil || one == nil || one.Value != "QuantMesh" {
		t.Fatalf("site setting=%#v err=%v", one, err)
	}
	if err := provider.DeleteSystemSetting(ctx, "site_name"); err != nil {
		t.Fatalf("delete setting: %v", err)
	}
	if deleted, _ := provider.GetSystemSetting(ctx, "site_name"); deleted != nil {
		t.Fatalf("deleted setting still present: %#v", deleted)
	}

	fallback := &fallbackJSONProvider{values: map[string]*storage.SystemSetting{
		"cfg": {Key: "cfg", Value: `{"threshold":2}`, Type: "json"},
	}}
	var cfg map[string]int
	if err := GetSystemSettingJSONFromProvider(ctx, fallback, "cfg", &cfg); err != nil {
		t.Fatalf("fallback get json: %v", err)
	}
	if cfg["threshold"] != 2 {
		t.Fatalf("fallback cfg=%#v", cfg)
	}
	if err := SetSystemSettingJSONToProvider(ctx, fallback, "next", map[string]string{"side": "long"}); err != nil {
		t.Fatalf("fallback set json: %v", err)
	}
	if fallback.values["next"].Type != "json" {
		t.Fatalf("fallback saved type=%s", fallback.values["next"].Type)
	}
	if err := SetSystemSettingJSONToProvider(ctx, fallback, "bad", func() {}); err == nil {
		t.Fatalf("unmarshalable json should fail")
	}
	if err := GetSystemSettingJSONFromProvider(ctx, fallback, "missing", &cfg); err == nil {
		t.Fatalf("missing fallback setting should fail")
	}
}

func TestStrategyMetadataHelpersCoverKnownAndUnknownStrategies(t *testing.T) {
	known := []string{"grid", "dca", "dca_enhanced", "martingale", "combo", "trend_following", "mean_reversion", "breakout"}
	for _, id := range known {
		if getStrategyName(id) == "" {
			t.Fatalf("strategy name empty for %s", id)
		}
		if getStrategyDescription(id) == "" {
			t.Fatalf("strategy description empty for %s", id)
		}
		if getStrategyType(id) == "" {
			t.Fatalf("strategy type empty for %s", id)
		}
		if tags := getStrategyTags(id); len(tags) == 0 {
			t.Fatalf("strategy tags empty for %s", id)
		}
		if features := getStrategyFeatures(id); len(features) == 0 {
			t.Fatalf("strategy features empty for %s", id)
		}
	}

	if getStrategyName("custom") != "custom" {
		t.Fatalf("unknown strategy name should echo id")
	}
	if getStrategyDescription("custom") != "策略描述" {
		t.Fatalf("unknown strategy description fallback mismatch")
	}
	if got := getStrategyTags("custom"); len(got) != 0 {
		t.Fatalf("unknown tags=%#v", got)
	}
	if got := getStrategyFeatures("custom"); !reflect.DeepEqual(got, []string{"基础功能"}) {
		t.Fatalf("unknown features=%#v", got)
	}
	if params := getStrategyParameters("grid"); len(params) != 4 || !params[0].Required {
		t.Fatalf("grid params=%#v", params)
	}
	if params := getStrategyParameters("dca_enhanced"); len(params) != 7 {
		t.Fatalf("dca enhanced params len=%d", len(params))
	}
	if params := getStrategyParameters("unknown"); len(params) != 0 {
		t.Fatalf("unknown params=%#v", params)
	}
	if !isStrategyPremium("trend_following") || isStrategyPremium("grid") {
		t.Fatalf("premium strategy classification mismatch")
	}
}

type fallbackJSONProvider struct {
	values map[string]*storage.SystemSetting
}

func (p *fallbackJSONProvider) GetSystemSettingBool(_ context.Context, key string, defaultValue bool) (bool, error) {
	if v, ok := p.values[key]; ok {
		return v.Value == "true", nil
	}
	return defaultValue, nil
}

func (p *fallbackJSONProvider) GetSystemSettings(context.Context, *storage.SystemSettingFilter) ([]*storage.SystemSetting, error) {
	result := make([]*storage.SystemSetting, 0, len(p.values))
	for _, setting := range p.values {
		result = append(result, setting)
	}
	return result, nil
}

func (p *fallbackJSONProvider) GetSystemSetting(_ context.Context, key string) (*storage.SystemSetting, error) {
	return p.values[key], nil
}

func (p *fallbackJSONProvider) SetSystemSettingBool(_ context.Context, key string, value bool) error {
	p.values[key] = &storage.SystemSetting{Key: key, Value: map[bool]string{true: "true", false: "false"}[value], Type: "boolean"}
	return nil
}

func (p *fallbackJSONProvider) SetSystemSettingString(_ context.Context, key, value string) error {
	p.values[key] = &storage.SystemSetting{Key: key, Value: value, Type: "string"}
	return nil
}

func (p *fallbackJSONProvider) SaveSystemSetting(_ context.Context, key, value, settingType string) error {
	p.values[key] = &storage.SystemSetting{Key: key, Value: value, Type: settingType}
	return nil
}

func (p *fallbackJSONProvider) DeleteSystemSetting(_ context.Context, key string) error {
	delete(p.values, key)
	return nil
}
