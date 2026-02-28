package risk

import (
	"context"
	"fmt"
	"sync"
	"time"

	"quantmesh/config"
	"quantmesh/event"
	"quantmesh/logger"
)

// CircuitBreakerStatus 熔断器状态
type CircuitBreakerStatus string

const (
	CircuitBreakerStatusNormal   CircuitBreakerStatus = "normal"   // 正常
	CircuitBreakerStatusTripped  CircuitBreakerStatus = "tripped"  // 已触发
	CircuitBreakerStatusRecovery CircuitBreakerStatus = "recovery" // 恢复中
)

// CircuitBreakerTrigger 触发器类型
type CircuitBreakerTrigger string

const (
	TriggerManual                CircuitBreakerTrigger = "manual"                  // 手动触发
	TriggerTotalDailyLoss        CircuitBreakerTrigger = "total_daily_loss"        // 单日总亏损
	TriggerMaxDrawdown           CircuitBreakerTrigger = "max_drawdown"            // 最大回撤
	TriggerConsecutiveLosses     CircuitBreakerTrigger = "consecutive_losses"      // 连续亏损
	TriggerWebSocketDisconnected CircuitBreakerTrigger = "websocket_disconnected"  // WebSocket断线
	TriggerAPIAuthFailed         CircuitBreakerTrigger = "api_auth_failed"         // API认证失败
	TriggerAllocationExceeded    CircuitBreakerTrigger = "allocation_exceeded"     // 配额超限
)

// CircuitBreakerEvent 熔断事件
type CircuitBreakerEvent struct {
	Timestamp    time.Time                `json:"timestamp"`
	Status       CircuitBreakerStatus     `json:"status"`
	Trigger      CircuitBreakerTrigger    `json:"trigger"`
	TriggeredBy  string                   `json:"triggered_by"` // 操作人
	Reason       string                   `json:"reason"`
	Metrics      map[string]float64       `json:"metrics"`
}

// GlobalCircuitBreaker 全局熔断器
type GlobalCircuitBreaker struct {
	config      *config.CircuitBreakerConfig
	status      CircuitBreakerStatus
	statusMu    sync.RWMutex
	eventBus    *event.EventBus
	botProvider BotProvider

	// 统计数据
	dailyPnL        float64 // 当日总盈亏
	maxDrawdown    float64 // 最大回撤
	consecutiveLosses int    // 连续亏损次数
	authFailCount    int    // API认证失败次数
	lastWSDisconnect time.Time

	// 熔断历史
	events      []*CircuitBreakerEvent
	eventsMu    sync.RWMutex
	trippedAt   time.Time

	// 冷却期结束时间
	cooldownUntil time.Time
}

// BotProvider Bot 提供者接口
type BotProvider interface {
	GetAllBots() []BotController
}

// BotController Bot 控制器接口
type BotController interface {
	PauseOpening(reason string)
	ResumeOpening()
	CancelAllOpenOrders() error
	CloseAllPositions(ctx context.Context, method string, timeout int) error
	GetPositionSummary() (float64, float64, error) // (unrealizedPnL, totalValue, error)
}

// NewGlobalCircuitBreaker 创建全局熔断器
func NewGlobalCircuitBreaker(cfg *config.CircuitBreakerConfig, eventBus *event.EventBus, botProvider BotProvider) *GlobalCircuitBreaker {
	gcb := &GlobalCircuitBreaker{
		config:      cfg,
		status:      CircuitBreakerStatusNormal,
		eventBus:    eventBus,
		botProvider: botProvider,
		events:      make([]*CircuitBreakerEvent, 0, 100),
	}

	// 启动后台检查
	if cfg.Enabled {
		go gcb.backgroundChecker()
	}

	return gcb
}

// backgroundChecker 后台检查触发条件
func (gcb *GlobalCircuitBreaker) backgroundChecker() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		if gcb.config.Enabled {
			gcb.checkTriggers()
		}
	}
}

