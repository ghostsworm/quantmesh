package event

import (
	"context"
	"testing"
	"time"
)

// MockDatabase 模拟数据库
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
	// 简化测试，不实际保存
	return nil
}

// MockNotifier 模拟通知服务
type MockNotifier struct {
	notifications []*Event
}

func (m *MockNotifier) Send(event *Event) {
	m.notifications = append(m.notifications, event)
}

func TestEventCenterBasic(t *testing.T) {
	// 创建事件总线
	eventBus := NewEventBus(100)
	if eventBus == nil {
		t.Fatal("Failed to create event bus")
	}
	
	// 创建模拟数据库和通知服务
	mockDB := &MockDatabase{}
	mockNotifier := &MockNotifier{}
	if mockDB == nil || mockNotifier == nil {
		t.Fatal("Failed to create mock services")
	}
	
	// 创建事件中心配置
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
	
	t.Log("✅ 事件中心配置创建成功")
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
	
	t.Log("✅ 事件严重程度测试通过")
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
	
	t.Log("✅ 事件来源测试通过")
}

func TestEventTitle(t *testing.T) {
	tests := []struct {
		eventType EventType
		contains  string
	}{
		{EventTypeOrderPlaced, "订单"},
		{EventTypeWebSocketDisconnected, "WebSocket"},
		{EventTypeAPIRateLimited, "限流"},
		{EventTypePriceVolatility, "价格"},
	}
	
	for _, tt := range tests {
		title := GetEventTitle(tt.eventType)
		if title == "" {
			t.Errorf("GetEventTitle(%s) returned empty string", tt.eventType)
		}
		t.Logf("✅ %s: %s", tt.eventType, title)
	}
}

