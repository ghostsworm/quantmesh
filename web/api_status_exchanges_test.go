package web

import (
	"testing"

	"quantmesh/config"
)

func TestEffectiveConfigForExchangeListPrefersFileConfigManager(t *testing.T) {
	origFCM := fileConfigManager
	origGlobal := globalConfig
	t.Cleanup(func() {
		fileConfigManager = origFCM
		globalConfig = origGlobal
	})

	globalConfig = &config.Config{
		Exchanges: map[string]config.ExchangeConfig{
			"binance": {},
		},
	}
	fcm := NewFileConfigManager("")
	fcm.SetRuntimeConfig(&config.Config{
		Exchanges: map[string]config.ExchangeConfig{
			"bitget": {APIKey: "k", SecretKey: "s", Passphrase: "p"},
		},
	})
	SetFileConfigManager(fcm)

	got := effectiveConfigForExchangeList()
	if got == nil {
		t.Fatal("expected non-nil config")
	}
	if _, ok := got.Exchanges["bitget"]; !ok {
		t.Fatal("expected bitget from FileConfigManager (Web 保存後的內存配置), not stale globalConfig")
	}
}
