package cfgmgr

import (
	"context"
	"fmt"
	"testing"

	"quantmesh/config"
	"quantmesh/storage"
)

type fakeConfigStorage struct {
	entries map[string]*storage.ConfigEntry
	history []*storage.ConfigHistory
}

func newFakeConfigStorage() *fakeConfigStorage {
	return &fakeConfigStorage{entries: make(map[string]*storage.ConfigEntry)}
}

func configKey(scope storage.ConfigScope, scopeID, key string) string {
	return fmt.Sprintf("%s:%s:%s", scope, scopeID, key)
}

func (s *fakeConfigStorage) GetConfig(ctx context.Context, scope storage.ConfigScope, scopeID, key string) (*storage.ConfigEntry, error) {
	return s.entries[configKey(scope, scopeID, key)], nil
}

func (s *fakeConfigStorage) GetConfigByKeys(ctx context.Context, scope storage.ConfigScope, scopeID string, keys []string) ([]*storage.ConfigEntry, error) {
	var entries []*storage.ConfigEntry
	for _, key := range keys {
		if entry := s.entries[configKey(scope, scopeID, key)]; entry != nil {
			entries = append(entries, entry)
		}
	}
	return entries, nil
}

func (s *fakeConfigStorage) GetConfigsByScope(ctx context.Context, scope storage.ConfigScope, scopeID string) ([]*storage.ConfigEntry, error) {
	var entries []*storage.ConfigEntry
	for _, entry := range s.entries {
		if entry.Scope == scope && entry.ScopeID == scopeID {
			entries = append(entries, entry)
		}
	}
	return entries, nil
}

func (s *fakeConfigStorage) GetConfigsByCategory(ctx context.Context, scope storage.ConfigScope, category string) ([]*storage.ConfigEntry, error) {
	var entries []*storage.ConfigEntry
	for _, entry := range s.entries {
		if entry.Scope == scope && entry.Category == category {
			entries = append(entries, entry)
		}
	}
	return entries, nil
}

func (s *fakeConfigStorage) GetAllConfigs(ctx context.Context) ([]*storage.ConfigEntry, error) {
	var entries []*storage.ConfigEntry
	for _, entry := range s.entries {
		entries = append(entries, entry)
	}
	return entries, nil
}

func (s *fakeConfigStorage) SetConfig(ctx context.Context, entry *storage.ConfigEntry, updatedBy string) error {
	entry.UpdatedBy = updatedBy
	entry.Version++
	s.entries[configKey(entry.Scope, entry.ScopeID, entry.Key)] = entry
	return nil
}

func (s *fakeConfigStorage) SetConfigs(ctx context.Context, entries []*storage.ConfigEntry, updatedBy string) error {
	for _, entry := range entries {
		if err := s.SetConfig(ctx, entry, updatedBy); err != nil {
			return err
		}
	}
	return nil
}

func (s *fakeConfigStorage) DeleteConfig(ctx context.Context, scope storage.ConfigScope, scopeID, key string) error {
	delete(s.entries, configKey(scope, scopeID, key))
	return nil
}

func (s *fakeConfigStorage) GetConfigHistory(ctx context.Context, configID int64, limit int) ([]*storage.ConfigHistory, error) {
	return s.history, nil
}

func (s *fakeConfigStorage) GetConfigHistoryByKey(ctx context.Context, scope storage.ConfigScope, scopeID, key string, limit int) ([]*storage.ConfigHistory, error) {
	return s.history, nil
}

func (s *fakeConfigStorage) InitializeConfigs(ctx context.Context, entries []*storage.ConfigEntry) error {
	for _, entry := range entries {
		s.entries[configKey(entry.Scope, entry.ScopeID, entry.Key)] = entry
	}
	return nil
}

func (s *fakeConfigStorage) ValidateConfig(entry *storage.ConfigEntry) error {
	return nil
}

type recordingWatcher struct {
	events []watchEvent
}

type watchEvent struct {
	scope    storage.ConfigScope
	scopeID  string
	key      string
	oldValue interface{}
	newValue interface{}
}

func (w *recordingWatcher) OnConfigChanged(scope storage.ConfigScope, scopeID, key string, oldValue, newValue interface{}) {
	w.events = append(w.events, watchEvent{scope: scope, scopeID: scopeID, key: key, oldValue: oldValue, newValue: newValue})
}

