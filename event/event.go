package event

import (
	"sync"
	"time"

	"quantmesh/logger"
)

// EventType 事件類型
type EventType string

const (
	// 訂單相關事件
	EventTypeOrderPlaced   EventType = "order_placed"
	EventTypeOrderFilled   EventType = "order_filled"
	EventTypeOrderCanceled EventType = "order_canceled"
	EventTypeOrderFailed   EventType = "order_failed" // 订單失败

	// 交易运行状態事件
	EventTypeTradingStarted     EventType = "trading_started"      // 交易啟动成功
	EventTypeTradingStopped     EventType = "trading_stopped"      // 交易停止成功
	EventTypeTradingStartFailed EventType = "trading_start_failed" // 交易啟动失败
	EventTypeTradingStopFailed  EventType = "trading_stop_failed"  // 交易停止失败

	// 持倉相关事件
	EventTypePositionOpened EventType = "position_opened"
	EventTypePositionClosed EventType = "position_closed"
	EventTypePositionPlanCompleted EventType = "position_plan_completed" // 倉位计划达成

	// 风控相关事件
	EventTypeRiskTriggered      EventType = "risk_triggered"
	EventTypeRiskRecovered      EventType = "risk_recovered"
	EventTypeStopLoss           EventType = "stop_loss"
	EventTypeTakeProfit         EventType = "take_profit"
	EventTypeMarginInsufficient    EventType = "margin_insufficient"      // 保证金不足
	EventTypeAllocationExceeded    EventType = "allocation_exceeded"     // 超出资金分配限制
	EventTypeAllocationLimitChanged EventType = "allocation_limit_changed" // 资金限額变更（正常/紧急模式切换）

	// 网络相关事件
	EventTypeWebSocketDisconnected EventType = "websocket_disconnected" // WebSocket 断连
	EventTypeWebSocketReconnected  EventType = "websocket_reconnected"  // WebSocket 重连
	EventTypeAPIRequestFailed      EventType = "api_request_failed"     // API 请求失败
	EventTypeConnectionTimeout     EventType = "connection_timeout"     // 连接超時

	// API 錯误事件
	EventTypeAPIRateLimited EventType = "api_rate_limited" // API 限流 (429)
	EventTypeAPIServerError EventType = "api_server_error" // 服務器錯误 (5xx)
	EventTypeAPIAuthFailed  EventType = "api_auth_failed"  // 认证失败
	EventTypeAPIBadRequest  EventType = "api_bad_request"  // 请求錯误 (4xx)

	// 價格波動事件
	EventTypePriceVolatility EventType = "price_volatility" // 價格大幅波动
	EventTypePriceAnomaly    EventType = "price_anomaly"    // 價格异常

	// 下單校驗事件
	EventTypePrecisionAdjustment EventType = "precision_adjustment" // 精度調整告警

	// 系统资源事件
	EventTypeSystemCPUHigh    EventType = "system_cpu_high"    // CPU 使用率過高
	EventTypeSystemMemoryHigh EventType = "system_memory_high" // 記憶體使用率過高
	EventTypeSystemDiskFull   EventType = "system_disk_full"   // 磁盘空间不足

	// 系统状態事件
	EventTypeError       EventType = "error"
	EventTypeSystemStart EventType = "system_start"
	EventTypeSystemStop  EventType = "system_stop"

	// 智子巡檢報告（定時彙總或緊急事件）
	EventTypeInspectorReport EventType = "inspector_report"
)

// EventSeverity 事件严重程度
type EventSeverity string

const (
	SeverityCritical EventSeverity = "critical" // 严重：影响交易的关键问题
	SeverityWarning  EventSeverity = "warning"  // 警告：需要关注但不影响交易
	SeverityInfo     EventSeverity = "info"     // 信息：一般性信息事件
)

