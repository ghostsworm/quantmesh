package risk

import (
	"context"
	"fmt"
	"strings"
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
	TriggerManual                CircuitBreakerTrigger = "manual"                 // 手动触发
	TriggerTotalDailyLoss        CircuitBreakerTrigger = "total_daily_loss"       // 单日总亏损
	TriggerMaxDrawdown           CircuitBreakerTrigger = "max_drawdown"           // 最大回撤
	TriggerConsecutiveLosses     CircuitBreakerTrigger = "consecutive_losses"     // 连续亏损
	TriggerWebSocketDisconnected CircuitBreakerTrigger = "websocket_disconnected" // WebSocket断线
	TriggerAPIAuthFailed         CircuitBreakerTrigger = "api_auth_failed"        // API认证失败
	TriggerAllocationExceeded    CircuitBreakerTrigger = "allocation_exceeded"    // 配额超限
)

const (
	// circuitBreakerCheckInterval 后台检查触发条件的间隔
	circuitBreakerCheckInterval = 30 * time.Second
	// circuitBreakerPauseSource 熔断器在暂停协调器中的来源名
	circuitBreakerPauseSource = "circuit_breaker"
	// triggeredByAuto 自动恢复的操作人标识
	triggeredByAuto = "auto"
	// triggeredBySystem 系统自动触发的操作人标识
	triggeredBySystem = "system"
)

// CircuitBreakerEvent 熔断事件
type CircuitBreakerEvent struct {
	Timestamp   time.Time             `json:"timestamp"`
	Status      CircuitBreakerStatus  `json:"status"`
	Trigger     CircuitBreakerTrigger `json:"trigger"`
	TriggeredBy string                `json:"triggered_by"` // 操作人
	Reason      string                `json:"reason"`
	Metrics     map[string]float64    `json:"metrics"`
}

// AlertNotifier 告警发送接口（由 notify.NotificationService 实现）
type AlertNotifier interface {
	Send(evt *event.Event)
}

// MetricsResetMarks 熔断恢复时的统计基线。
// Feeder 只统计基线之后的数据，避免恢复后立刻用同一批历史亏损再次触发。
type MetricsResetMarks struct {
	All    time.Time // 手动恢复：日内已实现盈亏、回撤高水位、连续亏损全部重算
	Streak time.Time // 任意恢复：连续亏损从此刻重新计数
}

// circuitMetrics 统计数据快照
type circuitMetrics struct {
	health              MetricsHealth
	dailyPnL            float64
	maxDrawdown         float64
	consecutiveLosses   int
	authFailCount       int
	lastWSDisconnect    time.Time
	allocationAvailable bool
	allocationExceeded  bool
	allocationReason    string
}

