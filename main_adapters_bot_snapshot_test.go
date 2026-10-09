package main

import (
	"errors"
	"strings"
	"sync"
	"testing"

	"quantmesh/config"
	"quantmesh/web"
)

func TestSnapshotBotRuntimeConfigIsDetached(t *testing.T) {
	enabled := true
	runtime := &BotRuntime{BotID: "owner", Config: config.BotConfig{
		ID: "owner", Name: "before", Enabled: &enabled,
		Strategies: []config.StrategyInstance{{Type: "grid", Config: map[string]interface{}{
			"nested": []interface{}{"original"},
		}}},
	}}

	snapshot, botID, err := snapshotBotRuntimeConfig(runtime)
	if err != nil {
		t.Fatal(err)
	}
	if botID != "owner" {
		t.Fatalf("Bot ID = %q, want owner", botID)
	}
	snapshot.Name = "mutated"
	*snapshot.Enabled = false
	snapshot.Strategies[0].Config["nested"].([]interface{})[0] = "mutated"

	runtime.configMu.RLock()
	defer runtime.configMu.RUnlock()
	if runtime.Config.Name != "before" || !*runtime.Config.Enabled ||
		runtime.Config.Strategies[0].Config["nested"].([]interface{})[0] != "original" {
		t.Fatalf("API snapshot aliases runtime configuration: %+v", runtime.Config)
	}
}

func TestSnapshotBotRuntimeConfigSerializesAgainstHotUpdate(t *testing.T) {
	runtime := &BotRuntime{BotID: "owner", Config: config.BotConfig{
		ID: "owner", Name: "futures", MarketType: "futures",
		Strategies: []config.StrategyInstance{{Type: "futures"}},
	}}
	const iterations = 500
	start := make(chan struct{})
	var writer sync.WaitGroup
	writer.Add(1)
	go func() {
		defer writer.Done()
		<-start
		for i := 0; i < iterations; i++ {
			market := "spot"
			if i%2 == 0 {
				market = "futures"
			}
			runtime.configMu.Lock()
			runtime.Config.Name = market
			runtime.Config.MarketType = market
			runtime.Config.Strategies[0].Type = market
			runtime.configMu.Unlock()
		}
	}()

	close(start)
	for i := 0; i < iterations; i++ {
		snapshot, _, err := snapshotBotRuntimeConfig(runtime)
		if err != nil {
			t.Fatal(err)
		}
		if snapshot.Name != snapshot.MarketType || snapshot.Name != snapshot.Strategies[0].Type {
			t.Fatalf("mixed runtime config snapshot: name=%q market=%q strategy=%q",
				snapshot.Name, snapshot.MarketType, snapshot.Strategies[0].Type)
		}
	}
	writer.Wait()
}

func TestGetBotReturnsDetachedRuntimeConfigWithoutGlobalConfig(t *testing.T) {
	previousConfig := web.GetConfig()
	web.SetFileConfigManager(nil)
	t.Cleanup(func() {
		if previousConfig == nil {
			web.SetFileConfigManager(nil)
			return
		}
		manager := web.NewFileConfigManager("")
		if err := manager.SetRuntimeConfig(previousConfig); err != nil {
			t.Error(err)
			return
		}
		web.SetFileConfigManager(manager)
	})

	enabled := true
	botConfig := config.BotConfig{ID: "owner", Name: "runtime", Enabled: &enabled,
		Strategies: []config.StrategyInstance{{Type: "grid", Config: map[string]interface{}{
			"nested": []interface{}{"original"},
		}}},
	}
	botManager := NewBotManager(nil, nil, nil, nil, "")
	runtime := &BotRuntime{BotID: "owner", Config: botConfig, Inner: &SymbolRuntime{}}
	botManager.AddRuntime(runtime)
	botManager.recordStartFailure("owner", errors.New("signed request failed: signature=private-token"))
	adapter := &botManagerProviderAdapter{manager: &SymbolManager{botManager: botManager}}

	detail, ok := adapter.GetBot("owner")
	if !ok || detail.Config == nil {
		t.Fatalf("GetBot() = (%+v, %t)", detail, ok)
	}
	if detail.Testnet {
		t.Fatal("missing global config did not fall back to Bot testnet value")
	}
	if detail.LastStartError != botStartFailureCode || strings.Contains(detail.LastStartError, "private-token") {
		t.Fatalf("GetBot exposed underlying startup diagnostic: %q", detail.LastStartError)
	}
	detail.Config.Name = "mutated"
	detail.Config.Strategies[0].Config["nested"].([]interface{})[0] = "mutated"
	runtime.configMu.RLock()
	defer runtime.configMu.RUnlock()
	if runtime.Config.Name != "runtime" || runtime.Config.Strategies[0].Config["nested"].([]interface{})[0] != "original" {
		t.Fatalf("GetBot response aliases runtime config: %+v", runtime.Config)
	}
}
