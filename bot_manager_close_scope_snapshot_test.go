package main

import (
	"sync"
	"testing"

	"quantmesh/config"
)

func TestClosePositionScopeSnapshotSerializesAgainstHotConfigReplacement(t *testing.T) {
	runtime := &BotRuntime{Config: config.BotConfig{Symbol: "BTCUSDT", MarketType: "spot"}}
	const iterations = 1000
	start := make(chan struct{})
	var writer sync.WaitGroup
	writer.Add(1)
	go func() {
		defer writer.Done()
		<-start
		for i := 0; i < iterations; i++ {
			symbol, marketType := "BTCUSDT", "spot"
			if i%2 == 1 {
				symbol, marketType = "ETHUSDT", "futures"
			}
			runtime.configMu.Lock()
			runtime.Config = config.BotConfig{Symbol: symbol, MarketType: marketType}
			runtime.configMu.Unlock()
		}
	}()

	close(start)
	for i := 0; i < iterations; i++ {
		symbol, marketType := runtime.closePositionScopeSnapshot()
		if (symbol == "BTCUSDT" && marketType != "spot") || (symbol == "ETHUSDT" && marketType != "futures") {
			t.Fatalf("mixed close scope snapshot: symbol=%q market=%q", symbol, marketType)
		}
	}
	writer.Wait()
}
