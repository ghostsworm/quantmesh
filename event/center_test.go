package event

import (
	"context"
	"strings"
	"testing"
	"time"
)

// MockDatabase 模拟數據库
type MockDatabase struct {
	events []*mockEventRecord
}

type mockEventRecord struct {
	Type      string
	Severity  string
	Source    string
	Title     string
	Message   string
	CreatedAt time.Time
}

func (m *MockDatabase) SaveEvent(ctx context.Context, event interface{}) error {
	// 简化测試，不實際保存
	return nil
}

// MockNotifier 模拟通知服務
type MockNotifier struct {
	notifications []*Event
}

func (m *MockNotifier) Send(event *Event) {
	m.notifications = append(m.notifications, event)
}

func TestEventCenterBasic(t *testing.T) {
	// 創建事件總線
	eventBus := NewEventBus(100)
	if eventBus == nil {
		t.Fatal("Failed to create event bus")
	}

	// 創建模拟數據库和通知服務
	mockDB := &MockDatabase{}
	mockNotifier := &MockNotifier{}
	if mockDB == nil || mockNotifier == nil {
		t.Fatal("Failed to create mock services")
	}

	// 創建事件中心配置
	config := &EventCenterConfig{
		Enabled:                  true,
		PriceVolatilityThreshold: 5.0,
		MonitoredSymbols:         []string{"BTCUSDT"},
		CleanupInterval:          24,
		Retention: RetentionConfig{
			CriticalDays:     365,
			WarningDays:      90,
			InfoDays:         30,
			CriticalMaxCount: 1000000,
			WarningMaxCount:  500000,
			InfoMaxCount:     300000,
		},
	}

	if config.PriceVolatilityThreshold != 5.0 {
		t.Errorf("Expected threshold 5.0, got %f", config.PriceVolatilityThreshold)
	}

	t.Log("✅ 事件中心配置創建成功")
}

func TestEventSeverity(t *testing.T) {
	tests := []struct {
		eventType EventType
		expected  EventSeverity
	}{
		{EventTypeWebSocketDisconnected, SeverityCritical},
		{EventTypeAPIRateLimited, SeverityWarning},
		{EventTypeOrderPlaced, SeverityInfo},
		{EventTypePriceVolatility, SeverityWarning},
	}

	for _, tt := range tests {
		severity := GetEventSeverity(tt.eventType)
		if severity != tt.expected {
			t.Errorf("GetEventSeverity(%s) = %s, want %s", tt.eventType, severity, tt.expected)
		}
	}

	t.Log("✅ 事件严重程度测試通過")
}

func TestEventSource(t *testing.T) {
	tests := []struct {
		eventType EventType
		expected  EventSource
	}{
		{EventTypeOrderPlaced, SourceExchange},
		{EventTypeWebSocketDisconnected, SourceNetwork},
		{EventTypeAPIRateLimited, SourceAPI},
		{EventTypeSystemCPUHigh, SourceSystem},
		{EventTypeRiskTriggered, SourceRisk},
	}

	for _, tt := range tests {
		source := GetEventSource(tt.eventType)
		if source != tt.expected {
			t.Errorf("GetEventSource(%s) = %s, want %s", tt.eventType, source, tt.expected)
		}
	}

	t.Log("✅ 事件来源测試通過")
}

func TestEventTitle(t *testing.T) {
	tests := []struct {
		eventType EventType
		contains  string
	}{
		{EventTypeOrderPlaced, "订單"},
		{EventTypeWebSocketDisconnected, "WebSocket"},
		{EventTypeAPIRateLimited, "限流"},
		{EventTypePriceVolatility, "價格"},
	}

	for _, tt := range tests {
		title := GetEventTitle(tt.eventType)
		if title == "" {
			t.Errorf("GetEventTitle(%s) returned empty string", tt.eventType)
		}
		t.Logf("✅ %s: %s", tt.eventType, title)
	}
}