// checkTriggers 检查所有触发条件
func (gcb *GlobalCircuitBreaker) checkTriggers() {
	gcb.statusMu.RLock()
	currentStatus := gcb.status
	gcb.statusMu.RUnlock()

	// 如果已触发且在恢复期，检查恢复条件
	if currentStatus == CircuitBreakerStatusTripped {
		gcb.checkRecovery()
		return
	}

	// 如果在冷却期，跳过检查
	if time.Now().Before(gcb.cooldownUntil) {
		return
	}

	// 检查各种触发条件
	if gcb.checkDailyLossTrigger() {
		return
	}
	if gcb.checkMaxDrawdownTrigger() {
		return
	}
	if gcb.checkConsecutiveLossesTrigger() {
		return
	}
	if gcb.checkWebSocketTrigger() {
		return
	}
	if gcb.checkAPIAuthTrigger() {
		return
	}
	if gcb.checkAllocationTrigger() {
		return
	}
}

// checkDailyLossTrigger 检查单日总亏损触发
func (gcb *GlobalCircuitBreaker) checkDailyLossTrigger() bool {
	if !gcb.config.Triggers.TotalDailyLoss.Enabled {
		return false
	}

	if gcb.dailyPnL <= -gcb.config.Triggers.TotalDailyLoss.Threshold {
		gcb.trip(TriggerTotalDailyLoss, "system", fmt.Sprintf("单日亏损超限: %.2f USDT", gcb.dailyPnL))
		return true
	}
	return false
}

// checkMaxDrawdownTrigger 检查最大回撤触发
func (gcb *GlobalCircuitBreaker) checkMaxDrawdownTrigger() bool {
	if !gcb.config.Triggers.MaxDrawdown.Enabled {
		return false
	}

	if gcb.maxDrawdown >= gcb.config.Triggers.MaxDrawdown.Threshold {
		gcb.trip(TriggerMaxDrawdown, "system", fmt.Sprintf("最大回撤超限: %.2f%%", gcb.maxDrawdown))
		return true
	}
	return false
}

// checkConsecutiveLossesTrigger 检查连续亏损触发
func (gcb *GlobalCircuitBreaker) checkConsecutiveLossesTrigger() bool {
	if !gcb.config.Triggers.ConsecutiveLosses.Enabled {
		return false
	}

	if gcb.consecutiveLosses >= gcb.config.Triggers.ConsecutiveLosses.Count {
		gcb.trip(TriggerConsecutiveLosses, "system", fmt.Sprintf("连续亏损次数超限: %d次", gcb.consecutiveLosses))
		return true
	}
	return false
}

// checkWebSocketTrigger 检查 WebSocket 断线触发
func (gcb *GlobalCircuitBreaker) checkWebSocketTrigger() bool {
	if !gcb.config.Triggers.WebSocketDisconnected.Enabled {
		return false
	}

	if !gcb.lastWSDisconnect.IsZero() {
		disconnectedFor := time.Since(gcb.lastWSDisconnect).Seconds()
		if int(disconnectedFor) > gcb.config.Triggers.WebSocketDisconnected.Timeout {
			gcb.trip(TriggerWebSocketDisconnected, "system", fmt.Sprintf("WebSocket断线超限: %d秒", int(disconnectedFor)))
			return true
		}
	}
	return false
}

// checkAPIAuthTrigger 检查 API 认证失败触发
func (gcb *GlobalCircuitBreaker) checkAPIAuthTrigger() bool {
	if !gcb.config.Triggers.APIAuthFailed.Enabled {
		return false
	}

	if gcb.authFailCount >= gcb.config.Triggers.APIAuthFailed.Count {
		gcb.trip(TriggerAPIAuthFailed, "system", fmt.Sprintf("API认证失败次数超限: %d次", gcb.authFailCount))
		return true
	}
	return false
}

// checkAllocationTrigger 检查配额超限触发
func (gcb *GlobalCircuitBreaker) checkAllocationTrigger() bool {
	if !gcb.config.Triggers.AllocationExceeded.Enabled {
		return false
	}

	// TODO: 检查分配管理器是否触发配额超限
	return false
}

// checkRecovery 检查恢复条件
func (gcb *GlobalCircuitBreaker) checkRecovery() {
	if gcb.config.Recovery.AutoResume {
		// 自动恢复逻辑
		trippedDuration := time.Since(gcb.trippedAt)
		if trippedDuration > time.Duration(gcb.config.Actions.PauseDuration)*time.Second {
			gcb.recover("auto")
		}
	}
}