func TestConfigManagerDefaultsAndTypedGetters(t *testing.T) {
	cfg := &config.Config{}
	cfg.Notifications.Enabled = true
	cfg.Notifications.Rules.OrderFilled = true
	cfg.Trading.PositionSafetyCheck = 7
	cfg.System.LogLevel = "DEBUG"
	cfg.Bots = []config.BotConfig{
		{
			ID:                  "bot-1",
			Exchange:            "binance",
			Symbol:              "BTCUSDT",
			MarketType:          "futures",
			Enabled:             config.BoolPtr(true),
			PriceInterval:       12.5,
			ProfitSpread:        4.5,
			PositionSafetyCheck: 6,
		},
	}

	manager := NewConfigManager(cfg, newFakeConfigStorage())

	if got := manager.GetBool(storage.ScopeGlobal, "", "notifications.enabled"); !got {
		t.Fatal("expected notifications.enabled default to be true")
	}
	if got := manager.GetString(storage.ScopeGlobal, "", "system.log_level"); got != "DEBUG" {
		t.Fatalf("system.log_level = %q, want DEBUG", got)
	}
	if got := manager.GetInt(storage.ScopeGlobal, "", "trading.position_safety_check"); got != 7 {
		t.Fatalf("position_safety_check = %d, want 7", got)
	}
	if got := manager.GetFloat64(storage.ScopeBot, "bot-1", "price_interval"); got != 12.5 {
		t.Fatalf("bot price_interval = %v, want 12.5", got)
	}
	if got := manager.GetWithDefault(storage.ScopeBot, "missing", "enabled", "fallback"); got != "fallback" {
		t.Fatalf("missing default = %#v, want fallback", got)
	}
}

func TestConfigManagerSetDeleteAndWatchers(t *testing.T) {
	cfg := &config.Config{}
	cfg.Notifications.Enabled = true
	store := newFakeConfigStorage()
	manager := NewConfigManager(cfg, store)
	watcher := &recordingWatcher{}
	manager.AddWatcher(watcher)

	if err := manager.Set(storage.ScopeGlobal, "", "notifications.enabled", false, "tester"); err != nil {
		t.Fatalf("Set failed: %v", err)
	}
	if got := manager.GetBool(storage.ScopeGlobal, "", "notifications.enabled"); got {
		t.Fatal("expected stored notifications.enabled to be false")
	}
	if len(watcher.events) != 1 {
		t.Fatalf("expected 1 watcher event, got %d", len(watcher.events))
	}
	if watcher.events[0].oldValue != true || watcher.events[0].newValue != false {
		t.Fatalf("unexpected set watcher event: %#v", watcher.events[0])
	}

	if err := manager.Delete(storage.ScopeGlobal, "", "notifications.enabled", "tester"); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}
	if got := manager.GetBool(storage.ScopeGlobal, "", "notifications.enabled"); !got {
		t.Fatal("expected deleted config to fall back to true")
	}
	if len(watcher.events) != 2 {
		t.Fatalf("expected 2 watcher events, got %d", len(watcher.events))
	}
	if watcher.events[1].oldValue != false || watcher.events[1].newValue != true {
		t.Fatalf("unexpected delete watcher event: %#v", watcher.events[1])
	}
}

func TestConfigManagerConfigsByScopeAndHistory(t *testing.T) {
	store := newFakeConfigStorage()
	entry, err := storage.NewConfigEntry(storage.ScopeBot, "bot-1", "enabled", true, "bot", "Enabled", "")
	if err != nil {
		t.Fatalf("NewConfigEntry failed: %v", err)
	}
	if err := store.SetConfig(context.Background(), entry, "tester"); err != nil {
		t.Fatalf("SetConfig failed: %v", err)
	}
	store.history = []*storage.ConfigHistory{{Key: "enabled"}}

	manager := NewConfigManager(&config.Config{}, store)
	values, err := manager.GetConfigsByScope(storage.ScopeBot, "bot-1")
	if err != nil {
		t.Fatalf("GetConfigsByScope failed: %v", err)
	}
	if values["enabled"] != true {
		t.Fatalf("enabled scope value = %#v, want true", values["enabled"])
	}

	history, err := manager.GetConfigHistory(storage.ScopeBot, "bot-1", "enabled", 10)
	if err != nil {
		t.Fatalf("GetConfigHistory failed: %v", err)
	}
	if len(history) != 1 || history[0].Key != "enabled" {
		t.Fatalf("unexpected history: %#v", history)
	}
}

func TestConfigManagerRejectsUnknownScopeDefault(t *testing.T) {
	manager := NewConfigManager(&config.Config{}, newFakeConfigStorage())
	if _, err := manager.Get(storage.ScopeExchange, "", "missing"); err == nil {
		t.Fatal("expected unknown scope default to fail")
	}
}
