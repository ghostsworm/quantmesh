package main

import (
	"fmt"
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
