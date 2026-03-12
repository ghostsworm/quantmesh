package main

import (
	"fmt"
	"sync"
	"testing"

	"quantmesh/config"
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