// GetEventSeverity 根據事件類型獲取默认严重程度
func GetEventSeverity(eventType EventType) EventSeverity {
	switch eventType {
	// Critical 级别：影响交易的严重问题
	case EventTypeRiskTriggered,
		EventTypeStopLoss,
		EventTypeMarginInsufficient,
		EventTypeAllocationExceeded,
		EventTypeWebSocketDisconnected,
		EventTypeAPIServerError,
		EventTypeAPIAuthFailed,
		EventTypeSystemCPUHigh,
		EventTypeSystemMemoryHigh,
		EventTypeSystemDiskFull,
		EventTypeSystemStop,
		EventTypeOrderFailed,
		EventTypeTradingStartFailed,
		EventTypeTradingStopFailed:
		return SeverityCritical

	// Warning 级别：需要关注的告警
	case EventTypeAPIRateLimited,
		EventTypeAPIRequestFailed,
		EventTypeConnectionTimeout,
		EventTypePriceVolatility,
		EventTypePriceAnomaly,
		EventTypeRiskRecovered,
		EventTypeAPIBadRequest,
		EventTypePrecisionAdjustment,
		EventTypeError:
		return SeverityWarning

	// Info 级别：一般信息事件
	case EventTypeOrderPlaced,
		EventTypeOrderFilled,
		EventTypeOrderCanceled,
		EventTypePositionOpened,
		EventTypePositionClosed,
		EventTypePositionPlanCompleted,
		EventTypeTakeProfit,
		EventTypeWebSocketReconnected,
		EventTypeSystemStart,
		EventTypeTradingStarted,
		EventTypeTradingStopped,
		EventTypeInspectorReport:
		return SeverityInfo

	default:
		return SeverityInfo
	}
}

// Event 事件結構
type Event struct {
	Type      EventType
	Timestamp time.Time
	Data      map[string]interface{}
}

// EventSource 事件源
type EventSource string

const (
	SourceExchange EventSource = "exchange" // 交易所相关
	SourceNetwork  EventSource = "network"  // 网络相关
	SourceSystem   EventSource = "system"   // 系统相关
	SourceStrategy EventSource = "strategy" // 策略相关
	SourceRisk     EventSource = "risk"     // 风控相关
	SourceAPI      EventSource = "api"      // API相关
)

// GetEventSource 根據事件類型獲取事件源
func GetEventSource(eventType EventType) EventSource {
	switch eventType {
	case EventTypeOrderPlaced, EventTypeOrderFilled, EventTypeOrderCanceled, EventTypeOrderFailed:
		return SourceExchange

	case EventTypeTradingStarted, EventTypeTradingStopped, EventTypeTradingStartFailed, EventTypeTradingStopFailed:
		return SourceSystem

	case EventTypePositionOpened, EventTypePositionClosed:
		return SourceExchange

	case EventTypeRiskTriggered, EventTypeRiskRecovered, EventTypeStopLoss, EventTypeTakeProfit,
		EventTypeMarginInsufficient, EventTypeAllocationExceeded, EventTypeAllocationLimitChanged,
		EventTypePositionPlanCompleted:
		return SourceRisk

	case EventTypeWebSocketDisconnected, EventTypeWebSocketReconnected,
		EventTypeAPIRequestFailed, EventTypeConnectionTimeout:
		return SourceNetwork

	case EventTypeAPIRateLimited, EventTypeAPIServerError, EventTypeAPIAuthFailed, EventTypeAPIBadRequest:
		return SourceAPI

	case EventTypePriceVolatility, EventTypePriceAnomaly, EventTypePrecisionAdjustment:
		return SourceStrategy

	case EventTypeSystemCPUHigh, EventTypeSystemMemoryHigh, EventTypeSystemDiskFull,
		EventTypeSystemStart, EventTypeSystemStop, EventTypeError,
		EventTypeInspectorReport:
		return SourceSystem

	default:
		return SourceSystem
	}
}

