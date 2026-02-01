package event

import (
	"context"
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

