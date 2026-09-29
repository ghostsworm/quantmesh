package web

import (
	"testing"
	"time"

	"quantmesh/config"
)

type equityScopeUpdateSpy struct {
	SymbolManagerProvider
	updated chan *config.Config
}

func (spy equityScopeUpdateSpy) UpdateEquityScopeConfig(cfg *config.Config) {
	spy.updated <- cfg
}

func TestConfigPersistenceNotifiesEquityScopeUpdater(t *testing.T) {
	restoreStorage := setupTestPrimaryAppConfigStorage(t)
	t.Cleanup(restoreStorage)
	previousProvider := symbolManagerProvider
	t.Cleanup(func() { symbolManagerProvider = previousProvider })
	spy := equityScopeUpdateSpy{updated: make(chan *config.Config, 1)}
	symbolManagerProvider = spy

	cfg := config.CreateMinimalConfig()
	cfg.App.CurrentExchange = "binance"
	cfg.Exchanges["binance"] = config.ExchangeConfig{APIKey: "test-key", SecretKey: "test-secret"}
	enabled := true
	cfg.Bots = []config.BotConfig{{
		ID:             "binance-btc-futures",
		Exchange:       "binance",
		Symbol:         "BTCUSDT",
		MarketType:     "futures",
		Enabled:        &enabled,
		PriceInterval:  100,
		OrderQuantity:  0.01,
		BuyWindowSize:  1,
		SellWindowSize: 1,
	}}
	manager := NewFileConfigManager("")
	if err := manager.UpdateConfig(cfg); err != nil {
		t.Fatalf("persist config: %v", err)
	}
	select {
	case updated := <-spy.updated:
		if len(updated.Bots) != 1 || updated.Bots[0].ID != cfg.Bots[0].ID {
			t.Fatalf("equity scope updater received stale config: %+v", updated.Bots)
		}
	case <-time.After(time.Second):
		t.Fatal("persisted config did not synchronize equity scope")
	}
}
