package main

import (
	"context"

	"quantmesh/config"
	"quantmesh/event"
	"quantmesh/lock"
	"quantmesh/storage"
)

// BotRuntime 代表單個 Bot 的運行時，封裝 SymbolRuntime 實現 Bot 級別的邏輯隔離
type BotRuntime struct {
	Config   config.BotConfig
	BotID    string
	Inner    *SymbolRuntime
	EventBus *event.EventBus
}

// BotManager 管理多個 BotRuntime，按 BotID 進行生命週期管理
type BotManager struct {
	cfg             *config.Config
	runtimes        map[string]*BotRuntime
	eventBus        *event.EventBus
	storageService  *storage.StorageService
	distributedLock lock.DistributedLock
}

// NewBotManager 創建 Bot 管理器
func NewBotManager(cfg *config.Config, eventBus *event.EventBus, storageService *storage.StorageService, distributedLock lock.DistributedLock) *BotManager {
	return &BotManager{
		cfg:             cfg,
		runtimes:        make(map[string]*BotRuntime),
		eventBus:        eventBus,
		storageService:  storageService,
		distributedLock: distributedLock,
	}
}

// StartBot 啟動指定 Bot
func (bm *BotManager) StartBot(ctx context.Context, botCfg config.BotConfig) (*BotRuntime, error) {
	botID := botCfg.ID
	if botID == "" {
		botID = config.GenerateBotID(botCfg.Exchange, botCfg.Symbol, botCfg.GetMarketType())
	}
	if _, ok := bm.runtimes[botID]; ok {
		return nil, nil // 已在運行
	}
	symCfg := config.BotConfigToSymbolConfig(botCfg)
	rt, err := startSymbolRuntime(ctx, bm.cfg, symCfg, bm.eventBus, bm.storageService, bm.distributedLock)
	if err != nil {
		return nil, err
	}
	br := &BotRuntime{
		Config:   botCfg,
		BotID:    botID,
		Inner:    rt,
		EventBus: bm.eventBus,
	}
	bm.runtimes[botID] = br
	return br, nil
}

// StopBot 停止指定 Bot
func (bm *BotManager) StopBot(botID string) error {
	br, ok := bm.runtimes[botID]
	if !ok {
		return nil
	}
	if br.Inner != nil && br.Inner.Stop != nil {
		br.Inner.Stop()
	}
	delete(bm.runtimes, botID)
	return nil
}

// Get 按 BotID 獲取運行時
func (bm *BotManager) Get(botID string) (*BotRuntime, bool) {
	br, ok := bm.runtimes[botID]
	return br, ok
}

// GetByExchangeSymbol 按交易所和交易對獲取運行時（兼容舊接口）
func (bm *BotManager) GetByExchangeSymbol(exchangeName, symbol string, marketType ...string) (*BotRuntime, bool) {
	mt := "futures"
	if len(marketType) > 0 && marketType[0] != "" {
		mt = marketType[0]
	}
	botID := config.GenerateBotID(exchangeName, symbol, mt)
	return bm.Get(botID)
}

// List 列出所有 Bot 運行時
func (bm *BotManager) List() []*BotRuntime {
	list := make([]*BotRuntime, 0, len(bm.runtimes))
	for _, br := range bm.runtimes {
		list = append(list, br)
	}
	return list
}

// Remove 從管理器中移除 Bot 運行時（會先停止 Bot）
func (bm *BotManager) Remove(botID string) {
	_ = bm.StopBot(botID)
}

// AddRuntime 註冊已創建的 BotRuntime（用於啟動時已有 SymbolRuntime 的向後兼容場景）
func (bm *BotManager) AddRuntime(br *BotRuntime) {
	if br == nil || br.BotID == "" {
		return
	}
	bm.runtimes[br.BotID] = br
}

// StopAll 停止所有 Bot
func (bm *BotManager) StopAll() {
	for _, br := range bm.runtimes {
		if br != nil && br.Inner != nil && br.Inner.Stop != nil {
			br.Inner.Stop()
		}
	}
	bm.runtimes = make(map[string]*BotRuntime)
}

// ListSymbolRuntimes 返回底層 SymbolRuntime 列表（供需要兼容 SymbolManager 的調用方使用）
func (bm *BotManager) ListSymbolRuntimes() []*SymbolRuntime {
	list := make([]*SymbolRuntime, 0, len(bm.runtimes))
	for _, br := range bm.runtimes {
		if br.Inner != nil {
			list = append(list, br.Inner)
		}
	}
	return list
}

// UpdateRuntimeTradingParams 更新運行中的 Bot 交易參數（熱更新）
func (bm *BotManager) UpdateRuntimeTradingParams(latestCfg *config.Config) (updatedBotIDs []string) {
	for _, botCfg := range latestCfg.Bots {
		botID := botCfg.ID
		if botID == "" {
			botID = config.GenerateBotID(botCfg.Exchange, botCfg.Symbol, botCfg.GetMarketType())
		}
		br, ok := bm.runtimes[botID]
		if !ok || br.Inner == nil || br.Inner.SuperPositionManager == nil {
			continue
		}
		symCfg := config.BotConfigToSymbolConfig(botCfg)
		changed := br.Inner.SuperPositionManager.UpdateTradingParams(
			symCfg.PriceInterval,
			symCfg.ProfitSpread,
			symCfg.OrderQuantity,
			symCfg.BuyWindowSize,
			symCfg.SellWindowSize,
		)
		if changed {
			br.Config = botCfg
			br.Inner.Config = symCfg
			updatedBotIDs = append(updatedBotIDs, botID)
		}
	}
	return
}