// GetEventTitle 獲取事件標题（中文）
func GetEventTitle(eventType EventType) string {
	titles := map[EventType]string{
		// 訂單相關
		EventTypeOrderPlaced:   "订單已下單",
		EventTypeOrderFilled:   "订單已成交",
		EventTypeOrderCanceled: "订單已取消",
		EventTypeOrderFailed:   "订單失败",

		// 交易运行状態
		EventTypeTradingStarted:     "交易已啟动",
		EventTypeTradingStopped:     "交易已停止",
		EventTypeTradingStartFailed: "交易啟动失败",
		EventTypeTradingStopFailed:  "交易停止失败",

		// 持倉相关
		EventTypePositionOpened: "持倉已开倉",
		EventTypePositionClosed: "持倉已平倉",
		EventTypePositionPlanCompleted: "倉位计划达成",

		// 风控相关
		EventTypeRiskTriggered:      "风控触发",
		EventTypeRiskRecovered:      "风控恢複",
		EventTypeStopLoss:           "止损触发",
		EventTypeTakeProfit:            "止盈触发",
		EventTypeMarginInsufficient:    "保证金不足",
		EventTypeAllocationExceeded:    "资金分配超限",
		EventTypeAllocationLimitChanged: "资金限額变更",

		// 网络相关
		EventTypeWebSocketDisconnected: "WebSocket 断开连接",
		EventTypeWebSocketReconnected:  "WebSocket 重新连接",
		EventTypeAPIRequestFailed:      "API 请求失败",
		EventTypeConnectionTimeout:     "连接超時",

		// API 錯误
		EventTypeAPIRateLimited: "API 限流",
		EventTypeAPIServerError: "API 服務器錯误",
		EventTypeAPIAuthFailed:  "API 认证失败",
		EventTypeAPIBadRequest:  "API 请求錯误",

		// 價格波動
		EventTypePriceVolatility:     "價格大幅波动",
		EventTypePriceAnomaly:        "價格异常",
		EventTypePrecisionAdjustment: "下單精度异常",

		// 系统资源
		EventTypeSystemCPUHigh:    "CPU 使用率過高",
		EventTypeSystemMemoryHigh: "記憶體使用率過高",
		EventTypeSystemDiskFull:   "磁盘空间不足",

		// 系统状態
		EventTypeError:       "系统錯误",
		EventTypeSystemStart: "系统啟动",
		EventTypeSystemStop:  "系统停止",

		// 智子巡檢
		EventTypeInspectorReport: "智子巡檢報告",
	}

	if title, ok := titles[eventType]; ok {
		return title
	}
	return string(eventType)
}

// EventBus 事件總線（支援多订阅者）
type EventBus struct {
	subscribers []chan *Event
	mu          sync.RWMutex
	bufferSize  int
	closed      bool
	// 事件去重：記錄最近发送的事件類型和時间戳
	dedupMap    map[EventType]time.Time
	dedupMu     sync.RWMutex
	dedupWindow time.Duration // 去重時间窗口，默认30秒
}

// NewEventBus 創建事件總線
func NewEventBus(bufferSize int) *EventBus {
	if bufferSize <= 0 {
		bufferSize = 5000 // 默认5000（增加默认值）
	}
	eb := &EventBus{
		subscribers: make([]chan *Event, 0),
		bufferSize:  bufferSize,
		closed:      false,
		dedupMap:    make(map[EventType]time.Time),
		dedupWindow: 30 * time.Second, // 默认30秒去重窗口
	}

	// 啟动定期清理去重map的goroutine
	go eb.startDedupCleanup()

	return eb
}

// startDedupCleanup 啟动定期清理去重map的goroutine
func (eb *EventBus) startDedupCleanup() {
	ticker := time.NewTicker(5 * time.Minute) // 每5分钟清理一次
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			// 检查是否已关闭
			eb.mu.RLock()
			closed := eb.closed
			eb.mu.RUnlock()

			if closed {
				return
			}

			eb.cleanupDedupMap()
		}
	}
}

