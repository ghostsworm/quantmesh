package event

import (
	"sync"
	"time"

	"quantmesh/logger"
)

// EventType 事件类型
type EventType string

const (
	// 订单相关事件
	EventTypeOrderPlaced   EventType = "order_placed"
	EventTypeOrderFilled   EventType = "order_filled"
	EventTypeOrderCanceled EventType = "order_canceled"
	EventTypeOrderFailed   EventType = "order_failed" // 订单失败

	// 交易运行状态事件
	EventTypeTradingStarted     EventType = "trading_started"      // 交易启动成功
	EventTypeTradingStopped     EventType = "trading_stopped"      // 交易停止成功
	EventTypeTradingStartFailed EventType = "trading_start_failed" // 交易启动失败
	EventTypeTradingStopFailed  EventType = "trading_stop_failed"  // 交易停止失败

	// 持仓相关事件
	EventTypePositionOpened EventType = "position_opened"
	EventTypePositionClosed EventType = "position_closed"

	// 风控相关事件
	EventTypeRiskTriggered      EventType = "risk_triggered"
	EventTypeRiskRecovered      EventType = "risk_recovered"
	EventTypeStopLoss           EventType = "stop_loss"
	EventTypeTakeProfit         EventType = "take_profit"
	EventTypeMarginInsufficient    EventType = "margin_insufficient"      // 保证金不足
	EventTypeAllocationExceeded    EventType = "allocation_exceeded"     // 超出资金分配限制
	EventTypeAllocationLimitChanged EventType = "allocation_limit_changed" // 资金限额变更（正常/紧急模式切换）

	// 网络相关事件
	EventTypeWebSocketDisconnected EventType = "websocket_disconnected" // WebSocket 断连
	EventTypeWebSocketReconnected  EventType = "websocket_reconnected"  // WebSocket 重连
	EventTypeAPIRequestFailed      EventType = "api_request_failed"     // API 请求失败
	EventTypeConnectionTimeout     EventType = "connection_timeout"     // 连接超时

	// API 错误事件
	EventTypeAPIRateLimited EventType = "api_rate_limited" // API 限流 (429)
	EventTypeAPIServerError EventType = "api_server_error" // 服务器错误 (5xx)
	EventTypeAPIAuthFailed  EventType = "api_auth_failed"  // 认证失败
	EventTypeAPIBadRequest  EventType = "api_bad_request"  // 请求错误 (4xx)

	// 价格波动事件
	EventTypePriceVolatility EventType = "price_volatility" // 价格大幅波动
	EventTypePriceAnomaly    EventType = "price_anomaly"    // 价格异常

	// 下单校验事件
	EventTypePrecisionAdjustment EventType = "precision_adjustment" // 精度调整告警

	// 系统资源事件
	EventTypeSystemCPUHigh    EventType = "system_cpu_high"    // CPU 使用率过高
	EventTypeSystemMemoryHigh EventType = "system_memory_high" // 内存使用率过高
	EventTypeSystemDiskFull   EventType = "system_disk_full"   // 磁盘空间不足

	// 系统状态事件
	EventTypeError       EventType = "error"
	EventTypeSystemStart EventType = "system_start"
	EventTypeSystemStop  EventType = "system_stop"
)

// EventSeverity 事件严重程度
type EventSeverity string

const (
	SeverityCritical EventSeverity = "critical" // 严重：影响交易的关键问题
	SeverityWarning  EventSeverity = "warning"  // 警告：需要关注但不影响交易
	SeverityInfo     EventSeverity = "info"     // 信息：一般性信息事件
)

// GetEventSeverity 根据事件类型获取默认严重程度
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
		EventTypeTakeProfit,
		EventTypeWebSocketReconnected,
		EventTypeSystemStart,
		EventTypeTradingStarted,
		EventTypeTradingStopped:
		return SeverityInfo

	default:
		return SeverityInfo
	}
}

// Event 事件结构
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

// GetEventSource 根据事件类型获取事件源
func GetEventSource(eventType EventType) EventSource {
	switch eventType {
	case EventTypeOrderPlaced, EventTypeOrderFilled, EventTypeOrderCanceled, EventTypeOrderFailed:
		return SourceExchange

	case EventTypeTradingStarted, EventTypeTradingStopped, EventTypeTradingStartFailed, EventTypeTradingStopFailed:
		return SourceSystem

	case EventTypePositionOpened, EventTypePositionClosed:
		return SourceExchange

	case EventTypeRiskTriggered, EventTypeRiskRecovered, EventTypeStopLoss, EventTypeTakeProfit,
		EventTypeMarginInsufficient, EventTypeAllocationExceeded, EventTypeAllocationLimitChanged:
		return SourceRisk

	case EventTypeWebSocketDisconnected, EventTypeWebSocketReconnected,
		EventTypeAPIRequestFailed, EventTypeConnectionTimeout:
		return SourceNetwork

	case EventTypeAPIRateLimited, EventTypeAPIServerError, EventTypeAPIAuthFailed, EventTypeAPIBadRequest:
		return SourceAPI

	case EventTypePriceVolatility, EventTypePriceAnomaly, EventTypePrecisionAdjustment:
		return SourceStrategy

	case EventTypeSystemCPUHigh, EventTypeSystemMemoryHigh, EventTypeSystemDiskFull,
		EventTypeSystemStart, EventTypeSystemStop, EventTypeError:
		return SourceSystem

	default:
		return SourceSystem
	}
}

