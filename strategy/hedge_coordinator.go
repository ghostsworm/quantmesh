package strategy

import (
	"context"
	"sync"

	"quantmesh/config"
	"quantmesh/event"
	"quantmesh/logger"
)

// getFloat64 從 event.Data 安全提取 float64
func getFloat64(m map[string]interface{}, key string) float64 {
	if m == nil {
		return 0
	}
	v, ok := m[key]
	if !ok {
		return 0
	}
	switch t := v.(type) {
	case float64:
		return t
	case int:
		return float64(t)
	case int64:
		return float64(t)
	}
	return 0
}

// getInt 從 event.Data 安全提取 int
func getInt(m map[string]interface{}, key string) int {
	if m == nil {
		return 0
	}
	v, ok := m[key]
	if !ok {
		return 0
	}
	switch t := v.(type) {
	case float64:
		return int(t)
	case int:
		return t
	case int64:
		return int(t)
	}
	return 0
}

// getString 從 event.Data 安全提取 string
func getString(m map[string]interface{}, key string) string {
	if m == nil {
		return ""
	}
	v, ok := m[key]
	if !ok {
		return ""
	}
	s, _ := v.(string)
	return s
}

// HedgeCoordinator 跨市場對沖協調器
// 監聽主 Bot (futures) 的持倉變化，根據 HedgeRatio 計算 spot Bot 應持有的對沖倉位，
// 通過 EventBus 發送對沖信號到 spot Bot
type HedgeCoordinator struct {
	group      config.BotGroup
	eventBus   *event.EventBus
	subCh      <-chan *event.Event
	ctx        context.Context
	cancel     context.CancelFunc
	mu         sync.RWMutex
	futuresPos float64 // 主 Bot 持倉（簡化：僅追蹤數量）
	spotPos    float64 // 對沖 Bot 持倉
}

// NewHedgeCoordinator 創建對沖協調器
func NewHedgeCoordinator(group config.BotGroup, eventBus *event.EventBus) *HedgeCoordinator {
	ctx, cancel := context.WithCancel(context.Background())
	hc := &HedgeCoordinator{
		group:    group,
		eventBus: eventBus,
		ctx:      ctx,
		cancel:   cancel,
	}
	if eventBus != nil {
		hc.subCh = eventBus.Subscribe()
	}
	return hc
}

// Start 啟動協調器（訂閱持倉相關事件）
func (hc *HedgeCoordinator) Start() error {
	if hc.eventBus == nil || hc.subCh == nil {
		logger.Info("HedgeCoordinator: event bus not configured, running in passive mode")
		return nil
	}
	go hc.run()
	logger.Info("HedgeCoordinator started for group %s", hc.group.ID)
	return nil
}

// Stop 停止協調器
func (hc *HedgeCoordinator) Stop() {
	hc.cancel()
	if hc.eventBus != nil && hc.subCh != nil {
		hc.eventBus.Unsubscribe(hc.subCh)
	}
	logger.Info("HedgeCoordinator stopped for group %s", hc.group.ID)
}

func (hc *HedgeCoordinator) run() {
	for {
		select {
		case <-hc.ctx.Done():
			return
		case evt, ok := <-hc.subCh:
			if !ok {
				return
			}
			hc.onEvent(evt)
		}
	}
}

// onEvent 處理事件（持倉變化等）
func (hc *HedgeCoordinator) onEvent(evt *event.Event) {
	switch evt.Type {
	case event.EventTypeOrderFilled, event.EventTypePositionOpened, event.EventTypePositionClosed:
		botID := getString(evt.Data, "bot_id")
		if botID == "" {
			return
		}
		// 僅處理本組的 futures Bot（第一個 BotID）
		if len(hc.group.BotIDs) < 2 || hc.group.BotIDs[0] != botID {
			return
		}
		marketType := getString(evt.Data, "market_type")
		if marketType != "futures" {
			return
		}
		position := getFloat64(evt.Data, "position")
		filledLayers := getInt(evt.Data, "filled_layers")
		symbol := getString(evt.Data, "symbol")
		exchangeName := getString(evt.Data, "exchange")
		if symbol == "" || exchangeName == "" {
			return
		}
		hc.mu.Lock()
		triggerLayers := hc.group.HedgeConfig.HedgeTriggerLayers
		shortRatio := hc.group.HedgeConfig.ShortNotionalRatio
		hc.mu.Unlock()
		if triggerLayers <= 0 {
			triggerLayers = 3
		}
		if shortRatio <= 0 {
			if hc.group.HedgeConfig.HedgeRatio > 0 {
				shortRatio = hc.group.HedgeConfig.HedgeRatio
			} else {
				shortRatio = 0.25
			}
		}
		if filledLayers < triggerLayers {
			logger.Debug("HedgeCoordinator: 網格未滿 %d 格 (當前 %d)，跳過對沖", triggerLayers, filledLayers)
			return
		}
		hc.mu.Lock()
		direction := hc.group.HedgeConfig.Direction
		hc.mu.Unlock()
		if direction == "" {
			direction = "LONG"
		}
		evtData := map[string]interface{}{
			"group_id":             hc.group.ID,
			"symbol":               symbol,
			"exchange":             exchangeName,
			"futures_filled_layers": filledLayers,
			"futures_position":     position,
		}
		// LONG 網格：合約持倉為正，發 target_spot_short（現貨做空對沖）
		// SHORT 網格：合約持倉為負，發 target_spot_long（現貨做多對沖）
		switch direction {
		case "SHORT":
			targetSpotLong := -position * shortRatio
			if targetSpotLong < 0.000001 {
				return
			}
			evtData["target_spot_long"] = targetSpotLong
			hc.eventBus.Publish(&event.Event{Type: event.EventTypeHedgeSignal, Data: evtData})
			logger.Info("📤 HedgeCoordinator: 發送對沖信號(做空) group=%s symbol=%s target_long=%.6f layers=%d",
				hc.group.ID, symbol, targetSpotLong, filledLayers)
		default:
			targetSpotShort := position * shortRatio
			if targetSpotShort < 0.000001 {
				return
			}
			evtData["target_spot_short"] = targetSpotShort
			hc.eventBus.Publish(&event.Event{Type: event.EventTypeHedgeSignal, Data: evtData})
			logger.Info("📤 HedgeCoordinator: 發送對沖信號(做多) group=%s symbol=%s target_short=%.6f layers=%d",
				hc.group.ID, symbol, targetSpotShort, filledLayers)
		}
	}
}

// GetTargetSpotPosition 根據主 Bot 持倉計算 spot 對沖目標倉位
func (hc *HedgeCoordinator) GetTargetSpotPosition(futuresPosition float64) float64 {
	hc.mu.RLock()
	ratio := hc.group.HedgeConfig.HedgeRatio
	hc.mu.RUnlock()
	if ratio <= 0 {
		ratio = 0.5
	}
	return futuresPosition * ratio
}