func TestEventCenterMessageBuildersAndNotifyRules(t *testing.T) {
	center := &EventCenter{}

	cases := []struct {
		name      string
		event     *Event
		contains  string
		notify    bool
		severity  EventSeverity
		eventType EventType
	}{
		{
			name: "order", event: &Event{Type: EventTypeOrderPlaced, Data: map[string]interface{}{
				"symbol": "BTCUSDT", "side": "BUY", "price": 100.0, "quantity": 0.5,
			}}, contains: "BTCUSDT BUY", severity: SeverityInfo, eventType: EventTypeOrderPlaced,
		},
		{
			name: "websocket with reason", event: &Event{Type: EventTypeWebSocketDisconnected, Data: map[string]interface{}{
				"exchange": "binance", "symbol": "BTCUSDT", "reason": "timeout",
			}}, contains: "timeout", notify: true, severity: SeverityCritical, eventType: EventTypeWebSocketDisconnected,
		},
		{
			name: "api endpoint", event: &Event{Type: EventTypeAPIRequestFailed, Data: map[string]interface{}{
				"exchange": "okx", "endpoint": "/orders", "error": "429",
			}}, contains: "/orders", notify: true, severity: SeverityWarning, eventType: EventTypeAPIRequestFailed,
		},
		{
			name: "price volatility", event: &Event{Type: EventTypePriceVolatility, Data: map[string]interface{}{
				"symbol": "ETHUSDT", "old_price": 100.0, "new_price": 110.0, "change_percent": 10.0,
			}}, contains: "10.00%", notify: true, severity: SeverityWarning, eventType: EventTypePriceVolatility,
		},
		{
			name: "system resource", event: &Event{Type: EventTypeSystemCPUHigh, Data: map[string]interface{}{
				"resource_type": "CPU", "usage": 95.0, "threshold": 90.0,
			}}, contains: "CPU", notify: true, severity: SeverityCritical, eventType: EventTypeSystemCPUHigh,
		},
		{
			name: "precision pause", event: &Event{Type: EventTypePrecisionAdjustment, Data: map[string]interface{}{
				"symbol": "BTCUSDT", "calculated_qty": 0.00001, "min_qty": 0.001, "action": "pause",
			}}, contains: "自动暂停", notify: true, severity: SeverityWarning, eventType: EventTypePrecisionAdjustment,
		},
		{
			name: "risk recovered", event: &Event{Type: EventTypeRiskRecovered, Data: map[string]interface{}{
				"symbol": "BTCUSDT", "price": 101.0,
			}}, contains: "風控解除", severity: SeverityWarning, eventType: EventTypeRiskRecovered,
		},
		{
			name: "default message", event: &Event{Type: EventType("custom"), Data: map[string]interface{}{
				"message": "hello event",
			}}, contains: "hello event", severity: SeverityInfo, eventType: EventType("custom"),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			message := center.buildMessage(tc.event)
			if !strings.Contains(message, tc.contains) {
				t.Fatalf("message = %q, want contains %q", message, tc.contains)
			}
			if got := center.shouldNotify(tc.eventType, tc.severity); got != tc.notify {
				t.Fatalf("shouldNotify() = %v, want %v", got, tc.notify)
			}
		})
	}

	if got := center.extractString(map[string]interface{}{"symbol": 123}, "symbol"); got != "" {
		t.Fatalf("extractString non-string = %q", got)
	}
	if !center.shouldNotify(EventTypeTradingStarted, SeverityInfo) {
		t.Fatal("trading started should notify")
	}
	if center.shouldNotify(EventTypeOrderPlaced, SeverityInfo) {
		t.Fatal("plain order info should not notify")
	}
}

func TestEventCenterCheckPriceVolatilityPublishesOnlyWhenThresholdCrossed(t *testing.T) {
	bus := NewEventBus(2)
	defer bus.Close()
	ch := bus.Subscribe()
	center := &EventCenter{
		eventBus:                 bus,
		priceVolatilityThreshold: 5,
		monitoredSymbols:         map[string]bool{"BTCUSDT": true},
	}

	center.CheckPriceVolatility("ETHUSDT", 100, 120)
	select {
	case event := <-ch:
		t.Fatalf("unmonitored symbol should not publish: %#v", event)
	default:
	}

	center.CheckPriceVolatility("BTCUSDT", 0, 120)
	center.CheckPriceVolatility("BTCUSDT", 100, 103)
	select {
	case event := <-ch:
		t.Fatalf("invalid or below-threshold prices should not publish: %#v", event)
	default:
	}

	center.CheckPriceVolatility("BTCUSDT", 100, 110)
	select {
	case event := <-ch:
		if event.Type != EventTypePriceVolatility || event.Data["symbol"] != "BTCUSDT" {
			t.Fatalf("unexpected event: %#v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("expected price volatility event")
	}
}

func TestEventBusSubscribeDeduplicateAndClose(t *testing.T) {
	bus := NewEventBus(1)
	ch := bus.Subscribe()
	if len(bus.subscribers) != 1 {
		t.Fatalf("subscriber count = %d", len(bus.subscribers))
	}

	bus.Publish(&Event{Type: EventTypeAPIRateLimited})
	select {
	case event := <-ch:
		if event.Type != EventTypeAPIRateLimited || event.Timestamp.IsZero() {
			t.Fatalf("unexpected event: %#v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("expected first rate limited event")
	}

	bus.Publish(&Event{Type: EventTypeAPIRateLimited})
	select {
	case event := <-ch:
		t.Fatalf("duplicate event should be suppressed: %#v", event)
	default:
	}

	bus.dedupMu.Lock()
	bus.dedupMap[EventTypeAPIRateLimited] = time.Now().Add(-bus.dedupWindow * 3)
	bus.dedupMu.Unlock()
	bus.cleanupDedupMap()
	if len(bus.dedupMap) != 0 {
		t.Fatalf("dedup map should be cleaned: %#v", bus.dedupMap)
	}

	bus.Unsubscribe(ch)
	if len(bus.subscribers) != 0 {
		t.Fatalf("subscriber count after unsubscribe = %d", len(bus.subscribers))
	}
	bus.Close()
	bus.Publish(&Event{Type: EventTypeOrderPlaced})
	bus.Close()
}
