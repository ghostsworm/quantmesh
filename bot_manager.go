package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"quantmesh/config"
	"quantmesh/event"
	"quantmesh/lock"
	"quantmesh/logger"
	"quantmesh/position"
	"quantmesh/storage"
)

// BotRuntime 代表單個 Bot 的運行時，封裝 SymbolRuntime 實現 Bot 級別的邏輯隔離
type BotRuntime struct {
	Config   config.BotConfig
	BotID    string
	Inner    *SymbolRuntime
	EventBus *event.EventBus
	configMu sync.RWMutex // 保護 Config 的並發訪問
}

// BotManager 管理多個 BotRuntime，按 BotID 進行生命週期管理
type BotManager struct {
	cfg               *config.Config
	runtimes          map[string]*BotRuntime
	runtimesMu        sync.RWMutex
	groupLegAlerted   map[string]bool
	groupLegTimers    map[string]*time.Timer
	singleLegGraceSec int
	eventBus          *event.EventBus
	storageService    *storage.StorageService
	distributedLock   lock.DistributedLock
	botStatesFileOverride string // 測試用，空時用默認 ./data/bot_states.json
}

// NewBotManager 創建 Bot 管理器
func NewBotManager(cfg *config.Config, eventBus *event.EventBus, storageService *storage.StorageService, distributedLock lock.DistributedLock) *BotManager {
	return &BotManager{
		cfg:             cfg,
		runtimes:        make(map[string]*BotRuntime),
		groupLegAlerted: make(map[string]bool),
		groupLegTimers:  make(map[string]*time.Timer),
		singleLegGraceSec: 30,
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
	bm.runtimesMu.RLock()
	defer bm.runtimesMu.RUnlock()
	return bm.findConflictingBotLocked(exchange, symbol, marketType)
}

func (bm *BotManager) findConflictingBotLocked(exchange, symbol, marketType string) *BotRuntime {
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
	bm.runtimesMu.RLock()
	_, exists := bm.runtimes[botID]
	bm.runtimesMu.RUnlock()
	if exists {
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

	// 🔒 檢查數據庫中的啟停狀態（優先級高於配置文件）
	// 如果數據庫中標記為已禁用，則跳過啟動
	dbEnabled, reason := bm.isBotEnabledInDB(botID)
	if !dbEnabled {
		logger.Warn("🚫 [%s] 跳過啟動：數據庫中已禁用此 Bot（原因: %s）", botID, reason)
		// 发布启动失败事件（因被禁用）
		bm.eventBus.Publish(&event.Event{
			Type: event.EventTypeTradingStartFailed,
			Data: map[string]interface{}{
				"bot_id":   botID,
				"exchange": botCfg.Exchange,
				"symbol":   botCfg.Symbol,
				"error":    fmt.Sprintf("Bot 在數據庫中被禁用: %s", reason),
			},
		})
		return nil, fmt.Errorf("bot_disabled_in_database: %s", reason)
	}

	symCfg := config.BotConfigToSymbolConfig(botCfg)
	onRequestStop := func(botID string) {
		_ = bm.StopBotWithReason(botID, "close_condition", "關閉條件觸發")
	}
	rt, err := startSymbolRuntime(ctx, bm.cfg, symCfg, bm.eventBus, bm.storageService, bm.distributedLock, onRequestStop)
	if err != nil {
		// 发布启动失败事件
		bm.eventBus.Publish(&event.Event{
			Type: event.EventTypeTradingStartFailed,
			Data: map[string]interface{}{
				"bot_id":   botID,
				"exchange": botCfg.Exchange,
				"symbol":   botCfg.Symbol,
				"error":    err.Error(),
			},
		})
		return nil, err
	}
	br := &BotRuntime{
		Config:   botCfg,
		BotID:    botID,
		Inner:    rt,
		EventBus: bm.eventBus,
	}
	bm.runtimesMu.Lock()
	if _, ok := bm.runtimes[botID]; ok {
		bm.runtimesMu.Unlock()
		if rt != nil && rt.Stop != nil {
			rt.Stop()
		}
		return nil, nil
	}
	if conflict := bm.findConflictingBotLocked(botCfg.Exchange, botCfg.Symbol, botCfg.GetMarketType()); conflict != nil {
		bm.runtimesMu.Unlock()
		if rt != nil && rt.Stop != nil {
			rt.Stop()
		}
		return nil, fmt.Errorf("symbol_conflict: %s:%s(%s) 已有 Bot [%s] 在運行",
			botCfg.Exchange, botCfg.Symbol, botCfg.GetMarketType(), conflict.BotID)
	}
	bm.runtimes[botID] = br
	bm.runtimesMu.Unlock()

	// 发布启动成功事件
	bm.eventBus.Publish(&event.Event{
		Type: event.EventTypeTradingStarted,
		Data: map[string]interface{}{
			"bot_id":   botID,
			"exchange": botCfg.Exchange,
			"symbol":   botCfg.Symbol,
			"strategy": botCfg.Strategies,
		},
	})
	bm.checkGroupLegConsistencyForBot(botID)

	return br, nil
}

// StopBot 停止指定 Bot
func (bm *BotManager) StopBot(botID string) error {
	return bm.StopBotWithReason(botID, "web_ui", "用戶通過 Web UI 停止")
}

// StopBotWithReason 停止指定 Bot 並記錄原因（供關閉條件等自動停止場景使用）
func (bm *BotManager) StopBotWithReason(botID, updatedBy, reason string) error {
	bm.runtimesMu.Lock()
	br, ok := bm.runtimes[botID]
	if !ok {
		bm.runtimesMu.Unlock()
		return nil
	}
	delete(bm.runtimes, botID)
	bm.runtimesMu.Unlock()

	if br.Inner != nil && br.Inner.Stop != nil {
		br.Inner.Stop()
	}

	// 🔥 保存停止狀態到數據庫（持久化，重啟後仍然有效）
	bm.saveBotStateToDB(botID, false, updatedBy, reason)

	// 发布停止事件
	bm.eventBus.Publish(&event.Event{
		Type: event.EventTypeTradingStopped,
		Data: map[string]interface{}{
			"bot_id":   botID,
			"exchange": br.Config.Exchange,
			"symbol":   br.Config.Symbol,
			"strategy": br.Config.Strategies,
			"reason":   reason,
		},
	})
	bm.checkGroupLegConsistencyForBot(botID)

	return nil
}

// EnableBot 啟用 Bot（從數據庫移除禁用標記）
// 注意：這只是從數據庫移除禁用標記，不會立即啟動 Bot
// Bot 需要通過 StartBot 方法或在配置文件中 enabled=true 才會啟動
func (bm *BotManager) EnableBot(botID string) error {
	// 🔥 從數據庫中刪除禁用記錄（或設置為 enabled=true）
	bm.saveBotStateToDB(botID, true, "web_ui", "用戶通過 Web UI 啟用")

	logger.Info("✅ [%s] Bot 已在數據庫中標記為啟用，可以通過 StartBot 方法啟動", botID)
	return nil
}

// Get 按 BotID 獲取運行時
func (bm *BotManager) Get(botID string) (*BotRuntime, bool) {
	bm.runtimesMu.RLock()
	defer bm.runtimesMu.RUnlock()
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
	bm.runtimesMu.RLock()
	defer bm.runtimesMu.RUnlock()
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
	bm.runtimesMu.Lock()
	defer bm.runtimesMu.Unlock()
	if bm.groupLegAlerted == nil {
		bm.groupLegAlerted = make(map[string]bool)
	}
	bm.runtimes[br.BotID] = br
}

// StopAll 停止所有 Bot
func (bm *BotManager) StopAll() {
	bm.runtimesMu.Lock()
	runtimes := make([]*BotRuntime, 0, len(bm.runtimes))
	for _, br := range bm.runtimes {
		runtimes = append(runtimes, br)
	}
	bm.runtimes = make(map[string]*BotRuntime)
	bm.groupLegAlerted = make(map[string]bool)
	for _, timer := range bm.groupLegTimers {
		if timer != nil {
			timer.Stop()
		}
	}
	bm.groupLegTimers = make(map[string]*time.Timer)
	bm.runtimesMu.Unlock()
	for _, br := range runtimes {
		if br != nil && br.Inner != nil && br.Inner.Stop != nil {
			br.Inner.Stop()
		}
	}
}

func (bm *BotManager) checkGroupLegConsistencyForBot(botID string) {
	if bm == nil || bm.cfg == nil || botID == "" {
		return
	}
	for _, group := range bm.cfg.BotGroups {
		if len(group.BotIDs) < 2 || !containsBotID(group.BotIDs, botID) {
			continue
		}

		var runningIDs []string
		total := len(group.BotIDs)
		publishAlert := false
		publishRecovered := false

		bm.runtimesMu.Lock()
		if bm.groupLegAlerted == nil {
			bm.groupLegAlerted = make(map[string]bool)
		}
		for _, id := range group.BotIDs {
			if _, ok := bm.runtimes[id]; ok {
				runningIDs = append(runningIDs, id)
			}
		}
		running := len(runningIDs)
		alreadyAlerted := bm.groupLegAlerted[group.ID]
		if running > 0 && running < total {
			if !alreadyAlerted {
				bm.groupLegAlerted[group.ID] = true
				publishAlert = true
			}
			bm.scheduleSingleLegEnforcementLocked(group.ID)
		} else {
			if running == total && alreadyAlerted {
				publishRecovered = true
			}
			bm.groupLegAlerted[group.ID] = false
			bm.cancelSingleLegEnforcementLocked(group.ID)
		}
		bm.runtimesMu.Unlock()

		if publishAlert {
			logger.Warn("⚠️ [BotGroup:%s] 对冲组出现单腿运行，running=%v total=%d", group.ID, runningIDs, total)
			if bm.eventBus != nil {
				bm.eventBus.Publish(&event.Event{
					Type: event.EventTypeError,
					Data: map[string]interface{}{
						"group_id":         group.ID,
						"group_name":       group.Name,
						"issue":            "single_leg_running",
						"running_bot_ids":  runningIDs,
						"expected_bot_ids": group.BotIDs,
					},
				})
			}
		}
		if publishRecovered {
			logger.Info("✅ [BotGroup:%s] 对冲组双腿已恢复一致运行", group.ID)
			if bm.eventBus != nil {
				bm.eventBus.Publish(&event.Event{
					Type: event.EventTypeRiskRecovered,
					Data: map[string]interface{}{
						"group_id":        group.ID,
						"group_name":      group.Name,
						"issue":           "single_leg_running",
						"running_bot_ids": runningIDs,
					},
				})
			}
		}
	}
}

func (bm *BotManager) scheduleSingleLegEnforcementLocked(groupID string) {
	if bm.singleLegGraceSec <= 0 {
		return
	}
	if bm.groupLegTimers == nil {
		bm.groupLegTimers = make(map[string]*time.Timer)
	}
	if _, exists := bm.groupLegTimers[groupID]; exists {
		return
	}
	delay := time.Duration(bm.singleLegGraceSec) * time.Second
	bm.groupLegTimers[groupID] = time.AfterFunc(delay, func() {
		bm.enforceSingleLegPause(groupID)
	})
}

func (bm *BotManager) cancelSingleLegEnforcementLocked(groupID string) {
	if bm.groupLegTimers == nil {
		return
	}
	if timer, exists := bm.groupLegTimers[groupID]; exists {
		timer.Stop()
		delete(bm.groupLegTimers, groupID)
	}
}

func (bm *BotManager) enforceSingleLegPause(groupID string) {
	if bm == nil || bm.cfg == nil || groupID == "" {
		return
	}

	var targetGroup *config.BotGroup
	for i := range bm.cfg.BotGroups {
		if bm.cfg.BotGroups[i].ID == groupID {
			targetGroup = &bm.cfg.BotGroups[i]
			break
		}
	}
	if targetGroup == nil {
		bm.runtimesMu.Lock()
		bm.cancelSingleLegEnforcementLocked(groupID)
		bm.runtimesMu.Unlock()
		return
	}

	runningIDs := make([]string, 0, len(targetGroup.BotIDs))
	bm.runtimesMu.Lock()
	for _, id := range targetGroup.BotIDs {
		if _, ok := bm.runtimes[id]; ok {
			runningIDs = append(runningIDs, id)
		}
	}
	// 当前已经恢复（全运行）或全部停止，不执行自动暂停
	if len(runningIDs) == 0 || len(runningIDs) == len(targetGroup.BotIDs) {
		bm.cancelSingleLegEnforcementLocked(groupID)
		bm.runtimesMu.Unlock()
		return
	}
	bm.cancelSingleLegEnforcementLocked(groupID)
	bm.runtimesMu.Unlock()

	for _, id := range runningIDs {
		if br, ok := bm.Get(id); ok && br != nil {
			br.PauseOpening("single_leg_running")
		}
	}

	logger.Warn("🛑 [BotGroup:%s] 单腿运行超过 %d 秒，自动暂停开仓。running=%v", groupID, bm.singleLegGraceSec, runningIDs)
	if bm.eventBus != nil {
		bm.eventBus.Publish(&event.Event{
			Type: event.EventTypeRiskTriggered,
			Data: map[string]interface{}{
				"group_id":        groupID,
				"issue":           "single_leg_running",
				"action":          "pause_opening",
				"running_bot_ids": runningIDs,
			},
		})
	}
}

func containsBotID(botIDs []string, target string) bool {
	for _, id := range botIDs {
		if id == target {
			return true
		}
	}
	return false
}

// ListSymbolRuntimes 返回底層 SymbolRuntime 列表（供需要兼容 SymbolManager 的調用方使用）
func (bm *BotManager) ListSymbolRuntimes() []*SymbolRuntime {
	bm.runtimesMu.RLock()
	defer bm.runtimesMu.RUnlock()
	list := make([]*SymbolRuntime, 0, len(bm.runtimes))
	for _, br := range bm.runtimes {
		if br.Inner != nil {
			list = append(list, br.Inner)
		}
	}
	return list
}

// UpdateRuntimeTradingParams 更新運行中的 Bot 交易參數（熱更新）
// 始終同步 Config 到運行時，確保 smart_order 等非交易參數變更也能反映到 GetBot 返回的詳情中
func (bm *BotManager) UpdateRuntimeTradingParams(latestCfg *config.Config) (updatedBotIDs []string) {
	for _, botCfg := range latestCfg.Bots {
		botID := botCfg.ID
		if botID == "" {
			botID = config.GenerateBotID(botCfg.Exchange, botCfg.Symbol, botCfg.GetMarketType())
		}
		bm.runtimesMu.RLock()
		br, ok := bm.runtimes[botID]
		bm.runtimesMu.RUnlock()
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
		// 始終同步 Config，確保 smart_order、風控等配置變更在刷新頁面時正確顯示
		br.Config = botCfg
		br.Inner.Config = symCfg
		if changed {
			updatedBotIDs = append(updatedBotIDs, botID)
		}
	}
	return
}

// ClosePositions 平倉（支持市價/限價）
func (br *BotRuntime) ClosePositions(ctx context.Context, cfg config.ClosePositionConfig) (*position.ClosePositionRecord, error) {
	if br.Inner == nil || br.Inner.SuperPositionManager == nil {
		return nil, fmt.Errorf("bot not initialized")
	}

	// 獲取交易所
	exchange := br.Inner.Exchange
	if exchange == nil {
		return nil, fmt.Errorf("exchange not initialized")
	}

	// 創建平倉管理器
	closeMgr := position.NewClosePositionManager(
		position.NewExchangeAdapterWrapper(exchange),
		br.BotID,
		br.Config.Symbol,
	)

	// 獲取持倉
	_, err := exchange.GetPositions(ctx, br.Config.Symbol)
	if err != nil {
		return nil, fmt.Errorf("failed to get positions: %w", err)
	}

	// 平倉
	// 這裡需要根據實際的持倉情況來決定平倉方向
	// 暫時簡化處理，假設做多平倉為賣單
	record, err := closeMgr.ClosePositions(ctx, "SELL", 100, cfg) // 這裡需要實際數量
	if err != nil {
		return nil, err
	}

	return record, nil
}

// GetCloseRecords 獲取平倉記錄
func (br *BotRuntime) GetCloseRecords() []*position.ClosePositionRecord {
	// 暫時返回空列表，實際需要從平倉管理器獲取
	return []*position.ClosePositionRecord{}
}

// GetSlotFilter 獲取槽位過濾器
func (br *BotRuntime) GetSlotFilter() *config.SlotFilterConfig {
	if br.Inner == nil || br.Inner.SuperPositionManager == nil {
		return nil
	}
	return br.Inner.SuperPositionManager.GetSlotFilter()
}

// SetSlotFilter 設置槽位過濾器
func (br *BotRuntime) SetSlotFilter(filter *config.SlotFilterConfig) {
	if br.Inner == nil || br.Inner.SuperPositionManager == nil {
		return
	}
	br.Inner.SuperPositionManager.SetSlotFilter(filter)
}

// GetSlots 獲取所有槽位信息
func (br *BotRuntime) GetSlots() []map[string]interface{} {
	if br.Inner == nil || br.Inner.SuperPositionManager == nil {
		return []map[string]interface{}{}
	}

	slots := br.Inner.SuperPositionManager.GetAllSlotsDetailed()
	result := make([]map[string]interface{}, len(slots))
	for i, slot := range slots {
		result[i] = map[string]interface{}{
			"price":          slot.Price,
			"position_status": slot.PositionStatus,
			"position_qty":    slot.PositionQty,
			"order_id":        slot.OrderID,
			"order_side":      slot.OrderSide,
			"order_status":    slot.OrderStatus,
			"order_price":     slot.OrderPrice,
			"slot_status":     slot.SlotStatus,
		}
	}
	return result
}

// GetBotRiskControl 获取 Bot 风控配置
func (br *BotRuntime) GetBotRiskControl() *config.BotRiskControl {
	if br.Config.OpenPositionControl.BotRiskControl == nil {
		return &config.BotRiskControl{}
	}
	return br.Config.OpenPositionControl.BotRiskControl
}

// SetBotRiskControl 设置 Bot 风控配置
func (br *BotRuntime) SetBotRiskControl(riskControl *config.BotRiskControl) error {
	br.configMu.Lock()
	defer br.configMu.Unlock()

	if br.Config.OpenPositionControl.BotRiskControl == nil {
		br.Config.OpenPositionControl.BotRiskControl = &config.BotRiskControl{}
	}
	br.Config.OpenPositionControl.BotRiskControl = riskControl
	return nil
}

// GetGridRiskControl 獲取網格風控配置
func (br *BotRuntime) GetGridRiskControl() config.GridRiskControl {
	br.configMu.RLock()
	defer br.configMu.RUnlock()
	return br.Config.GridRiskControl
}

// SetGridRiskControl 設置網格風控配置（運行時熱更新 + 同步到 SuperPositionManager）
func (br *BotRuntime) SetGridRiskControl(grc config.GridRiskControl) error {
	br.configMu.Lock()
	defer br.configMu.Unlock()
	br.Config.GridRiskControl = grc
	if br.Inner != nil && br.Inner.SuperPositionManager != nil {
		br.Inner.SuperPositionManager.SetGridRiskControl(grc)
	}
	return nil
}

// PauseOpening 暂停开仓
func (br *BotRuntime) PauseOpening(reason string) {
	br.configMu.Lock()
	defer br.configMu.Unlock()

	// 更新 OpenPositionControl 中的 PauseOpening 状态
	br.Config.OpenPositionControl.PauseOpening = true

	// 同时更新 BotRiskControl 中的状态
	if br.Config.OpenPositionControl.BotRiskControl == nil {
		br.Config.OpenPositionControl.BotRiskControl = &config.BotRiskControl{}
	}
	br.Config.OpenPositionControl.BotRiskControl.PauseOpening = true
	br.Config.OpenPositionControl.BotRiskControl.PauseOpeningReason = reason
	br.Config.OpenPositionControl.BotRiskControl.Enabled = true

	// 🔥 如果设置了自动恢复时间，启动自动恢复 goroutine
	if br.Config.OpenPositionControl.BotRiskControl.AutoResumeAfter > 0 {
		autoResumeSec := br.Config.OpenPositionControl.BotRiskControl.AutoResumeAfter
		go br.autoResumeAfter(autoResumeSec)
	}
}

// ResumeOpening 恢复开仓
func (br *BotRuntime) ResumeOpening() {
	br.configMu.Lock()
	defer br.configMu.Unlock()

	// 更新 OpenPositionControl 中的 PauseOpening 状态
	br.Config.OpenPositionControl.PauseOpening = false

	// 同时更新 BotRiskControl 中的状态
	if br.Config.OpenPositionControl.BotRiskControl != nil {
		br.Config.OpenPositionControl.BotRiskControl.PauseOpening = false
		br.Config.OpenPositionControl.BotRiskControl.PauseOpeningReason = ""
	}
}

// GetPositionStatus 获取仓位状态（包括是否达到限制）
func (br *BotRuntime) GetPositionStatus() map[string]interface{} {
	if br.Inner == nil || br.Inner.SuperPositionManager == nil {
		return map[string]interface{}{
			"error": "bot not initialized",
		}
	}

	spm := br.Inner.SuperPositionManager

	// 使用公开方法获取仓位信息
	positionLayers := spm.GetActiveLayers()
	totalPositionValue := spm.GetTotalPositionValueUSDT()

	// 获取当前价格
	currentPrice := spm.GetLastMarketPrice()
	totalPositionQty := totalPositionValue / currentPrice

	// 获取杠杆并计算实际占用资金
	leverage := spm.GetLeverage()
	if leverage <= 0 {
		leverage = 1
	}
	actualMargin := totalPositionValue / float64(leverage)

	// 读取风控配置（使用读锁保护）
	br.configMu.RLock()
	riskControl := br.Config.OpenPositionControl.BotRiskControl
	openControl := br.Config.OpenPositionControl
	br.configMu.RUnlock()

	// 计算暂停状态（安全处理 nil 情况）
	paused := openControl.PauseOpening
	if riskControl != nil && riskControl.Enabled && riskControl.PauseOpening {
		paused = true
	}

	status := map[string]interface{}{
		"total_position_qty":    totalPositionQty,
		"total_position_value":  totalPositionValue,
		"total_actual_margin":   actualMargin,
		"leverage":              leverage,
		"position_layers":       positionLayers,
		"current_price":         currentPrice,
		"paused":                paused,
	}

	// 检查是否达到数量限制
	reachedLimitQty := false
	if riskControl != nil && riskControl.Enabled && riskControl.MaxPositionQuantity > 0 {
		status["max_position_qty"] = riskControl.MaxPositionQuantity
		reachedLimitQty = totalPositionQty >= riskControl.MaxPositionQuantity
	} else if openControl.MaxPositionQuantity > 0 {
		status["max_position_qty"] = openControl.MaxPositionQuantity
		reachedLimitQty = totalPositionQty >= openControl.MaxPositionQuantity
	}
	status["reached_limit_qty"] = reachedLimitQty

	// 检查是否达到价值限制（使用实际占用资金）
	reachedLimitValue := false
	if riskControl != nil && riskControl.Enabled && riskControl.MaxPositionValue > 0 {
		status["max_position_value"] = riskControl.MaxPositionValue
		reachedLimitValue = actualMargin >= riskControl.MaxPositionValue
	} else if openControl.MaxPositionValue > 0 {
		status["max_position_value"] = openControl.MaxPositionValue
		reachedLimitValue = actualMargin >= openControl.MaxPositionValue
	}
	status["reached_limit_value"] = reachedLimitValue

	// 检查是否达到层数限制
	reachedLimitLayers := false
	if riskControl != nil && riskControl.Enabled && riskControl.MaxPositionLayers > 0 {
		status["max_position_layers"] = riskControl.MaxPositionLayers
		reachedLimitLayers = positionLayers >= riskControl.MaxPositionLayers
	} else if openControl.MaxPositionLayers > 0 {
		status["max_position_layers"] = openControl.MaxPositionLayers
		reachedLimitLayers = positionLayers >= openControl.MaxPositionLayers
	}
	status["reached_limit_layers"] = reachedLimitLayers

	// 是否应该停止开仓
	status["should_stop_opening"] = reachedLimitQty || reachedLimitValue || reachedLimitLayers || paused

	return status
}

// CancelAllOpenOrders 取消所有开仓订单
func (br *BotRuntime) CancelAllOpenOrders() error {
	if br.Inner == nil || br.Inner.SuperPositionManager == nil {
		return fmt.Errorf("bot not initialized")
	}
	br.Inner.SuperPositionManager.CancelAllOpenOrders()
	return nil
}

// CloseAllPositions 平掉所有仓位
func (br *BotRuntime) CloseAllPositions(ctx context.Context, method string, timeout int) error {
	if br.Inner == nil || br.Inner.SuperPositionManager == nil {
		return fmt.Errorf("bot not initialized")
	}
	spm := br.Inner.SuperPositionManager

	// 获取当前价格以确定平仓方向
	currentPrice := spm.GetLastMarketPrice()
	if currentPrice == 0 {
		return fmt.Errorf("unable to get current price")
	}

	// 确定平仓方向
	var side string
	if br.Config.GetDirection() == "SHORT" {
		side = "BUY"  // 做空平仓是买入
	} else {
		side = "SELL" // 做多平仓是卖出
	}

	// 获取总持仓价值
	totalValue := spm.GetTotalPositionValueUSDT()
	if totalValue == 0 {
		return nil // 没有持仓
	}

	// 计算需要平仓的数量
	totalQty := totalValue / currentPrice

	// 创建平仓配置
	cfg := config.ClosePositionConfig{
		Method:     method,
		TimeoutSec: timeout,
		AutoRetry:  timeout > 0,
		MaxRetries: 3,
	}

	// 创建平仓管理器
	exchange := br.Inner.Exchange
	closeMgr := position.NewClosePositionManager(
		position.NewExchangeAdapterWrapper(exchange),
		br.BotID,
		br.Config.Symbol,
	)

	// 执行平仓
	_, err := closeMgr.ClosePositions(ctx, side, totalQty, cfg)
	return err
}

// GetPositionSummary 获取仓位摘要信息
func (br *BotRuntime) GetPositionSummary() (float64, float64, error) {
	if br.Inner == nil || br.Inner.SuperPositionManager == nil {
		return 0, 0, fmt.Errorf("bot not initialized")
	}

	spm := br.Inner.SuperPositionManager
	currentPrice := spm.GetLastMarketPrice()
	if currentPrice == 0 {
		return 0, 0, fmt.Errorf("unable to get current price")
	}

	// 获取未实现盈亏
	unrealizedPnL := spm.GetUnrealizedPnL(currentPrice)

	// 获取总持仓价值
	totalValue := spm.GetTotalPositionValueUSDT()

	return unrealizedPnL, totalValue, nil
}


// autoResumeAfter 在指定秒数后自动恢复开仓
func (br *BotRuntime) autoResumeAfter(seconds int) {
	if seconds <= 0 {
		return
	}

	logger.Info("⏰ [%s] 自动恢复定时器已启动，将在 %d 秒后恢复开仓", br.BotID, seconds)

	time.Sleep(time.Duration(seconds) * time.Second)

	// 检查是否仍处于暂停状态
	br.configMu.RLock()
	stillPaused := br.Config.OpenPositionControl.PauseOpening
	pauseReason := ""
	if br.Config.OpenPositionControl.BotRiskControl != nil {
		pauseReason = br.Config.OpenPositionControl.BotRiskControl.PauseOpeningReason
	}
	br.configMu.RUnlock()

	if stillPaused {
		logger.Info("🔔 [%s] 自动恢复定时器触发，恢复开仓（暂停原因: %s）", br.BotID, pauseReason)
		br.ResumeOpening()
	} else {
		logger.Info("ℹ️ [%s] 自动恢复定时器触发，但开仓已恢复，跳过", br.BotID)
	}
}

// IsBotEnabledInDB 检查數據庫中的 Bot 啟停狀態（公開方法，供 StartSymbol 等調用方使用）
// 返回值: (是否啟用, 原因)
// 如果數據庫中沒有記錄，默認返回 (true, "")（使用配置文件的值）
func (bm *BotManager) IsBotEnabledInDB(botID string) (bool, string) {
	return bm.isBotEnabledInDB(botID)
}

// isBotEnabledInDB 检查數據庫中的 Bot 啟停狀態
// 返回值: (是否啟用, 原因)
// 如果數據庫中沒有記錄，默認返回 (true, "")（使用配置文件的值）
// 若存儲不可用或查詢失敗，嘗試從文件 fallback 讀取（與 saveBotStateToDB 的寫入邏輯對應）
// 若文件也無記錄，保守返回 (false, reason)，避免已停止的 Bot 重啟後自動運行
func (bm *BotManager) isBotEnabledInDB(botID string) (bool, string) {
	if bm.storageService == nil {
		// 存儲未初始化時，與 store==nil 一樣嘗試文件 fallback
		// 否則 EnableBot 寫入文件後，StartBot 仍會因不讀文件而拒絕啟動
		if enabled, found := bm.isBotEnabledFromFile(botID); found {
			return enabled, "from_file"
		}
		logger.Warn("存儲服務未初始化，保守跳過 Bot 自動啟動（避免已停止狀態遺失）[%s]", botID)
		return false, "storage_unavailable"
	}

	store := bm.storageService.GetStorage()
	if store == nil {
		// 存儲未啟用（如 storage.enabled=false），嘗試文件 fallback
		if enabled, found := bm.isBotEnabledFromFile(botID); found {
			return enabled, "from_file"
		}
		logger.Warn("存儲未初始化，保守跳過 Bot 自動啟動（避免已停止狀態遺失）[%s]", botID)
		return false, "storage_unavailable"
	}

	state, err := store.GetBotState(botID)
	if err != nil {
		logger.Warn("查詢 Bot 狀態失敗: %v，保守視為已禁用", err)
		return false, "query_failed"
	}

	if state == nil {
		// 數據庫中沒有記錄，使用配置文件的值
		return true, ""
	}

	return state.Enabled, state.Reason
}

// saveBotStateToDB 保存 Bot 啟停狀態到數據庫（存儲不可用時 fallback 到文件）
func (bm *BotManager) saveBotStateToDB(botID string, enabled bool, updatedBy, reason string) {
	state := &storage.BotState{
		BotID:     botID,
		Enabled:   enabled,
		UpdatedAt: time.Now(),
		UpdatedBy: updatedBy,
		Reason:    reason,
	}

	if bm.storageService != nil {
		store := bm.storageService.GetStorage()
		if store != nil {
			if err := store.SetBotState(state); err != nil {
				logger.Error("保存 Bot 狀態失敗: %v", err)
			} else {
				logger.Info("✅ [%s] Bot 狀態已保存到數據庫: enabled=%v, reason=%s", botID, enabled, reason)
				return
			}
		}
	}

	// 存儲不可用時 fallback 到文件，確保停止狀態能持久化
	if err := bm.saveBotStateToFile(state); err != nil {
		logger.Error("保存 Bot 狀態到文件失敗: %v", err)
	} else {
		logger.Info("✅ [%s] Bot 狀態已保存到文件: enabled=%v, reason=%s", botID, enabled, reason)
	}
}

// botStatesFilePath 返回 bot_states 文件路徑
func (bm *BotManager) botStatesFilePath() string {
	if bm != nil && bm.botStatesFileOverride != "" {
		return bm.botStatesFileOverride
	}
	return filepath.Join("./data", "bot_states.json")
}

// isBotEnabledFromFile 從文件讀取 Bot 啟停狀態（存儲不可用時的 fallback）
func (bm *BotManager) isBotEnabledFromFile(botID string) (enabled bool, found bool) {
	path := bm.botStatesFilePath()
	data, err := os.ReadFile(path)
	if err != nil {
		return false, false
	}
	var m map[string]struct {
		Enabled bool   `json:"enabled"`
		Reason  string `json:"reason"`
	}
	if err := json.Unmarshal(data, &m); err != nil {
		return false, false
	}
	s, ok := m[botID]
	if !ok {
		return false, false
	}
	return s.Enabled, true
}

// GetStoppedAt 返回已停止 Bot 的停止時間（ISO 8601）。若 Bot 正在運行或無停止記錄則返回空。
func (bm *BotManager) GetStoppedAt(botID string) (string, bool) {
	bm.runtimesMu.RLock()
	_, running := bm.runtimes[botID]
	bm.runtimesMu.RUnlock()
	if running {
		return "", false
	}
	if bm.storageService != nil {
		store := bm.storageService.GetStorage()
		if store != nil {
			state, err := store.GetBotState(botID)
			if err == nil && state != nil && !state.Enabled {
				return state.UpdatedAt.Format(time.RFC3339), true
			}
		}
	}
	// 文件 fallback：若存儲不可用，嘗試從文件讀取
	if stoppedAt, ok := bm.getStoppedAtFromFile(botID); ok {
		return stoppedAt, true
	}
	return "", false
}

// getStoppedAtFromFile 從 bot_states.json 讀取停止時間（僅當 enabled=false 時有效）
func (bm *BotManager) getStoppedAtFromFile(botID string) (string, bool) {
	path := bm.botStatesFilePath()
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	var m map[string]struct {
		Enabled   bool   `json:"enabled"`
		UpdatedAt string `json:"updated_at"`
		Reason    string `json:"reason"`
	}
	if err := json.Unmarshal(data, &m); err != nil {
		return "", false
	}
	s, ok := m[botID]
	if !ok || s.Enabled || s.UpdatedAt == "" {
		return "", false
	}
	return s.UpdatedAt, true
}

// saveBotStateToFile 保存 Bot 狀態到文件（存儲不可用時的 fallback）
func (bm *BotManager) saveBotStateToFile(state *storage.BotState) error {
	path := bm.botStatesFilePath()
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	m := make(map[string]struct {
		Enabled   bool   `json:"enabled"`
		UpdatedAt string `json:"updated_at"`
		UpdatedBy string `json:"updated_by"`
		Reason    string `json:"reason"`
	})
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, &m)
	}
	m[state.BotID] = struct {
		Enabled   bool   `json:"enabled"`
		UpdatedAt string `json:"updated_at"`
		UpdatedBy string `json:"updated_by"`
		Reason    string `json:"reason"`
	}{state.Enabled, state.UpdatedAt.Format(time.RFC3339), state.UpdatedBy, state.Reason}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

