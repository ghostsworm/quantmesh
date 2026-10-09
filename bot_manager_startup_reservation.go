package main

import (
	"fmt"
	"sync"

	"quantmesh/config"
)

func botConflictScopeSnapshot(cfg config.BotConfig) config.BotConfig {
	snapshot := config.BotConfig{ID: config.BotIDOrGenerate(cfg), Exchange: cfg.Exchange, Symbol: cfg.Symbol, MarketType: cfg.MarketType, UseSpotMargin: cfg.UseSpotMargin}
	if cfg.FundingPerpSpread != nil {
		spread := *cfg.FundingPerpSpread
		snapshot.FundingPerpSpread = &spread
	}
	return snapshot
}

// Reservations span external initialization through runtime registration. They
// reuse existing conflict policy, including both spread legs, rather than
// pretending that a post-initialization registration check undoes side effects.
func (bm *BotManager) reserveBotStartup(cfg config.BotConfig) (func(), error) {
	botID := config.BotIDOrGenerate(cfg)
	if bm == nil || botID == "" {
		return nil, fmt.Errorf("Bot startup reservation requires manager and identity")
	}
	// Only these fields are read by BotsConflict. Copy the pointer-owned spread
	// definition so callers cannot mutate a reservation via their config pointer.
	snapshot := botConflictScopeSnapshot(cfg)
	bm.runtimesMu.Lock()
	defer bm.runtimesMu.Unlock()
	if conflict := bm.findConflictingRuntimeUnlocked(&snapshot); conflict != nil {
		return nil, fmt.Errorf("symbol_conflict: runtime %s owns startup scope", conflict.BotID)
	}
	for id, pending := range bm.pendingStarts {
		if id == botID || config.BotsConflict(&pending, &snapshot) {
			return nil, fmt.Errorf("symbol_conflict: Bot %s initialization owns startup scope", id)
		}
	}
	if bm.pendingStarts == nil {
		bm.pendingStarts = make(map[string]config.BotConfig)
	}
	bm.pendingStarts[botID] = snapshot
	var once sync.Once
	return func() {
		once.Do(func() {
			bm.runtimesMu.Lock()
			delete(bm.pendingStarts, botID)
			bm.runtimesMu.Unlock()
		})
	}, nil
}