// shouldDeduplicate 判断事件類型是否需要去重
func (eb *EventBus) shouldDeduplicate(eventType EventType) bool {
	// 需要去重的事件類型（频繁触发且重複無意义的）
	dedupTypes := map[EventType]bool{
		EventTypeAllocationExceeded:     true, // 资金分配超限
		EventTypeAllocationLimitChanged: false, // 资金限額变更（不去重，每次都通知）
		EventTypeMarginInsufficient:     true, // 保证金不足
		EventTypeAPIRateLimited:         true, // API 限流
		EventTypeAPIRequestFailed:    true, // API 请求失败
		EventTypePrecisionAdjustment: true, // 精度調整
	}
	return dedupTypes[eventType]
}

// checkDeduplicate 检查事件是否需要去重（返回true表示应該发送，false表示应該跳過）
func (eb *EventBus) checkDeduplicate(eventType EventType) bool {
	if !eb.shouldDeduplicate(eventType) {
		return true // 不需要去重，直接发送
	}

	eb.dedupMu.Lock()
	defer eb.dedupMu.Unlock()

	now := time.Now()
	lastSent, exists := eb.dedupMap[eventType]

	if !exists || now.Sub(lastSent) >= eb.dedupWindow {
		// 可以发送：首次发送或已超過時间窗口
		eb.dedupMap[eventType] = now
		return true
	}

	// 跳過：在時间窗口内，已发送過相同類型的事件
	return false
}

// Publish 发布事件（非阻塞，发送给所有订阅者）
func (eb *EventBus) Publish(event *Event) {
	if event == nil {
		return
	}

	// 設置時间戳
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}

	// 检查是否需要去重
	if !eb.checkDeduplicate(event.Type) {
		// 事件被去重，跳過发送（但記錄日志以便調試）
		logger.Debug("🔇 事件已去重，跳過发送: %s (在 %v 内已发送過)", event.Type, eb.dedupWindow)
		return
	}

	eb.mu.RLock()
	defer eb.mu.RUnlock()

	if eb.closed {
		return
	}

	// 发送给所有订阅者
	for _, ch := range eb.subscribers {
		select {
		case ch <- event:
			// 成功发布
		default:
			// Channel 满了，記錄警告但不阻塞
			logger.Warn("⚠️ 事件队列已满，丢弃事件: %s", event.Type)
		}
	}
}

// Subscribe 订阅事件（返回新的 channel，支援多個订阅者）
func (eb *EventBus) Subscribe() <-chan *Event {
	eb.mu.Lock()
	defer eb.mu.Unlock()

	ch := make(chan *Event, eb.bufferSize)
	eb.subscribers = append(eb.subscribers, ch)
	return ch
}

// Unsubscribe 取消订阅（可選，用於清理资源）
func (eb *EventBus) Unsubscribe(ch <-chan *Event) {
	eb.mu.Lock()
	defer eb.mu.Unlock()

	for i, sub := range eb.subscribers {
		if sub == ch {
			eb.subscribers = append(eb.subscribers[:i], eb.subscribers[i+1:]...)
			close(sub)
			break
		}
	}
}

// cleanupDedupMap 清理去重map中的過期記錄（定期調用）
func (eb *EventBus) cleanupDedupMap() {
	eb.dedupMu.Lock()
	defer eb.dedupMu.Unlock()

	now := time.Now()
	for eventType, lastSent := range eb.dedupMap {
		// 刪除超過2倍時间窗口的記錄（避免記憶體泄漏）
		if now.Sub(lastSent) >= eb.dedupWindow*2 {
			delete(eb.dedupMap, eventType)
		}
	}
}

// Close 关闭事件總線
func (eb *EventBus) Close() {
	eb.mu.Lock()
	defer eb.mu.Unlock()

	if eb.closed {
		return
	}
	eb.closed = true

	for _, ch := range eb.subscribers {
		close(ch)
	}
	eb.subscribers = nil

	// 清理去重map
	eb.dedupMu.Lock()
	eb.dedupMap = nil
	eb.dedupMu.Unlock()
}