// GlobalCircuitBreaker 全局熔断器
type GlobalCircuitBreaker struct {
	config      *config.CircuitBreakerConfig
	status      CircuitBreakerStatus
	statusMu    sync.RWMutex
	eventBus    *event.EventBus
	botProvider BotProvider

	// 统计数据（受 statusMu 保护）
	dailyPnL             float64 // 当日总盈亏
	maxDrawdown          float64 // 最大回撤
	metricsHealth        MetricsHealth
	metricsHealthApplyMu sync.Mutex
	consecutiveLosses    int // 连续亏损次数
	authFailCount        int // API认证失败次数
	lastWSDisconnect     time.Time
	allocationAvailable  bool
	allocationExceeded   bool
	allocationReason     string
	allocationError      string
	resetMarks           MetricsResetMarks
	wsDisconnected       map[string]time.Time // 各數據流首次斷線時間，受 statusMu 保護

	// 熔断历史
	events    []*CircuitBreakerEvent
	eventsMu  sync.RWMutex
	trippedAt time.Time

	// 冷却期结束时间（受 statusMu 保护）
	cooldownUntil time.Time

	// 可选依赖（受 depsMu 保护）
	depsMu   sync.RWMutex
	notifier AlertNotifier
	pauser   *OpeningPauseCoordinator
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

// AllocationRiskProvider reports whether the current Bot allocation exceeds
// its configured fixed limit. Missing or unsupported measurements are errors.
type AllocationRiskProvider interface {
	AllocationRiskStatus() (exceeded bool, reason string, err error)
}

type allocationRiskHold interface {
	SetAllocationRiskHold(bool)
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

// SetNotifier 设置告警通知器（nil 表示仅记录日志）
func (gcb *GlobalCircuitBreaker) SetNotifier(n AlertNotifier) {
	gcb.depsMu.Lock()
	defer gcb.depsMu.Unlock()
	gcb.notifier = n
}

// SetPauseCoordinator 设置暂停开仓协调器（与复合风控共用，避免互相覆盖恢复）
func (gcb *GlobalCircuitBreaker) SetPauseCoordinator(p *OpeningPauseCoordinator) {
	gcb.depsMu.Lock()
	gcb.pauser = p
	if p != nil && p.IsHeldBy(circuitBreakerPauseSource) {
		gcb.statusMu.Lock()
		if gcb.status != CircuitBreakerStatusTripped {
			gcb.status = CircuitBreakerStatusTripped
			gcb.trippedAt = time.Now()
		}
		gcb.statusMu.Unlock()
	}
	gcb.depsMu.Unlock()
	if p != nil {
		p.SetOpeningAdmissionCheck(metricsUnavailablePauseSource, gcb.metricsOpeningAdmissionAllowed)
	}
	// Binding precedes automatic Bot startup. Establish the data-health hold
	// synchronously, before the feeder's first (possibly slow) observation.
	// Release depsMu before applying: applyMetricsHealthGate reads deps itself.
	gcb.applyMetricsHealthGate()
}

func (gcb *GlobalCircuitBreaker) deps() (AlertNotifier, *OpeningPauseCoordinator) {
	gcb.depsMu.RLock()
	defer gcb.depsMu.RUnlock()
	return gcb.notifier, gcb.pauser
}

// SubscribeConnectivityEvents 订阅事件总线中的 WebSocket 断线/重连与 API 认证失败事件。
// 注意：截至 R2，交易所适配层尚未发布这些事件类型，此订阅为预留钩子。
func (gcb *GlobalCircuitBreaker) SubscribeConnectivityEvents(ctx context.Context, bus *event.EventBus) {
	if bus == nil {
		return
	}
	ch := bus.Subscribe()
	go func() {
		defer bus.Unsubscribe(ch)
		for {
			select {
			case <-ctx.Done():
				return
			case evt, ok := <-ch:
				if !ok {
					return
				}
				if evt == nil {
					continue
				}
				switch evt.Type {
				case event.EventTypeWebSocketDisconnected:
					gcb.ReportWebSocketDisconnect(connectivityEventKey(evt))
				case event.EventTypeWebSocketReconnected:
					gcb.ReportWebSocketReconnected(connectivityEventKey(evt))
				case event.EventTypeWebSocketStopped:
					gcb.ReportWebSocketStopped(connectivityEventKey(evt))
				case event.EventTypeAPIAuthFailed:
					gcb.ReportAuthFailure()
				}
			}
		}
	}()
}

// backgroundChecker 后台检查触发条件
func (gcb *GlobalCircuitBreaker) backgroundChecker() {
	ticker := time.NewTicker(circuitBreakerCheckInterval)
	defer ticker.Stop()

	for range ticker.C {
		if gcb.config.Enabled {
			gcb.checkTriggers()
		}
	}
}

// snapshotMetrics 在锁内复制统计数据
func (gcb *GlobalCircuitBreaker) snapshotMetrics() circuitMetrics {
	gcb.statusMu.RLock()
	defer gcb.statusMu.RUnlock()
	return circuitMetrics{
		health:              gcb.metricsHealth,
		dailyPnL:            gcb.dailyPnL,
		maxDrawdown:         gcb.maxDrawdown,
		consecutiveLosses:   gcb.consecutiveLosses,
		authFailCount:       gcb.authFailCount,
		lastWSDisconnect:    gcb.lastWSDisconnect,
		allocationAvailable: gcb.allocationAvailable,
		allocationExceeded:  gcb.allocationExceeded,
		allocationReason:    gcb.allocationReason,
	}
}

// checkTriggers 检查所有触发条件
func (gcb *GlobalCircuitBreaker) checkTriggers() {
	gcb.refreshAllocationRisk()
	gcb.applyMetricsHealthGate()
	gcb.statusMu.RLock()
	currentStatus := gcb.status
	cooldownUntil := gcb.cooldownUntil
	gcb.statusMu.RUnlock()

	// 如果已触发，检查恢复条件
	if currentStatus == CircuitBreakerStatusTripped {
		gcb.checkRecovery()
		return
	}

	// 如果在冷却期，跳过检查
	if time.Now().Before(cooldownUntil) {
		return
	}

	m := gcb.snapshotMetrics()
	if trigger, reason, hit := gcb.evaluateTriggers(m); hit {
		gcb.trip(trigger, triggeredBySystem, reason)
	}
}

// evaluateTriggers 按优先级返回第一个命中的触发器
func (gcb *GlobalCircuitBreaker) evaluateTriggers(m circuitMetrics) (CircuitBreakerTrigger, string, bool) {
	checks := []func(circuitMetrics) (CircuitBreakerTrigger, string, bool){
		gcb.checkDailyLossTrigger,
		gcb.checkMaxDrawdownTrigger,
		gcb.checkConsecutiveLossesTrigger,
		gcb.checkWebSocketTrigger,
		gcb.checkAPIAuthTrigger,
		gcb.checkAllocationTrigger,
	}
	for _, check := range checks {
		if trigger, reason, hit := check(m); hit {
			return trigger, reason, true
		}
	}
	return "", "", false
}

// checkDailyLossTrigger 检查单日总亏损触发
func (gcb *GlobalCircuitBreaker) checkDailyLossTrigger(m circuitMetrics) (CircuitBreakerTrigger, string, bool) {
	t := gcb.config.Triggers.TotalDailyLoss
	if !t.Enabled || t.Threshold <= 0 {
		return "", "", false
	}
	if m.dailyPnL <= -t.Threshold {
		return TriggerTotalDailyLoss, fmt.Sprintf("单日亏损超限: %.2f USDT (阈值 %.2f)", m.dailyPnL, t.Threshold), true
	}
	return "", "", false
}

// checkMaxDrawdownTrigger 检查最大回撤触发
func (gcb *GlobalCircuitBreaker) checkMaxDrawdownTrigger(m circuitMetrics) (CircuitBreakerTrigger, string, bool) {
	t := gcb.config.Triggers.MaxDrawdown
	if !t.Enabled || t.Threshold <= 0 {
		return "", "", false
	}
	health := m.health.validAt(time.Now())
	if !health.CheckedAt.IsZero() && !health.verifiedDrawdown() {
		return "", "", false // unavailable/unadjusted data pauses new risk, never forces a liquidation
	}
	if m.maxDrawdown >= t.Threshold {
		return TriggerMaxDrawdown, fmt.Sprintf("最大回撤超限: %.2f%% (阈值 %.2f%%)", m.maxDrawdown, t.Threshold), true
	}
	return "", "", false
}

// checkConsecutiveLossesTrigger 检查连续亏损触发
func (gcb *GlobalCircuitBreaker) checkConsecutiveLossesTrigger(m circuitMetrics) (CircuitBreakerTrigger, string, bool) {
	t := gcb.config.Triggers.ConsecutiveLosses
	if !t.Enabled || t.Count <= 0 {
		return "", "", false
	}
	if m.consecutiveLosses >= t.Count {
		return TriggerConsecutiveLosses, fmt.Sprintf("连续亏损次数超限: %d次 (阈值 %d)", m.consecutiveLosses, t.Count), true
	}
	return "", "", false
}

// checkWebSocketTrigger 检查 WebSocket 断线触发
func (gcb *GlobalCircuitBreaker) checkWebSocketTrigger(m circuitMetrics) (CircuitBreakerTrigger, string, bool) {
	t := gcb.config.Triggers.WebSocketDisconnected
	if !t.Enabled || m.lastWSDisconnect.IsZero() {
		return "", "", false
	}
	disconnectedFor := time.Since(m.lastWSDisconnect)
	if disconnectedFor > time.Duration(t.Timeout)*time.Second {
		return TriggerWebSocketDisconnected, fmt.Sprintf("WebSocket断线超限: %d秒", int(disconnectedFor.Seconds())), true
	}
	return "", "", false
}

// checkAPIAuthTrigger 检查 API 认证失败触发
func (gcb *GlobalCircuitBreaker) checkAPIAuthTrigger(m circuitMetrics) (CircuitBreakerTrigger, string, bool) {
	t := gcb.config.Triggers.APIAuthFailed
	if !t.Enabled || t.Count <= 0 {
		return "", "", false
	}
	if m.authFailCount >= t.Count {
		return TriggerAPIAuthFailed, fmt.Sprintf("API认证失败次数超限: %d次", m.authFailCount), true
	}
	return "", "", false
}

// checkAllocationTrigger 检查配额超限触发
func (gcb *GlobalCircuitBreaker) checkAllocationTrigger(m circuitMetrics) (CircuitBreakerTrigger, string, bool) {
	if !gcb.config.Triggers.AllocationExceeded.Enabled || !m.allocationAvailable || !m.allocationExceeded {
		return "", "", false
	}
	return TriggerAllocationExceeded, m.allocationReason, true
}

// autoResumeBlockers 自动恢复前检查：会随时间/行情自然回到阈值内的指标必须已恢复。
// 连续亏损与认证失败计数在恢复时重置，不作为阻塞条件（否则停止交易后永远无法恢复）。
func (gcb *GlobalCircuitBreaker) autoResumeBlockers(m circuitMetrics) []string {
	var blockers []string
	health := m.health.validAt(time.Now())
	if gcb.config.Triggers.MaxDrawdown.Enabled && !health.CheckedAt.IsZero() && (!health.Available || !health.DrawdownAvailable || !health.CashFlowAdjusted || !health.Persisted) {
		blockers = append(blockers, "账户权益/现金流或持久化尚未核实")
	}
	if gcb.config.Triggers.AllocationExceeded.Enabled && (!m.allocationAvailable || m.allocationExceeded) {
		blockers = append(blockers, "资金分配状态未核实或仍超限")
	}
	if _, reason, hit := gcb.checkDailyLossTrigger(m); hit {
		blockers = append(blockers, reason)
	}
	if _, reason, hit := gcb.checkMaxDrawdownTrigger(m); hit {
		blockers = append(blockers, reason)
	}
	if gcb.config.Triggers.WebSocketDisconnected.Enabled && !m.lastWSDisconnect.IsZero() {
		blockers = append(blockers, "WebSocket 尚未重连")
	}
	return blockers
}

// checkRecovery 检查恢复条件
func (gcb *GlobalCircuitBreaker) checkRecovery() {
	if !gcb.config.Recovery.AutoResume || gcb.config.Recovery.ManualRequired {
		return
	}
	// pause_duration=0 表示无限期暂停，只能手动恢复
	if gcb.config.Actions.PauseDuration <= 0 {
		return
	}

	gcb.statusMu.RLock()
	trippedAt := gcb.trippedAt
	gcb.statusMu.RUnlock()

	if time.Since(trippedAt) <= time.Duration(gcb.config.Actions.PauseDuration)*time.Second {
		return
	}

	if blockers := gcb.autoResumeBlockers(gcb.snapshotMetrics()); len(blockers) > 0 {
		logger.Warn("⏸️ [全局熔断] 暂停期已过，但指标仍未恢复，继续保持熔断: %s", strings.Join(blockers, "; "))
		return
	}

	if err := gcb.recover(triggeredByAuto); err != nil {
		logger.Warn("⚠️ [全局熔断] 自动恢复跳过: %v", err)
	}
}

// trip 触发熔断
func (gcb *GlobalCircuitBreaker) trip(trigger CircuitBreakerTrigger, triggeredBy, reason string) {
	now := time.Now()

	gcb.statusMu.Lock()
	if gcb.status == CircuitBreakerStatusTripped {
		gcb.statusMu.Unlock()
		logger.Warn("⚠️ [全局熔断] 熔断已触发，忽略重复触发: %s, 原因: %s", trigger, reason)
		return
	}

	metrics := map[string]float64{
		"daily_pnl":          gcb.dailyPnL,
		"max_drawdown":       gcb.maxDrawdown,
		"consecutive_losses": float64(gcb.consecutiveLosses),
	}
	gcb.status = CircuitBreakerStatusTripped
	gcb.trippedAt = now
	gcb.statusMu.Unlock()

	logger.Error("🔴 [全局熔断] 熔断触发! 触发器: %s, 原因: %s, 操作人: %s", trigger, reason, triggeredBy)

	cbEvent := &CircuitBreakerEvent{
		Timestamp:   now,
		Status:      CircuitBreakerStatusTripped,
		Trigger:     trigger,
		TriggeredBy: triggeredBy,
		Reason:      reason,
		Metrics:     metrics,
	}

	gcb.eventsMu.Lock()
	gcb.events = append(gcb.events, cbEvent)
	gcb.eventsMu.Unlock()

	// 发布事件
	if gcb.eventBus != nil {
		gcb.eventBus.Publish(&event.Event{
			Type: event.EventType("circuit_breaker:triggered"),
			Data: map[string]interface{}{
				"event": cbEvent,
			},
		})
	}

	// 1. 停止所有新开仓：同步执行，保证随后的恢复一定发生在暂停之后（否则暂停可能落在恢复之后而永久生效）
	var results []string
	if gcb.config.Actions.StopAllNewOrders {
		results = append(results, gcb.pauseAllBots(string(cbEvent.Trigger)))
	}

	// 其余耗时动作（撤单/平仓/通知）异步执行
	go gcb.executeActions(cbEvent, results)
}

// executeActions 执行熔断动作（撤单、平仓、通知）
func (gcb *GlobalCircuitBreaker) executeActions(cbEvent *CircuitBreakerEvent, results []string) {
	logger.Warn("🚨 [全局熔断] 开始执行熔断动作...")

	// 2. 撤销所有挂单
	if gcb.config.Actions.CancelAllOpenOrders {
		results = append(results, gcb.cancelAllOrders())
	}

	// 3. 平仓（如果启用）
	if gcb.config.Actions.ClosePositions.Enabled {
		results = append(results, gcb.closeAllPositions())
	}

	// 4. 发送通知
	gcb.sendNotification(event.EventTypeRiskTriggered, map[string]interface{}{
		"source":       "global_circuit_breaker",
		"trigger":      string(cbEvent.Trigger),
		"reason":       cbEvent.Reason,
		"triggered_by": cbEvent.TriggeredBy,
		"actions":      strings.Join(results, "; "),
	})

	logger.Warn("✅ [全局熔断] 熔断动作执行完成: %s", strings.Join(results, "; "))
}

// pauseAllBots 暂停所有 Bot 开仓
func (gcb *GlobalCircuitBreaker) pauseAllBots(trigger string) string {
	bots := gcb.botProvider.GetAllBots()
	reason := "circuit_breaker:" + trigger
	_, pauser := gcb.deps()
	if pauser != nil {
		if err := pauser.Pause(circuitBreakerPauseSource, reason, bots); err != nil {
			logger.Error("[全局熔断] 暂停已施加，但持久化风险来源失败，需保持人工核查: %v", err)
		}
	} else {
		for _, bot := range bots {
			pauseBotWithoutAutoResume(bot, reason)
		}
	}
	logger.Info("⏸️ [全局熔断] 已暂停 %d 个 Bot 的开仓", len(bots))
	return fmt.Sprintf("已暂停 %d 个Bot开仓", len(bots))
}

// cancelAllOrders 撤销所有挂单
func (gcb *GlobalCircuitBreaker) cancelAllOrders() string {
	report := cancelOrdersOnBots(gcb.botProvider.GetAllBots())
	if err := report.Err(); err != nil {
		logger.Error("❌ [全局熔断] 撤单存在失败: %v", err)
	}
	summary := report.Summary("已撤单")
	logger.Info("🔄 [全局熔断] %s (状态: %s)", summary, report.Status())
	return summary
}

// closeAllPositions 平仓（每个 Bot 独立超时；timeout<=0 使用默认值）
func (gcb *GlobalCircuitBreaker) closeAllPositions() string {
	cp := gcb.config.Actions.ClosePositions
	report := closePositionsOnBots(context.Background(), gcb.botProvider.GetAllBots(), cp.Method, cp.Timeout)
	if err := report.Err(); err != nil {
		logger.Error("❌ [全局熔断] 平仓存在失败: %v", err)
	}
	summary := report.Summary("已平仓")
	logger.Info("📤 [全局熔断] %s (状态: %s)", summary, report.Status())
	return summary
}

// sendNotification 通过通知服务发送告警
func (gcb *GlobalCircuitBreaker) sendNotification(evtType event.EventType, data map[string]interface{}) {
	if !gcb.config.Notifications.Enabled {
		return
	}
	notifier, _ := gcb.deps()
	if notifier == nil {
		logger.Warn("📢 [全局熔断] 通知已启用但未注入通知服务，仅记录日志: %v", data)
		return
	}
	if len(gcb.config.Notifications.Channels) > 0 {
		// NotificationService 按全局已启用渠道广播，暂不支持按 channels 过滤
		data["channels"] = strings.Join(gcb.config.Notifications.Channels, ",")
	}
	notifier.Send(&event.Event{
		Type:      evtType,
		Timestamp: time.Now(),
		Data:      data,
	})
}

// ManualTrigger 手动触发熔断
func (gcb *GlobalCircuitBreaker) ManualTrigger(triggeredBy, reason string) {
	gcb.trip(TriggerManual, triggeredBy, reason)
}

// ManualRecover 手动恢复
func (gcb *GlobalCircuitBreaker) ManualRecover(triggeredBy string) error {
	logger.Info("🔧 [全局熔断] 手动恢复，操作人: %s", triggeredBy)
	return gcb.recover(triggeredBy)
}

// recover 恢复
func (gcb *GlobalCircuitBreaker) recover(triggeredBy string) error {
	logger.Info("♻️  [全局熔断] 开始恢复...")

	now := time.Now()
	auto := triggeredBy == triggeredByAuto
	reason := "手动恢复"
	if auto {
		reason = "自动恢复"
	}

	gcb.statusMu.Lock()
	if gcb.status != CircuitBreakerStatusTripped {
		gcb.statusMu.Unlock()
		return fmt.Errorf("当前状态不是已触发，无需恢复")
	}
	gcb.status = CircuitBreakerStatusNormal
	gcb.cooldownUntil = now.Add(time.Duration(gcb.config.Recovery.CooldownMinutes) * time.Minute)
	// 计数型指标恢复即清零，避免同一批历史再次触发 → 平仓 → 恢复 → 重建仓的循环
	gcb.consecutiveLosses = 0
	gcb.authFailCount = 0
	gcb.resetMarks.Streak = now
	if !auto {
		// 手动恢复代表操作人确认继续：日内亏损与回撤基线一并重置
		gcb.dailyPnL = 0
		gcb.maxDrawdown = 0
		gcb.resetMarks.All = now
	}
	gcb.statusMu.Unlock()

	// 记录事件
	cbEvent := &CircuitBreakerEvent{
		Timestamp:   now,
		Status:      CircuitBreakerStatusNormal,
		Trigger:     TriggerManual,
		TriggeredBy: triggeredBy,
		Reason:      reason,
	}

	// 恢复所有 Bot
	bots := gcb.botProvider.GetAllBots()
	_, pauser := gcb.deps()
	if pauser != nil {
		resumed, releaseErr := pauser.ReleaseChecked(circuitBreakerPauseSource, bots)
		if releaseErr != nil {
			gcb.statusMu.Lock()
			gcb.status = CircuitBreakerStatusTripped
			gcb.statusMu.Unlock()
			return fmt.Errorf("durably release global circuit-breaker pause: %w", releaseErr)
		}
		if resumed {
			logger.Info("▶️ [全局熔断] 已恢复 %d 个 Bot", len(bots))
		}
	} else {
		for _, bot := range bots {
			bot.ResumeOpening()
		}
		logger.Info("▶️ [全局熔断] 已恢复 %d 个 Bot", len(bots))
	}

	gcb.eventsMu.Lock()
	gcb.events = append(gcb.events, cbEvent)
	gcb.eventsMu.Unlock()

	// 发布事件
	if gcb.eventBus != nil {
		gcb.eventBus.Publish(&event.Event{
			Type: event.EventType("circuit_breaker:recovered"),
			Data: map[string]interface{}{
				"event": cbEvent,
			},
		})
	}

	gcb.sendNotification(event.EventTypeRiskRecovered, map[string]interface{}{
		"source":       "global_circuit_breaker",
		"reason":       reason,
		"triggered_by": triggeredBy,
	})

	return nil
}

// UpdateMetrics 更新统计数据
func (gcb *GlobalCircuitBreaker) UpdateMetrics(dailyPnL, maxDrawdown float64, consecutiveLosses int) {
	if err := gcb.UpdateExternalMetrics(dailyPnL, maxDrawdown, consecutiveLosses); err != nil {
		logger.Warn("[全局熔断] 拒绝外部指标覆盖: %v", err)
	}
}

// UpdateExternalMetrics cannot overwrite an authoritative feeder sample or
// certify hand-entered numbers as reconciled account evidence.
func (gcb *GlobalCircuitBreaker) UpdateExternalMetrics(dailyPnL, maxDrawdown float64, consecutiveLosses int) error {
	gcb.statusMu.Lock()
	defer gcb.statusMu.Unlock()
	if !gcb.metricsHealth.CheckedAt.IsZero() {
		return fmt.Errorf("账户指标由内部喂数器管理，禁止外部覆盖")
	}
	if !finiteEquity(dailyPnL) || !finiteEquity(maxDrawdown) || maxDrawdown < 0 || consecutiveLosses < 0 {
		return fmt.Errorf("无效风险指标")
	}
	gcb.dailyPnL = dailyPnL
	gcb.maxDrawdown = maxDrawdown
	gcb.consecutiveLosses = consecutiveLosses
	return nil
}

// MetricsResetMarks 返回统计基线（供 MetricsFeeder 使用）
func (gcb *GlobalCircuitBreaker) MetricsResetMarks() MetricsResetMarks {
	gcb.statusMu.RLock()
	defer gcb.statusMu.RUnlock()
	return gcb.resetMarks
}

// ReportAuthFailure 报告 API 认证失败
func (gcb *GlobalCircuitBreaker) ReportAuthFailure() {
	gcb.statusMu.Lock()
	defer gcb.statusMu.Unlock()

	gcb.authFailCount++
	logger.Warn("⚠️ [全局熔断] API认证失败计数: %d", gcb.authFailCount)
}

// ReportWebSocketDisconnect 报告 WebSocket 断线
func (gcb *GlobalCircuitBreaker) ReportWebSocketDisconnect(connectionID ...string) {
	gcb.statusMu.Lock()
	defer gcb.statusMu.Unlock()

	if gcb.wsDisconnected == nil {
		gcb.wsDisconnected = make(map[string]time.Time)
	}
	key := connectivityKey(connectionID)
	if _, exists := gcb.wsDisconnected[key]; !exists {
		gcb.wsDisconnected[key] = time.Now()
	}
	gcb.refreshDisconnectTimeLocked()
	logger.Warn("⚠️ [全局熔断] WebSocket断线")
}

// ReportWebSocketReconnected 报告 WebSocket 重连
func (gcb *GlobalCircuitBreaker) ReportWebSocketReconnected(connectionID ...string) {
	gcb.statusMu.Lock()
	defer gcb.statusMu.Unlock()

	delete(gcb.wsDisconnected, connectivityKey(connectionID))
	gcb.refreshDisconnectTimeLocked()
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
	out := make([]*CircuitBreakerEvent, limit)
	copy(out, gcb.events[len(gcb.events)-limit:])
	return out
}

// IsTripped 是否已触发
func (gcb *GlobalCircuitBreaker) IsTripped() bool {
	return gcb.GetStatus() == CircuitBreakerStatusTripped
}

// GetConfig 获取配置
func (gcb *GlobalCircuitBreaker) GetConfig() *config.CircuitBreakerConfig {
	return gcb.config
}
