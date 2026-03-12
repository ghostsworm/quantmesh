package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"quantmesh/config"
	"quantmesh/event"
)

// TestBotManagerConcurrentAccessNoPanic 驗證並發讀寫 runtimes 不會觸發 map 競態崩潰
func TestBotManagerConcurrentAccessNoPanic(t *testing.T) {
	bm := &BotManager{
		runtimes: make(map[string]*BotRuntime),
	}

	var wg sync.WaitGroup

	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				botID := fmt.Sprintf("bot-%d-%d", i, j)
				bm.AddRuntime(&BotRuntime{
					BotID:  botID,
					Config: config.BotConfig{ID: botID, Exchange: "binance", Symbol: "BTCUSDT", MarketType: "futures"},
				})
			}
		}(i)
	}

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 600; j++ {
				_ = bm.List()
			}
		}()
	}

	wg.Wait()
}

func TestBotManagerWarnsOnSingleLegRunning(t *testing.T) {
	eb := event.NewEventBus(64)
	sub := eb.Subscribe()
	defer eb.Unsubscribe(sub)

	cfg := &config.Config{
		BotGroups: []config.BotGroup{
			{ID: "g1", Name: "hedge-btc", BotIDs: []string{"fut-bot", "spot-bot"}},
		},
	}
	bm := &BotManager{
		cfg:              cfg,
		runtimes:         make(map[string]*BotRuntime),
		eventBus:         eb,
		groupLegAlerted:  make(map[string]bool),
		groupLegTimers:   make(map[string]*time.Timer),
	}

	bm.AddRuntime(&BotRuntime{BotID: "fut-bot", Config: config.BotConfig{ID: "fut-bot", Exchange: "binance", Symbol: "BTCUSDT", MarketType: "futures"}})
	bm.AddRuntime(&BotRuntime{BotID: "spot-bot", Config: config.BotConfig{ID: "spot-bot", Exchange: "binance", Symbol: "BTCUSDT", MarketType: "spot"}})

	// 从双腿运行切到单腿运行，应触发告警
	if err := bm.StopBot("spot-bot"); err != nil {
		t.Fatalf("StopBot failed: %v", err)
	}

	gotAlert := false
	timeout := time.After(2 * time.Second)
	for !gotAlert {
		select {
		case evt := <-sub:
			if evt != nil && evt.Type == event.EventTypeError {
				if groupID, ok := evt.Data["group_id"].(string); ok && groupID == "g1" {
					gotAlert = true
				}
			}
		case <-timeout:
			t.Fatalf("expected single-leg warning event, but not received")
		}
	}

	// 恢复双腿运行，应触发恢复提示
	bm.AddRuntime(&BotRuntime{BotID: "spot-bot", Config: config.BotConfig{ID: "spot-bot", Exchange: "binance", Symbol: "BTCUSDT", MarketType: "spot"}})
	bm.checkGroupLegConsistencyForBot("spot-bot")

	gotRecovered := false
	timeout2 := time.After(2 * time.Second)
	for !gotRecovered {
		select {
		case evt := <-sub:
			if evt != nil && evt.Type == event.EventTypeRiskRecovered {
				if groupID, ok := evt.Data["group_id"].(string); ok && groupID == "g1" {
					gotRecovered = true
				}
			}
		case <-timeout2:
			t.Fatalf("expected hedge group recovered event, but not received")
		}
	}
}

func TestBotManagerAutoPausesSingleLegAfterGrace(t *testing.T) {
	eb := event.NewEventBus(64)
	sub := eb.Subscribe()
	defer eb.Unsubscribe(sub)

	cfg := &config.Config{
		BotGroups: []config.BotGroup{
			{ID: "g2", Name: "hedge-eth", BotIDs: []string{"fut-bot2", "spot-bot2"}},
		},
	}
	bm := &BotManager{
		cfg:               cfg,
		runtimes:          make(map[string]*BotRuntime),
		eventBus:          eb,
		groupLegAlerted:   make(map[string]bool),
		groupLegTimers:    make(map[string]*time.Timer),
		singleLegGraceSec: 1,
	}

	fut := &BotRuntime{BotID: "fut-bot2", Config: config.BotConfig{ID: "fut-bot2", Exchange: "binance", Symbol: "ETHUSDT", MarketType: "futures"}}
	spot := &BotRuntime{BotID: "spot-bot2", Config: config.BotConfig{ID: "spot-bot2", Exchange: "binance", Symbol: "ETHUSDT", MarketType: "spot"}}
	bm.AddRuntime(fut)
	bm.AddRuntime(spot)

	// 触发单腿运行
	if err := bm.StopBot("spot-bot2"); err != nil {
		t.Fatalf("StopBot failed: %v", err)
	}

	gotTriggered := false
	timeout := time.After(3 * time.Second)
	for !gotTriggered {
		select {
		case evt := <-sub:
			if evt != nil && evt.Type == event.EventTypeRiskTriggered {
				if groupID, ok := evt.Data["group_id"].(string); ok && groupID == "g2" {
					gotTriggered = true
				}
			}
		case <-timeout:
			t.Fatalf("expected risk_triggered event for single leg auto-pause")
		}
	}

	fut.configMu.RLock()
	paused := fut.Config.OpenPositionControl.PauseOpening
	fut.configMu.RUnlock()
	if !paused {
		t.Fatalf("expected running leg to be paused after single-leg grace timeout")
	}
}

// TestBotManagerIsBotEnabledInDB_StorageUnavailable 驗證存儲不可用時保守返回禁用
func TestBotManagerIsBotEnabledInDB_StorageUnavailable(t *testing.T) {
	bm := &BotManager{storageService: nil}
	enabled, reason := bm.IsBotEnabledInDB("test-bot")
	if enabled {
		t.Fatalf("storage 不可用時應保守返回 enabled=false，got enabled=true")
	}
	if reason != "storage_unavailable" {
		t.Fatalf("expected reason=storage_unavailable, got %q", reason)
	}
}

// TestBotManagerBotStateFileFallback 驗證存儲不可用時從文件讀取已停止狀態
func TestBotManagerBotStateFileFallback(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bot_states.json")
	// 寫入已停止的 bot 狀態
	data := `{"stopped-bot":{"enabled":false,"reason":"用戶停止"}}`
	if err := os.WriteFile(path, []byte(data), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	bm := &BotManager{storageService: nil, botStatesFileOverride: path}
	enabled, found := bm.isBotEnabledFromFile("stopped-bot")
	if !found {
		t.Fatalf("應從文件讀取到 stopped-bot 的狀態")
	}
	if enabled {
		t.Fatalf("stopped-bot 應為 enabled=false")
	}

	// 不存在的 bot 應返回 found=false
	_, found2 := bm.isBotEnabledFromFile("nonexistent")
	if found2 {
		t.Fatalf("不存在的 bot 應返回 found=false")
	}
}
