package notify

import (
	"strings"
	"testing"
	"time"

	"quantmesh/event"
)

func TestFormatTelegramMessage_OrderFilled(t *testing.T) {
	ts := time.Date(2026, 3, 26, 12, 0, 0, 0, time.UTC)
	evt := &event.Event{
		Type:      event.EventTypeOrderFilled,
		Timestamp: ts,
		Data:      map[string]interface{}{"symbol": "BTCUSDT"},
	}
	got := formatTelegramMessage(evt)
	if !strings.Contains(got, "成交") {
		t.Fatalf("expected title fragment, got %q", got)
	}
	if !strings.Contains(got, "2026-03-26 12:00:00") {
		t.Fatalf("expected timestamp line, got %q", got)
	}
	if !strings.Contains(got, "BTCUSDT") {
		t.Fatalf("expected data field, got %q", got)
	}
}

func TestFormatTelegramMessage_InspectorReportWithBody(t *testing.T) {
	ts := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	evt := &event.Event{
		Type:      event.EventTypeInspectorReport,
		Timestamp: ts,
		Data: map[string]interface{}{
			"title": "自訂標題",
			"body":  "報告正文第一行",
		},
	}
	got := formatTelegramMessage(evt)
	if !strings.Contains(got, "自訂標題") {
		t.Fatalf("expected custom title, got %q", got)
	}
	if !strings.Contains(got, "報告正文第一行") {
		t.Fatalf("expected body content, got %q", got)
	}
}
