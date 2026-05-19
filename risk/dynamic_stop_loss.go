package risk

import (
	"fmt"
	"sync"
	"time"

	"quantmesh/config"
	"quantmesh/event"
	"quantmesh/logger"
)

const (
	defaultVolatilityCheckInterval      = 60 * time.Second
	defaultProfitTrailingUpdateInterval = 10 * time.Second
)

// DynamicStopLossManager 动态止损管理器
type DynamicStopLossManager struct {
	config      *config.DynamicStopLossConfig
	eventBus    *event.EventBus
	botProvider BotProvider

	// 运行时状态
	activeSlots   map[string]*DynamicStopLossSlot // slotKey -> 动态止损信息
	activeSlotsMu sync.RWMutex
	lifecycleMu   sync.Mutex
	isRunning     bool
	stopChan      chan struct{}

	// 统计信息
	totalAdjustments int64
	adjustmentsMu    sync.RWMutex
}

// DynamicStopLossSlot 动态止损槽位信息
type DynamicStopLossSlot struct {
	BotID        string    `json:"bot_id"`
	Symbol       string    `json:"symbol"`
	EntryPrice   float64   `json:"entry_price"`
	OriginalStop float64   `json:"original_stop_loss"`
	CurrentStop  float64   `json:"current_stop_loss"`
	HighPrice    float64   `json:"high_price"` // 盈利追踪中的最高价
	LastUpdate   time.Time `json:"last_update"`
	Activated    bool      `json:"activated"` // 是否已激活盈利追踪
}

// StopLossAdjustment 止损调整记录
type StopLossAdjustment struct {
	Timestamp      time.Time `json:"timestamp"`
	BotID          string    `json:"bot_id"`
	Symbol         string    `json:"symbol"`
	OldStopLoss    float64   `json:"old_stop_loss"`
	NewStopLoss    float64   `json:"new_stop_loss"`
	Reason         string    `json:"reason"`
	AdjustmentType string    `json:"adjustment_type"` // volatility, time, profit, trend
}

// NewDynamicStopLossManager 创建动态止损管理器
func NewDynamicStopLossManager(cfg *config.DynamicStopLossConfig, eventBus *event.EventBus, botProvider BotProvider) *DynamicStopLossManager {
	return &DynamicStopLossManager{
		config:      cfg,
		eventBus:    eventBus,
		botProvider: botProvider,
		activeSlots: make(map[string]*DynamicStopLossSlot),
	}
}

// Start 启动动态止损管理器
func (dslm *DynamicStopLossManager) Start() {
	dslm.lifecycleMu.Lock()
	defer dslm.lifecycleMu.Unlock()

	if dslm.config == nil {
		logger.Warn("⚠️ [动态止损] 配置为空，跳过启动")
		return
	}
	if dslm.isRunning {
		logger.Warn("⚠️ [动态止损] 管理器已在运行")
		return
	}

	stopChan := make(chan struct{})
	dslm.stopChan = stopChan
	dslm.isRunning = true
	logger.Info("🎯 [动态止损] 启动动态止损管理器")

	// 启动多个检查器
	if dslm.config.VolatilityBased.Enabled {
		interval := dslm.checkIntervalDuration(
			"波动率检查",
			dslm.config.VolatilityBased.CheckInterval,
			defaultVolatilityCheckInterval,
		)
		go dslm.volatilityChecker(stopChan, interval)
	}

	if dslm.config.TimeBased.Enabled {
		go dslm.timeBasedChecker(stopChan)
	}

	if dslm.config.ProfitBasedTrailing.Enabled {
		interval := dslm.checkIntervalDuration(
			"盈利追踪检查",
			dslm.config.ProfitBasedTrailing.UpdateFrequency,
			defaultProfitTrailingUpdateInterval,
		)
		go dslm.profitTrailingChecker(stopChan, interval)
	}

	if dslm.config.TrendReversal.Enabled {
		go dslm.trendReversalChecker(stopChan)
	}
}

func (dslm *DynamicStopLossManager) checkIntervalDuration(name string, seconds int, fallback time.Duration) time.Duration {
	if seconds <= 0 {
		logger.Warn("⚠️ [动态止损] %s间隔配置无效: %d 秒，使用默认值 %s", name, seconds, fallback)
		return fallback
	}
	return time.Duration(seconds) * time.Second
}

