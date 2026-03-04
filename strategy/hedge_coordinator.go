package strategy

import (
	"context"
	"sync"

	"quantmesh/config"
	"quantmesh/event"
	"quantmesh/logger"
)

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
	// 簡化實現：僅記錄事件，實際對沖邏輯需與 position manager 整合
	// 完整實現需：解析 evt 中的 exchange/symbol/marketType，判斷是否為本 group 的 futures Bot，
	// 計算 spot 應持倉 = futuresPos * HedgeRatio，發送對沖信號
	switch evt.Type {
	case event.EventTypeOrderFilled, event.EventTypePositionOpened, event.EventTypePositionClosed:
		// TODO: 解析持倉變化，計算對沖目標，發送對沖信號
		_ = evt
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
