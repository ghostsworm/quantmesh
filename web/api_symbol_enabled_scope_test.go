package web

import (
	"testing"
	"time"

	"quantmesh/config"
)

type equityScopeReentrantSpy struct {
	SymbolManagerProvider
	manager *FileConfigManager
	checked chan error
}

func (spy equityScopeReentrantSpy) UpdateEquityScopeConfig(*config.Config) {
	_, err := spy.manager.GetConfig()
	spy.checked <- err
}

func TestSetSymbolEnabledNotifiesEquityScopeAfterUnlock(t *testing.T) {
	restoreStorage := setupTestPrimaryAppConfigStorage(t)
	t.Cleanup(restoreStorage)
	previousManager, previousProvider, previousReloader := fileConfigManager, symbolManagerProvider, configHotReloader
	t.Cleanup(func() {
		fileConfigManager = previousManager
		symbolManagerProvider = previousProvider
		configHotReloader = previousReloader
	})

	cfg := config.CreateMinimalConfig()
	cfg.App.CurrentExchange = "binance"
	cfg.Exchanges["binance"] = config.ExchangeConfig{APIKey: "test-key", SecretKey: "test-secret"}
	cfg.Trading.Symbols = []config.SymbolConfig{{
		Exchange:       "binance",
		Symbol:         "BTCUSDT",
		MarketType:     "futures",
		PriceInterval:  100,
		OrderQuantity:  0.01,
		BuyWindowSize:  1,
		SellWindowSize: 1,
	}}
	manager := NewFileConfigManager("")
	if err := manager.SetRuntimeConfig(cfg); err != nil {
		t.Fatalf("set runtime config: %v", err)
	}
	fileConfigManager = manager
	configHotReloader = nil
	spy := equityScopeReentrantSpy{manager: manager, checked: make(chan error, 1)}
	symbolManagerProvider = spy

	completed := make(chan error, 1)
	go func() { completed <- SetSymbolEnabled("binance", "BTCUSDT", false, "futures") }()
	select {
	case err := <-completed:
		if err != nil {
			t.Fatalf("set symbol enabled: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("SetSymbolEnabled blocked while notifying updater that re-enters GetConfig")
	}
	select {
	case err := <-spy.checked:
		if err != nil {
			t.Fatalf("updater GetConfig: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("equity scope updater was not called")
	}
}
