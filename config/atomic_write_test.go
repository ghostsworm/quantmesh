package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSaveConfigWithoutValidationWritesAtomicallyWithPrivatePerms(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	cfg := CreateMinimalConfig()

	if err := SaveConfigWithoutValidation(cfg, path); err != nil {
		t.Fatalf("SaveConfigWithoutValidation failed: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat config: %v", err)
	}
	if got := info.Mode().Perm(); got != 0600 {
		t.Fatalf("expected private config perms 0600, got %o", got)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read temp dir: %v", err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".config.yaml.tmp-") {
			t.Fatalf("temporary config file was not cleaned up: %s", entry.Name())
		}
	}
}

func TestSaveBotConfigWritesAtomicallyWithPrivatePerms(t *testing.T) {
	dir := t.TempDir()
	manager, err := NewBotConfigManager(dir)
	if err != nil {
		t.Fatalf("NewBotConfigManager failed: %v", err)
	}
	cfg := &BotConfigFile{
		BotID:      "binance_btcusdt_futures",
		Exchange:   "binance",
		Symbol:     "BTCUSDT",
		MarketType: "futures",
		Strategies: []BotStrategyConfig{
			{Type: "grid", Enabled: true},
		},
	}

	if err := manager.SaveBotConfig(cfg); err != nil {
		t.Fatalf("SaveBotConfig failed: %v", err)
	}

	path := manager.GetBotConfigPath(cfg.BotID)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat bot config: %v", err)
	}
	if got := info.Mode().Perm(); got != 0600 {
		t.Fatalf("expected private bot config perms 0600, got %o", got)
	}
}