// Stop 停止动态止损管理器
func (dslm *DynamicStopLossManager) Stop() {
	dslm.lifecycleMu.Lock()
	defer dslm.lifecycleMu.Unlock()

	if !dslm.isRunning {
		return
	}

	dslm.isRunning = false
	close(dslm.stopChan)
	dslm.stopChan = nil
	logger.Info("🛑 [动态止损] 停止动态止损管理器")
}

// volatilityChecker 波动率检查器
func (dslm *DynamicStopLossManager) volatilityChecker(stopChan <-chan struct{}, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-stopChan:
			return
		case <-ticker.C:
			dslm.checkVolatilityAdjustment()
		}
	}
}

// timeBasedChecker 时间分段检查器
func (dslm *DynamicStopLossManager) timeBasedChecker(stopChan <-chan struct{}) {
	ticker := time.NewTicker(5 * time.Minute) // 每5分钟检查一次
	defer ticker.Stop()

	for {
		select {
		case <-stopChan:
			return
		case <-ticker.C:
			dslm.checkTimeBasedAdjustment()
		}
	}
}

// profitTrailingChecker 盈利追踪检查器
func (dslm *DynamicStopLossManager) profitTrailingChecker(stopChan <-chan struct{}, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-stopChan:
			return
		case <-ticker.C:
			dslm.checkProfitTrailing()
		}
	}
}

// trendReversalChecker 趋势反转检查器
func (dslm *DynamicStopLossManager) trendReversalChecker(stopChan <-chan struct{}) {
	ticker := time.NewTicker(1 * time.Minute) // 每分钟检查一次
	defer ticker.Stop()

	for {
		select {
		case <-stopChan:
			return
		case <-ticker.C:
			dslm.checkTrendReversal()
		}
	}
}

// checkVolatilityAdjustment 检查波动率调整
func (dslm *DynamicStopLossManager) checkVolatilityAdjustment() {
	// TODO: 实现基于ATR的波动率调整
	// 1. 计算ATR
	// 2. 根据ATR调整止损距离
	// 3. 确保止损在[minStopLoss, maxStopLoss]范围内
	logger.Debug("🔍 [动态止损] 检查波动率调整")
}

// checkTimeBasedAdjustment 检查时间分段调整
func (dslm *DynamicStopLossManager) checkTimeBasedAdjustment() {
	currentHour := time.Now().Hour()
	adjustmentMultiplier := 1.0

	// 检查是否在高波动时段
	for _, hour := range dslm.config.TimeBased.HighVolatilityHours {
		if hour == currentHour {
			adjustmentMultiplier = dslm.config.TimeBased.HighVolatilityMultiplier
			logger.Debug("⏰ [动态止损] 当前处于高波动时段，倍数: %.2f", adjustmentMultiplier)
			break
		}
	}

	// 检查是否在低波动时段
	for _, hour := range dslm.config.TimeBased.LowVolatilityHours {
		if hour == currentHour {
			adjustmentMultiplier = dslm.config.TimeBased.LowVolatilityMultiplier
			logger.Debug("⏰ [动态止损] 当前处于低波动时段，倍数: %.2f", adjustmentMultiplier)
			break
		}
	}

	if adjustmentMultiplier != 1.0 {
		dslm.applyTimeBasedAdjustment(adjustmentMultiplier)
	}
}

// checkProfitTrailing 检查盈利追踪
func (dslm *DynamicStopLossManager) checkProfitTrailing() {
	if dslm.botProvider == nil {
		logger.Warn("⚠️ [动态止损] Bot 提供者为空，跳过盈利追踪检查")
		return
	}

	bots := dslm.botProvider.GetAllBots()

	for _, bot := range bots {
		// 获取当前仓位和盈利情况
		unrealizedPnL, totalValue, err := bot.GetPositionSummary()
		if err != nil {
			continue
		}

		if totalValue == 0 {
			continue
		}

		profitRatio := unrealizedPnL / totalValue

		// 检查是否激活盈利追踪
		if profitRatio >= dslm.config.ProfitBasedTrailing.ActivateProfit {
			// 使用bot作为key（简化实现）
			slotKey := fmt.Sprintf("%p", bot)
			dslm.activateProfitTrailingForSlot(slotKey, profitRatio)
		}

		// 更新盈利追踪止损
		slotKey := fmt.Sprintf("%p", bot)
		dslm.updateProfitTrailingStopForSlot(slotKey, profitRatio)
	}
}

