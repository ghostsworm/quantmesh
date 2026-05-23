package web

import (
	"context"
	"path/filepath"
	"testing"

	"quantmesh/config"
	"quantmesh/storage"
)

func TestConfigMigrationBundleRoundTrip(t *testing.T) {
	originalFileConfigManager := fileConfigManager
	originalPrimaryStorage := primaryStorageForAppConfig
	originalBotConfigManager := botConfigManager
	originalHotReloader := configHotReloader
	originalSymbolProvider := symbolManagerProvider
	originalNewsSync := newsMonitorRuntimeSync
	defer func() {
		fileConfigManager = originalFileConfigManager
		primaryStorageForAppConfig = originalPrimaryStorage
		botConfigManager = originalBotConfigManager
		configHotReloader = originalHotReloader
		symbolManagerProvider = originalSymbolProvider
		newsMonitorRuntimeSync = originalNewsSync
	}()
	configHotReloader = nil
	symbolManagerProvider = nil
	newsMonitorRuntimeSync = nil

	sourceStorage, sourceConfig := newMigrationTestStorage(t)
	defer sourceStorage.Close()
	fileConfigManager = NewFileConfigManager("")
	fileConfigManager.SetRuntimeConfig(sourceConfig)
	primaryStorageForAppConfig = sourceStorage

	if _, err := storage.SaveAppConfigSnapshot(context.Background(), sourceStorage, sourceConfig, "test", "source"); err != nil {
		t.Fatal(err)
	}
	bundle, err := buildConfigMigrationBundle(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if bundle.Database == nil || bundle.Database.AppConfig == nil {
		t.Fatal("expected app_config in migration bundle")
	}
	if len(bundle.Database.BotConfigs) != 1 {
		t.Fatalf("expected 1 bot_config in migration bundle, got %d", len(bundle.Database.BotConfigs))
	}

	destStorage, _ := newMigrationTestStorage(t)
	defer destStorage.Close()
	fileConfigManager = NewFileConfigManager("")
	primaryStorageForAppConfig = destStorage
	var errManager error
	botConfigManager, errManager = config.NewBotConfigManager(t.TempDir())
	if errManager != nil {
		t.Fatal(errManager)
	}

	result, err := applyConfigMigrationBundle(context.Background(), bundle)
	if err != nil {
		t.Fatal(err)
	}
	if result.ImportedBotConfigs != 1 {
		t.Fatalf("expected 1 imported bot config, got %d", result.ImportedBotConfigs)
	}
	if result.WrittenBotConfigFiles != 1 {
		t.Fatalf("expected 1 written bot config file, got %d", result.WrittenBotConfigFiles)
	}

	appDoc, err := destStorage.GetAppConfigDocument(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if appDoc == nil || appDoc.Revision < 1 {
		t.Fatalf("expected imported app_config, got %+v", appDoc)
	}
	botDoc, err := destStorage.GetBotConfigDocument(context.Background(), "migration-bot")
	if err != nil {
		t.Fatal(err)
	}
	if botDoc == nil || botDoc.Revision < 1 {
		t.Fatalf("expected imported bot_config, got %+v", botDoc)
	}
	if _, err := botConfigManager.LoadBotConfig("migration-bot"); err != nil {
		t.Fatalf("expected local bot config file: %v", err)
	}
	if got := fileConfigManager.getCurrentConfig(); got == nil || got.App.CurrentExchange != "binance" {
		t.Fatalf("expected runtime config imported, got %+v", got)
	}
}

func newMigrationTestStorage(t *testing.T) (*storage.SQLStorage, *config.Config) {
	t.Helper()
	st, err := storage.NewSQLStorage(filepath.Join(t.TempDir(), "quantmesh.db"))
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadConfigFromBytes([]byte(`app:
  current_exchange: binance
exchanges:
  binance:
    api_key: "source-key"
    secret_key: "source-secret"
trading:
  symbols:
    - symbol: BTCUSDT
      exchange: binance
      price_interval: 100
      order_quantity: 10
      buy_window_size: 5
      sell_window_size: 5
`))
	if err != nil {
		t.Fatal(err)
	}
	enabled := true
	cfg.Bots = []config.BotConfig{{
		ID:                    "migration-bot",
		Name:                  "Migration Bot",
		Exchange:              "binance",
		Symbol:                "BTCUSDT",
		MarketType:            "futures",
		Enabled:               &enabled,
		Strategies:            []config.StrategyInstance{{Type: "grid", Weight: 1}},
		PriceInterval:         100,
		OrderQuantity:         10,
		MinOrderValue:         20,
		BuyWindowSize:         5,
		SellWindowSize:        5,
		ReconcileInterval:     60,
		OrderCleanupThreshold: 50,
		CleanupBatchSize:      10,
		MarginLockDurationSec: 10,
		PositionSafetyCheck:   config.DefaultPositionSafetyCheck,
		Direction:             "LONG",
	}}
	return st, cfg
}