// trip 触发熔断
func (gcb *GlobalCircuitBreaker) trip(trigger CircuitBreakerTrigger, triggeredBy, reason string) {
	gcb.statusMu.Lock()
	defer gcb.statusMu.Unlock()

	logger.Error("🔴 [全局熔断] 熔断触发! 触发器: %s, 原因: %s, 操作人: %s", trigger, reason, triggeredBy)

	// 记录事件
	cbEvent := &CircuitBreakerEvent{
		Timestamp:  time.Now(),
		Status:     CircuitBreakerStatusTripped,
		Trigger:    trigger,
		TriggeredBy: triggeredBy,
		Reason:     reason,
		Metrics: map[string]float64{
			"daily_pnl":         gcb.dailyPnL,
			"max_drawdown":      gcb.maxDrawdown,
			"consecutive_losses": float64(gcb.consecutiveLosses),
		},
	}

	gcb.eventsMu.Lock()
	gcb.events = append(gcb.events, cbEvent)
	gcb.eventsMu.Unlock()

	gcb.status = CircuitBreakerStatusTripped
	gcb.trippedAt = time.Now()

	// 发布事件
	if gcb.eventBus != nil {
		gcb.eventBus.Publish(&event.Event{
			Type: event.EventType("circuit_breaker:triggered"),
			Data: map[string]interface{}{
				"event": cbEvent,
			},
		})
	}

	// 执行熔断动作
	go gcb.executeActions()
}

// executeActions 执行熔断动作
func (gcb *GlobalCircuitBreaker) executeActions() {
	logger.Warn("🚨 [全局熔断] 开始执行熔断动作...")

	// 1. 停止所有新开仓
	if gcb.config.Actions.StopAllNewOrders {
		gcb.pauseAllBots("circuit_breaker")
	}

	// 2. 撤销所有挂单
	if gcb.config.Actions.CancelAllOpenOrders {
		gcb.cancelAllOrders()
	}

	// 3. 平仓（如果启用）
	if gcb.config.Actions.ClosePositions.Enabled {
		gcb.closeAllPositions()
	}

	// 4. 发送通知
	gcb.sendNotification()

	logger.Warn("✅ [全局熔断] 熔断动作执行完成")
}

// pauseAllBots 暂停所有 Bot 开仓
func (gcb *GlobalCircuitBreaker) pauseAllBots(reason string) {
	bots := gcb.botProvider.GetAllBots()
	for _, bot := range bots {
		bot.PauseOpening(reason)
	}
	logger.Info("⏸️ [全局熔断] 已暂停 %d 个 Bot 的开仓", len(bots))
}

// cancelAllOrders 撤销所有挂单
func (gcb *GlobalCircuitBreaker) cancelAllOrders() {
	bots := gcb.botProvider.GetAllBots()
	successCount := 0
	for _, bot := range bots {
		if err := bot.CancelAllOpenOrders(); err != nil {
			logger.Error("❌ [全局熔断] Bot 撤单失败: %v", err)
		} else {
			successCount++
		}
	}
	logger.Info("🔄 [全局熔断] 已撤销 %d/%d 个 Bot 的挂单", successCount, len(bots))
}

// closeAllPositions 平仓
func (gcb *GlobalCircuitBreaker) closeAllPositions() {
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(gcb.config.Actions.ClosePositions.Timeout)*time.Second)
	defer cancel()

	bots := gcb.botProvider.GetAllBots()
	successCount := 0
	for _, bot := range bots {
		if err := bot.CloseAllPositions(ctx, gcb.config.Actions.ClosePositions.Method, gcb.config.Actions.ClosePositions.Timeout); err != nil {
			logger.Error("❌ [全局熔断] Bot 平仓失败: %v", err)
		} else {
			successCount++
		}
	}
	logger.Info("📤 [全局熔断] 已平仓 %d/%d 个 Bot", successCount, len(bots))
}

// sendNotification 发送通知
func (gcb *GlobalCircuitBreaker) sendNotification() {
	if !gcb.config.Notifications.Enabled {
		return
	}

	logger.Info("📢 [全局熔断] 发送通知到渠道: %v", gcb.config.Notifications.Channels)
	// TODO: 实现各种通知渠道的发送
}

// ManualTrigger 手动触发熔断
func (gcb *GlobalCircuitBreaker) ManualTrigger(triggeredBy, reason string) {
	gcb.trip(TriggerManual, triggeredBy, reason)
}

