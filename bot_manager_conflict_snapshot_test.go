package main

import (
	"sync"
	"testing"

	"quantmesh/config"
)

func TestBotConflictSnapshotConcurrentRuntimeHotUpdates(t *testing.T) {
	bm := NewBotManager(&config.Config{}, nil, nil, nil, "")
	owner := config.BotConfig{ID: "owner", Exchange: "binance", Symbol: "BTCUSDT", MarketType: "futures"}
	br := &BotRuntime{BotID: owner.ID, Config: owner, Inner: &SymbolRuntime{Config: config.BotConfigToSymbolConfig(owner), UpdateOpenControl: func(config.OpenPositionControl) error { return nil }}}
	bm.AddRuntime(br)
	var wg sync.WaitGroup
	start := make(chan struct{})
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-start
		for i := range 1000 {
			next := owner
			next.OpenPositionControl.PauseOpening = i%2 == 0
			if err := br.applyRuntimeTradingParams(next); err != nil {
				t.Errorf("normal hot update rejected: %v", err)
				return
			}
		}
	}()
	close(start)
	for range 1000 {
		if bm.findConflictingRuntime(&owner) != br {
			t.Error("registered financial scope disappeared during hot update")
			break
		}
	}
	wg.Wait()
}

func TestBotConflictSnapshotHotCallbackCanInspectManager(t *testing.T) {
	bm := NewBotManager(&config.Config{}, nil, nil, nil, "")
	owner := config.BotConfig{ID: "owner", Exchange: "binance", Symbol: "BTCUSDT", MarketType: "futures"}
	br := &BotRuntime{BotID: owner.ID, Config: owner, Inner: &SymbolRuntime{Config: config.BotConfigToSymbolConfig(owner)}}
	br.Inner.UpdateOpenControl = func(config.OpenPositionControl) error {
		if bm.findConflictingRuntime(&owner) != br {
			t.Error("hot callback lost registered scope")
		}
		return nil
	}
	bm.AddRuntime(br)
	if err := br.applyRuntimeTradingParams(owner); err != nil {
		t.Fatal(err)
	}
}

func TestBotConflictSnapshotRetainsOriginalSpreadFinancialLegs(t *testing.T) {
	bm := NewBotManager(&config.Config{}, nil, nil, nil, "")
	spread := &config.FundingPerpSpreadConfig{LegA: config.FundingPerpLeg{Exchange: "binance", Symbol: "BTCUSDT"}, LegB: config.FundingPerpLeg{Exchange: "okx", Symbol: "BTC-USDT-SWAP"}}
	br := &BotRuntime{BotID: "spread", Config: config.BotConfig{ID: "spread", MarketType: config.MarketTypeFundingPerpSpread, FundingPerpSpread: spread}}
	bm.AddRuntime(br)
	br.configMu.Lock()
	spread.LegA.Symbol = "ETHUSDT"
	br.configMu.Unlock()
	bm.AddRuntime(br) // Idempotent registration cannot redefine running financial legs.
	original := config.BotConfig{Exchange: "binance", Symbol: "BTCUSDT", MarketType: "futures"}
	if bm.findConflictingRuntime(&original) != br {
		t.Fatal("mutable config silently released original financial leg")
	}
	original.Symbol = "ETHUSDT"
	if bm.findConflictingRuntime(&original) != nil {
		t.Fatal("mutable config acquired an uninitialized replacement financial leg")
	}
}

func TestBotConflictSnapshotMissingRegistrationEvidenceCannotClaimUnrelated(t *testing.T) {
	br := &BotRuntime{BotID: "unknown"}
	bm := &BotManager{runtimes: map[string]*BotRuntime{"unknown": br}}
	candidate := config.BotConfig{Exchange: "binance", Symbol: "BTCUSDT"}
	if bm.findConflictingRuntime(&candidate) != br {
		t.Fatal("missing registered scope treated as unrelated financial runtime")
	}
}
