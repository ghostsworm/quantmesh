package main

import (
	"context"
	"fmt"
	"strings"

	"quantmesh/config"
	"quantmesh/event"
	"quantmesh/lock"
	"quantmesh/logger"
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

// symbolKey 返回交易所+交易對+市場類型的標準化 key，用於衝突檢測
func symbolKey(exchange, symbol, marketType string) string {
	return strings.ToLower(fmt.Sprintf("%s:%s:%s", exchange, symbol, marketType))
}

// findConflictingBot 檢查是否已有同一交易所+交易對+市場類型的 Bot 在運行
// 同交易所同幣同市場類型的多個 Bot 會共享交易所的同一個倉位，
// 導致倉位重複認領、訂單互相干擾、對賬互相覆蓋等嚴重問題。
func (bm *BotManager) findConflictingBot(exchange, symbol, marketType string) *BotRuntime {
	targetKey := symbolKey(exchange, symbol, marketType)
	for _, br := range bm.runtimes {
		existingKey := symbolKey(br.Config.Exchange, br.Config.Symbol, br.Config.GetMarketType())
		if existingKey == targetKey {
			return br
		}
	}
	return nil
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

	// 🔒 衝突檢測：阻止同一交易所+交易對+市場類型啟動多個 Bot
	// 原因：交易所倉位按 Symbol 隔離，多個 Bot 無法區分誰擁有哪部分倉位，
	// 會導致倉位重複認領、訂單互撞、對賬覆蓋等問題。
	if conflict := bm.findConflictingBot(botCfg.Exchange, botCfg.Symbol, botCfg.GetMarketType()); conflict != nil {
		logger.Warn("🚫 [%s] 跳過啟動：同一交易對 %s:%s(%s) 已有 Bot [%s] 在運行。"+
			"交易所倉位按幣種隔離，多個 Bot 共享同一倉位會導致互相衝突。"+
			"如需多策略，請在同一 Bot 內配置多個 strategies。",
			botID, botCfg.Exchange, botCfg.Symbol, botCfg.GetMarketType(), conflict.BotID)
		return nil, fmt.Errorf("symbol_conflict: %s:%s(%s) 已有 Bot [%s] 在運行",
			botCfg.Exchange, botCfg.Symbol, botCfg.GetMarketType(), conflict.BotID)
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