// activateProfitTrailingForSlot 为槽位激活盈利追踪
func (dslm *DynamicStopLossManager) activateProfitTrailingForSlot(slotKey string, profitRatio float64) {
	dslm.activeSlotsMu.Lock()
	slot, exists := dslm.activeSlots[slotKey]
	if !exists {
		slot = &DynamicStopLossSlot{
			BotID:     slotKey,
			Activated: true,
		}
		dslm.activeSlots[slotKey] = slot
	}

	if !slot.Activated {
		slot.Activated = true
		logger.Info("📈 [动态止损] %s 激活盈利追踪，盈利比例: %.2f%%", slot.BotID, profitRatio*100)
	}
	dslm.activeSlotsMu.Unlock()
}

// updateProfitTrailingStopForSlot 更新槽位的盈利追踪止损
func (dslm *DynamicStopLossManager) updateProfitTrailingStopForSlot(slotKey string, profitRatio float64) {
	dslm.activeSlotsMu.RLock()
	slot, exists := dslm.activeSlots[slotKey]
	dslm.activeSlotsMu.RUnlock()

	if !exists || !slot.Activated {
		return
	}

	// TODO: 获取当前价格并更新追踪止损
	// 如果价格上涨，更新highPrice
	// 新止损 = highPrice * (1 - trailingDistance)
	logger.Debug("📊 [动态止损] 更新 %s 盈利追踪止损，当前盈利: %.2f%%", slot.BotID, profitRatio*100)
}

// checkTrendReversal 检查趋势反转
func (dslm *DynamicStopLossManager) checkTrendReversal() {
	// TODO: 实现趋势反转检测
	// 1. 获取最近N根K线
	// 2. 判断趋势（上升/下降）
	// 3. 检测趋势反转信号
	// 4. 反转时收紧止损
	logger.Debug("📈 [动态止损] 检查趋势反转")
}

// applyTimeBasedAdjustment 应用时间分段调整
func (dslm *DynamicStopLossManager) applyTimeBasedAdjustment(multiplier float64) {
	if dslm.botProvider == nil {
		logger.Warn("⚠️ [动态止损] Bot 提供者为空，跳过时间分段止损调整")
		return
	}

	bots := dslm.botProvider.GetAllBots()

	for _, bot := range bots {
		// 获取当前止损设置
		// TODO: 从bot获取当前止损配置
		// newStopLoss = originalStopLoss * multiplier
		logger.Debug("⏰ [动态止损] 调整 %s 止损倍数为 %.2f", bot, multiplier)
	}
}

// GetActiveSlots 获取活跃的动态止损槽位
func (dslm *DynamicStopLossManager) GetActiveSlots() map[string]*DynamicStopLossSlot {
	dslm.activeSlotsMu.RLock()
	defer dslm.activeSlotsMu.RUnlock()

	result := make(map[string]*DynamicStopLossSlot, len(dslm.activeSlots))
	for k, v := range dslm.activeSlots {
		if v == nil {
			continue
		}
		slotCopy := *v
		result[k] = &slotCopy
	}
	return result
}

// GetTotalAdjustments 获取总调整次数
func (dslm *DynamicStopLossManager) GetTotalAdjustments() int64 {
	dslm.adjustmentsMu.RLock()
	defer dslm.adjustmentsMu.RUnlock()
	return dslm.totalAdjustments
}

// recordAdjustment 记录调整
func (dslm *DynamicStopLossManager) recordAdjustment(adjustment *StopLossAdjustment) {
	dslm.adjustmentsMu.Lock()
	dslm.totalAdjustments++
	dslm.adjustmentsMu.Unlock()

	// 发布事件
	if dslm.eventBus != nil {
		dslm.eventBus.Publish(&event.Event{
			Type: event.EventType("stop_loss:adjusted"),
			Data: map[string]interface{}{
				"adjustment": adjustment,
			},
		})
	}

	logger.Info("🎯 [动态止损] 止损已调整: %s %s %.4f -> %.4f (%s)",
		adjustment.BotID, adjustment.Symbol, adjustment.OldStopLoss, adjustment.NewStopLoss, adjustment.Reason)
}