// ManualRecover 手动恢复
func (gcb *GlobalCircuitBreaker) ManualRecover(triggeredBy string) error {
	gcb.statusMu.Lock()
	defer gcb.statusMu.Unlock()

	if gcb.status != CircuitBreakerStatusTripped {
		return fmt.Errorf("当前状态不是已触发，无需恢复")
	}

	logger.Info("🔧 [全局熔断] 手动恢复，操作人: %s", triggeredBy)
	gcb.recover(triggeredBy)
	return nil
}

// recover 恢复
func (gcb *GlobalCircuitBreaker) recover(triggeredBy string) {
	logger.Info("♻️  [全局熔断] 开始恢复...")

	// 记录事件
	cbEvent := &CircuitBreakerEvent{
		Timestamp:   time.Now(),
		Status:      CircuitBreakerStatusNormal,
		Trigger:     TriggerManual,
		TriggeredBy: triggeredBy,
		Reason:      "手动恢复",
	}

	gcb.eventsMu.Lock()
	gcb.events = append(gcb.events, cbEvent)
	gcb.eventsMu.Unlock()

	gcb.status = CircuitBreakerStatusNormal
	gcb.cooldownUntil = time.Now().Add(time.Duration(gcb.config.Recovery.CooldownMinutes) * time.Minute)

	// 恢复所有 Bot
	bots := gcb.botProvider.GetAllBots()
	for _, bot := range bots {
		bot.ResumeOpening()
	}
	logger.Info("▶️ [全局熔断] 已恢复 %d 个 Bot", len(bots))

	// 发布事件
	if gcb.eventBus != nil {
		gcb.eventBus.Publish(&event.Event{
			Type: event.EventType("circuit_breaker:recovered"),
			Data: map[string]interface{}{
				"event": cbEvent,
			},
		})
	}
}

// UpdateMetrics 更新统计数据
func (gcb *GlobalCircuitBreaker) UpdateMetrics(dailyPnL, maxDrawdown float64, consecutiveLosses int) {
	gcb.statusMu.Lock()
	defer gcb.statusMu.Unlock()

	gcb.dailyPnL = dailyPnL
	gcb.maxDrawdown = maxDrawdown
	gcb.consecutiveLosses = consecutiveLosses
}

// ReportAuthFailure 报告 API 认证失败
func (gcb *GlobalCircuitBreaker) ReportAuthFailure() {
	gcb.statusMu.Lock()
	defer gcb.statusMu.Unlock()

	gcb.authFailCount++
	logger.Warn("⚠️ [全局熔断] API认证失败计数: %d", gcb.authFailCount)
}

// ReportWebSocketDisconnect 报告 WebSocket 断线
func (gcb *GlobalCircuitBreaker) ReportWebSocketDisconnect() {
	gcb.statusMu.Lock()
	defer gcb.statusMu.Unlock()

	gcb.lastWSDisconnect = time.Now()
	logger.Warn("⚠️ [全局熔断] WebSocket断线")
}

// ReportWebSocketReconnected 报告 WebSocket 重连
func (gcb *GlobalCircuitBreaker) ReportWebSocketReconnected() {
	gcb.statusMu.Lock()
	defer gcb.statusMu.Unlock()

	gcb.lastWSDisconnect = time.Time{} // 重置
	logger.Info("✅ [全局熔断] WebSocket重连成功")
}

// GetStatus 获取当前状态
func (gcb *GlobalCircuitBreaker) GetStatus() CircuitBreakerStatus {
	gcb.statusMu.RLock()
	defer gcb.statusMu.RUnlock()
	return gcb.status
}

// GetEvents 获取事件历史
func (gcb *GlobalCircuitBreaker) GetEvents(limit int) []*CircuitBreakerEvent {
	gcb.eventsMu.RLock()
	defer gcb.eventsMu.RUnlock()

	if limit <= 0 || limit > len(gcb.events) {
		limit = len(gcb.events)
	}
	return gcb.events[len(gcb.events)-limit:]
}

// IsTripped 是否已触发
func (gcb *GlobalCircuitBreaker) IsTripped() bool {
	return gcb.GetStatus() == CircuitBreakerStatusTripped
}

// GetConfig 获取配置
func (gcb *GlobalCircuitBreaker) GetConfig() *config.CircuitBreakerConfig {
	return gcb.config
}