// GetEventTitle 获取事件标题（中文）
func GetEventTitle(eventType EventType) string {
	titles := map[EventType]string{
		// 订单相关
		EventTypeOrderPlaced:   "订单已下单",
		EventTypeOrderFilled:   "订单已成交",
		EventTypeOrderCanceled: "订单已取消",
		EventTypeOrderFailed:   "订单失败",

		// 交易运行状态
		EventTypeTradingStarted:     "交易已启动",
		EventTypeTradingStopped:     "交易已停止",
		EventTypeTradingStartFailed: "交易启动失败",
		EventTypeTradingStopFailed:  "交易停止失败",

		// 持仓相关
		EventTypePositionOpened: "持仓已开仓",
		EventTypePositionClosed: "持仓已平仓",

		// 风控相关
		EventTypeRiskTriggered:      "风控触发",
		EventTypeRiskRecovered:      "风控恢复",
		EventTypeStopLoss:           "止损触发",
		EventTypeTakeProfit:            "止盈触发",
		EventTypeMarginInsufficient:    "保证金不足",
		EventTypeAllocationExceeded:    "资金分配超限",
		EventTypeAllocationLimitChanged: "资金限额变更",

		// 网络相关
		EventTypeWebSocketDisconnected: "WebSocket 断开连接",
		EventTypeWebSocketReconnected:  "WebSocket 重新连接",
		EventTypeAPIRequestFailed:      "API 请求失败",
		EventTypeConnectionTimeout:     "连接超时",

		// API 错误
		EventTypeAPIRateLimited: "API 限流",
		EventTypeAPIServerError: "API 服务器错误",
		EventTypeAPIAuthFailed:  "API 认证失败",
		EventTypeAPIBadRequest:  "API 请求错误",

		// 价格波动
		EventTypePriceVolatility:     "价格大幅波动",
		EventTypePriceAnomaly:        "价格异常",
		EventTypePrecisionAdjustment: "下单精度异常",

		// 系统资源
		EventTypeSystemCPUHigh:    "CPU 使用率过高",
		EventTypeSystemMemoryHigh: "内存使用率过高",
		EventTypeSystemDiskFull:   "磁盘空间不足",

		// 系统状态
		EventTypeError:       "系统错误",
		EventTypeSystemStart: "系统启动",
		EventTypeSystemStop:  "系统停止",
	}

	if title, ok := titles[eventType]; ok {
		return title
	}
	return string(eventType)
}

// EventBus 事件总线（支持多订阅者）
type EventBus struct {
	subscribers []chan *Event
	mu          sync.RWMutex
	bufferSize  int
	closed      bool
	// 事件去重：记录最近发送的事件类型和时间戳
	dedupMap    map[EventType]time.Time
	dedupMu     sync.RWMutex
	dedupWindow time.Duration // 去重时间窗口，默认30秒
}

// NewEventBus 创建事件总线
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

	// 启动定期清理去重map的goroutine
	go eb.startDedupCleanup()

	return eb
}

// startDedupCleanup 启动定期清理去重map的goroutine
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

// shouldDeduplicate 判断事件类型是否需要去重
func (eb *EventBus) shouldDeduplicate(eventType EventType) bool {
	// 需要去重的事件类型（频繁触发且重复无意义的）
	dedupTypes := map[EventType]bool{
		EventTypeAllocationExceeded:     true, // 资金分配超限
		EventTypeAllocationLimitChanged: false, // 资金限额变更（不去重，每次都通知）
		EventTypeMarginInsufficient:     true, // 保证金不足
		EventTypeAPIRateLimited:         true, // API 限流
		EventTypeAPIRequestFailed:    true, // API 请求失败
		EventTypePrecisionAdjustment: true, // 精度调整
	}
	return dedupTypes[eventType]
}

// checkDeduplicate 检查事件是否需要去重（返回true表示应该发送，false表示应该跳过）
func (eb *EventBus) checkDeduplicate(eventType EventType) bool {
	if !eb.shouldDeduplicate(eventType) {
		return true // 不需要去重，直接发送
	}

	eb.dedupMu.Lock()
	defer eb.dedupMu.Unlock()

	now := time.Now()
	lastSent, exists := eb.dedupMap[eventType]

	if !exists || now.Sub(lastSent) >= eb.dedupWindow {
		// 可以发送：首次发送或已超过时间窗口
		eb.dedupMap[eventType] = now
		return true
	}

	// 跳过：在时间窗口内，已发送过相同类型的事件
	return false
}

// Publish 发布事件（非阻塞，发送给所有订阅者）
func (eb *EventBus) Publish(event *Event) {
	if event == nil {
		return
	}

	// 设置时间戳
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}

	// 检查是否需要去重
	if !eb.checkDeduplicate(event.Type) {
		// 事件被去重，跳过发送（但记录日志以便调试）
		logger.Debug("🔇 事件已去重，跳过发送: %s (在 %v 内已发送过)", event.Type, eb.dedupWindow)
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
			// Channel 满了，记录警告但不阻塞
			logger.Warn("⚠️ 事件队列已满，丢弃事件: %s", event.Type)
		}
	}
}

// Subscribe 订阅事件（返回新的 channel，支持多个订阅者）
func (eb *EventBus) Subscribe() <-chan *Event {
	eb.mu.Lock()
	defer eb.mu.Unlock()

	ch := make(chan *Event, eb.bufferSize)
	eb.subscribers = append(eb.subscribers, ch)
	return ch
}

// Unsubscribe 取消订阅（可选，用于清理资源）
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

// cleanupDedupMap 清理去重map中的过期记录（定期调用）
func (eb *EventBus) cleanupDedupMap() {
	eb.dedupMu.Lock()
	defer eb.dedupMu.Unlock()

	now := time.Now()
	for eventType, lastSent := range eb.dedupMap {
		// 删除超过2倍时间窗口的记录（避免内存泄漏）
		if now.Sub(lastSent) >= eb.dedupWindow*2 {
			delete(eb.dedupMap, eventType)
		}
	}
}

// Close 关闭事件总线
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
