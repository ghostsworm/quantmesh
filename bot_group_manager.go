package main

import (
	"context"
	"sync"

	"quantmesh/config"
	"quantmesh/event"
	"quantmesh/logger"
	"quantmesh/strategy"
	"quantmesh/web"
)

// BotGroupManager Bot 組生命週期管理
// 負責啟停 Group 內所有 Bot、聚合狀態、對沖協調
type BotGroupManager struct {
	mu            sync.RWMutex
	coordinators  map[string]*strategy.HedgeCoordinator
	eventBus      *event.EventBus
	startBotFn    func(ctx context.Context, cfg config.BotConfig) error
	stopBotFn     func(botID string) error
}

// NewBotGroupManager 創建 Bot 組管理器
func NewBotGroupManager(eventBus *event.EventBus, startBotFn func(context.Context, config.BotConfig) error, stopBotFn func(string) error) *BotGroupManager {
	return &BotGroupManager{
		coordinators: make(map[string]*strategy.HedgeCoordinator),
		eventBus:     eventBus,
		startBotFn:   startBotFn,
		stopBotFn:    stopBotFn,
	}
}

// StartGroup 啟動 Group 內所有 Bot 及對沖協調器
func (m *BotGroupManager) StartGroup(ctx context.Context, group config.BotGroup) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	cfg, err := web.GetLatestConfig()
	if err != nil || cfg == nil {
		return err
	}

	// 啟動組內每個 Bot
	for _, botID := range group.BotIDs {
		var botCfg *config.BotConfig
		for i := range cfg.Bots {
			id := cfg.Bots[i].ID
			if id == "" {
				id = config.GenerateBotID(cfg.Bots[i].Exchange, cfg.Bots[i].Symbol, cfg.Bots[i].GetMarketType())
			}
			if id == botID {
				botCfg = &cfg.Bots[i]
				break
			}
		}
		if botCfg == nil {
			logger.Warn("BotGroupManager: bot %s not found in config", botID)
			continue
		}
		if err := m.startBotFn(ctx, *botCfg); err != nil {
			return err
		}
	}

	// 啟動對沖協調器
	coord := strategy.NewHedgeCoordinator(group, m.eventBus)
	if err := coord.Start(); err != nil {
		logger.Warn("BotGroupManager: hedge coordinator start failed: %v", err)
	}
	m.coordinators[group.ID] = coord
	return nil
}

// StopGroup 停止 Group 內所有 Bot 及對沖協調器
func (m *BotGroupManager) StopGroup(groupID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	cfg, err := web.GetLatestConfig()
	if err != nil || cfg == nil {
		return err
	}

	var group *config.BotGroup
	for i := range cfg.BotGroups {
		if cfg.BotGroups[i].ID == groupID {
			group = &cfg.BotGroups[i]
			break
		}
	}
	if group == nil {
		return nil
	}

	// 停止對沖協調器
	if coord, ok := m.coordinators[groupID]; ok {
		coord.Stop()
		delete(m.coordinators, groupID)
	}

	// 停止組內每個 Bot
	for _, botID := range group.BotIDs {
		_ = m.stopBotFn(botID)
	}
	return nil
}

// GetGroupStatus 聚合 Group 內所有 Bot 的 P&L、風控狀態
func (m *BotGroupManager) GetGroupStatus(groupID string) (map[string]interface{}, bool) {
	cfg, err := web.GetLatestConfig()
	if err != nil || cfg == nil {
		return nil, false
	}
	for _, g := range cfg.BotGroups {
		if g.ID == groupID {
			// 簡化：返回組信息，實際 P&L 需從 botManagerProvider 聚合
			return map[string]interface{}{
				"group_id": g.ID,
				"name":     g.Name,
				"type":     g.Type,
				"bot_ids":  g.BotIDs,
			}, true
		}
	}
	return nil, false
}

// OnBotEvent 接收 Bot 事件，判斷是否需要觸發對沖信號
func (m *BotGroupManager) OnBotEvent(evt *event.Event) {
	// 由 HedgeCoordinator 通過 Subscribe 訂閱 EventBus 處理
	_ = evt
}
