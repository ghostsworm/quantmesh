package monitor

import (
	"sync"
	"testing"
	"time"

	"quantmesh/event"
)

type recordingAlertSender struct {
	mu     sync.Mutex
	events []*event.Event
}

func (r *recordingAlertSender) Send(evt *event.Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, evt)
}

func TestWatchdogRoutesAlertsToNotifier(t *testing.T) {
	cfg := testWatchdogConfig()
	cfg.Watchdog.Notifications.RateThreshold.Enabled = false
	w := NewWatchdog(cfg, nil, nil, nil)
	if w.notifier != nil {
		t.Fatal("nil NotificationService 不應被裝成非 nil 介面")
	}
	sender := &recordingAlertSender{}
	w.notifier = sender

	if err := w.checkThresholds(&SystemMetrics{Timestamp: time.Now(), CPUPercent: 95, MemoryMB: 900}); err != nil {
		t.Fatal(err)
	}

	sender.mu.Lock()
	defer sender.mu.Unlock()
	if len(sender.events) != 2 {
		t.Fatalf("CPU 與記憶體超閾值應各發一條告警, got %d", len(sender.events))
	}
	if sender.events[0].Type != event.EventTypeSystemCPUHigh || sender.events[1].Type != event.EventTypeSystemMemoryHigh {
		t.Fatalf("告警類型錯誤: %s, %s", sender.events[0].Type, sender.events[1].Type)
	}
	if sender.events[0].Data["source"] != "watchdog" {
		t.Fatalf("告警應帶來源, got %v", sender.events[0].Data)
	}
}

func TestWatchdogSendNotificationToleratesNilMetrics(t *testing.T) {
	cfg := testWatchdogConfig()
	cfg.Watchdog.Notifications.FixedThreshold.Enabled = false
	w := NewWatchdog(cfg, nil, nil, nil)
	sender := &recordingAlertSender{}
	w.notifier = sender

	// metrics 為 nil 時不應解引用
	w.sendNotification("rate_threshold", watchdogMetricCPU, nil, "test")
	sender.mu.Lock()
	defer sender.mu.Unlock()
	if len(sender.events) != 1 {
		t.Fatalf("應發送一條告警, got %d", len(sender.events))
	}
}
