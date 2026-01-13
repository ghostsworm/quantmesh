package event

import (
	"time"

	"quantmesh/logger"
)

// EventType 事件类型
type EventType string

const (
	// 订单相关事件
	EventTypeOrderPlaced        EventType = "order_placed"
	EventTypeOrderFilled        EventType = "order_filled"
	EventTypeOrderCanceled      EventType = "order_canceled"
	EventTypeOrderFailed        EventType = "order_failed" // 订单失败
	
	// 持仓相关事件
	EventTypePositionOpened     EventType = "position_opened"
	EventTypePositionClosed     EventType = "position_closed"
	
	// 风控相关事件
	EventTypeRiskTriggered      EventType = "risk_triggered"
	EventTypeRiskRecovered      EventType = "risk_recovered"
	EventTypeStopLoss           EventType = "stop_loss"
	EventTypeTakeProfit         EventType = "take_profit"
	EventTypeMarginInsufficient EventType = "margin_insufficient" // 保证金不足
	EventTypeAllocationExceeded EventType = "allocation_exceeded" // 超出资金分配限制
	
	// 网络相关事件
	EventTypeWebSocketDisconnected EventType = "websocket_disconnected" // WebSocket 断连
	EventTypeWebSocketReconnected  EventType = "websocket_reconnected"  // WebSocket 重连
	EventTypeAPIRequestFailed      EventType = "api_request_failed"     // API 请求失败
	EventTypeConnectionTimeout     EventType = "connection_timeout"     // 连接超时
	
	// API 错误事件
	EventTypeAPIRateLimited   EventType = "api_rate_limited"   // API 限流 (429)
	EventTypeAPIServerError   EventType = "api_server_error"   // 服务器错误 (5xx)
	EventTypeAPIAuthFailed    EventType = "api_auth_failed"    // 认证失败
	EventTypeAPIBadRequest    EventType = "api_bad_request"    // 请求错误 (4xx)
	
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
		EventTypeOrderFailed:
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
		EventTypeSystemStart:
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
		
	case EventTypePositionOpened, EventTypePositionClosed:
		return SourceExchange
		
	case EventTypeRiskTriggered, EventTypeRiskRecovered, EventTypeStopLoss, EventTypeTakeProfit,
		EventTypeMarginInsufficient, EventTypeAllocationExceeded:
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
		
		// 持仓相关
		EventTypePositionOpened: "持仓已开仓",
		EventTypePositionClosed: "持仓已平仓",
		
		// 风控相关
		EventTypeRiskTriggered:      "风控触发",
		EventTypeRiskRecovered:      "风控恢复",
		EventTypeStopLoss:           "止损触发",
		EventTypeTakeProfit:         "止盈触发",
		EventTypeMarginInsufficient: "保证金不足",
		EventTypeAllocationExceeded: "资金分配超限",
		
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
		EventTypePriceVolatility: "价格大幅波动",
		EventTypePriceAnomaly:    "价格异常",
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

// EventBus 事件总线
type EventBus struct {
	eventCh    chan *Event
	bufferSize int
}

// NewEventBus 创建事件总线
func NewEventBus(bufferSize int) *EventBus {
	if bufferSize <= 0 {
		bufferSize = 1000 // 默认1000
	}
	return &EventBus{
		eventCh:    make(chan *Event, bufferSize),
		bufferSize: bufferSize,
	}
}

// Publish 发布事件（非阻塞）
func (eb *EventBus) Publish(event *Event) {
	if event == nil {
		return
	}

	// 设置时间戳
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}

	select {
	case eb.eventCh <- event:
		// 成功发布
	default:
		// Channel 满了，记录警告但不阻塞
		logger.Warn("⚠️ 事件队列已满，丢弃事件: %s", event.Type)
	}
}

// Subscribe 订阅事件（返回 channel）
func (eb *EventBus) Subscribe() <-chan *Event {
	return eb.eventCh
}

// Close 关闭事件总线
func (eb *EventBus) Close() {
	close(eb.eventCh)
}
