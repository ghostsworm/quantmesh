package event

import (
	"time"

	"quantmesh/logger"
)

// EventType 事件类型
type EventType string

const (
	EventTypeOrderPlaced    EventType = "order_placed"
	EventTypeOrderFilled    EventType = "order_filled"
	EventTypeOrderCanceled  EventType = "order_canceled"
	EventTypePositionOpened EventType = "position_opened"
	EventTypePositionClosed EventType = "position_closed"
	EventTypeRiskTriggered  EventType = "risk_triggered"
	EventTypeRiskRecovered  EventType = "risk_recovered"
	EventTypeStopLoss       EventType = "stop_loss"
	EventTypeTakeProfit     EventType = "take_profit"
	EventTypeError          EventType = "error"
	EventTypeSystemStart    EventType = "system_start"
	EventTypeSystemStop     EventType = "system_stop"
)

// Event 事件结构
type Event struct {
	Type      EventType
	Timestamp time.Time
	Data      map[string]interface{}
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
