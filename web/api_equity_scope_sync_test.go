package web

import (
	"context"
	"errors"
	"testing"
	"time"

	"quantmesh/config"
)

type equityScopeUpdateSpy struct {
	SymbolManagerProvider
	updated chan *config.Config
}

type equityScopePreparerSpy struct {
	SymbolManagerProvider
	err      error
	prepared int
	updated  int
}

func (spy *equityScopePreparerSpy) PrepareEquityScopeConfig(context.Context, *config.Config) error {
	spy.prepared++
	return spy.err
}

func (spy *equityScopePreparerSpy) UpdateEquityScopeConfig(*config.Config) {
	spy.updated++
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

func TestConfigMutationRequiresRetiredAccountArchiveBeforePersistence(t *testing.T) {
	restoreStorage := setupTestPrimaryAppConfigStorage(t)
	t.Cleanup(restoreStorage)
	previousProvider := symbolManagerProvider
	t.Cleanup(func() { symbolManagerProvider = previousProvider })
	preparer := &equityScopePreparerSpy{err: errors.New("archive unavailable")}
	symbolManagerProvider = preparer

	cfg := config.CreateMinimalConfig()
	cfg.App.CurrentExchange = "binance"
	cfg.Exchanges["binance"] = config.ExchangeConfig{APIKey: "account-key", SecretKey: "account-secret"}
	enabled := true
	cfg.Bots = []config.BotConfig{{ID: "binance-btc-futures", Exchange: "binance", Symbol: "BTCUSDT", MarketType: "futures",
		Enabled: &enabled, PriceInterval: 100, OrderQuantity: 0.01, BuyWindowSize: 1, SellWindowSize: 1}}
	manager := NewFileConfigManager("")
	if err := manager.UpdateConfig(cfg); err == nil {
		t.Fatal("config mutation persisted despite failed retired-account archival")
	}
	if preparer.prepared != 1 || preparer.updated != 0 {
		t.Fatalf("archive preflight/update sequence=%d/%d, want 1/0", preparer.prepared, preparer.updated)
	}
	if _, err := manager.GetConfig(); err == nil {
		t.Fatal("failed archive unexpectedly published the new configuration in memory")
	}
}
