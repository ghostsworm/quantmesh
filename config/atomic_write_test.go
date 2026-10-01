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

func TestBotConfigManagerRejectsPathTraversalIDs(t *testing.T) {
	root := t.TempDir()
	manager, err := NewBotConfigManager(root)
	if err != nil {
		t.Fatal(err)
	}
	outsideDir := filepath.Join(root, "outside")
	if err := os.MkdirAll(outsideDir, 0755); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(outsideDir, "keep.txt")
	if err := os.WriteFile(marker, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}

	for _, botID := range []string{"../outside", "..\\outside", "/tmp/outside", " binance:BTCUSDT:futures"} {
		if err := manager.DeleteBotConfig(botID); err == nil {
			t.Errorf("DeleteBotConfig(%q) unexpectedly accepted unsafe ID", botID)
		}
		if err := manager.SaveBotConfig(&BotConfigFile{BotID: botID}); err == nil {
			t.Errorf("SaveBotConfig(%q) unexpectedly accepted unsafe ID", botID)
		}
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("outside marker was affected: %v", err)
	}
	if manager.GetBotConfigPath("../outside") != "" || manager.GetBotDataPath("../outside") != "" {
		t.Fatal("path helper returned a path for an unsafe bot ID")
	}
	if err := ValidateBotConfigID("xtcom:zero0:spot"); err != nil {
		t.Fatalf("ordinary ID containing x/0 was rejected: %v", err)
	}
}
