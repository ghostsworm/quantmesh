package notify

import (
	"strings"
	"testing"
	"time"

	"quantmesh/config"
	"quantmesh/event"
)

func testNotifyEvent(eventType event.EventType, data map[string]interface{}) *event.Event {
	return &event.Event{
		Type:      eventType,
		Timestamp: time.Date(2026, 6, 4, 9, 30, 0, 0, time.UTC),
		Data:      data,
	}
}

func TestNotifierConstructorsValidateRequiredConfig(t *testing.T) {
	cfg := &config.Config{}

	if _, err := NewDingTalkNotifier(cfg); err == nil {
		t.Fatalf("empty DingTalk webhook should fail")
	}
	cfg.Notifications.DingTalk.Webhook = "http://example.invalid/dingtalk"
	dingTalk, err := NewDingTalkNotifier(cfg)
	if err != nil {
		t.Fatalf("configured DingTalk notifier failed: %v", err)
	}
	if dingTalk.Name() != "DingTalk" {
		t.Fatalf("DingTalk name = %q", dingTalk.Name())
	}
	if dingTalk.generateSign(12345) == "" {
		t.Fatalf("DingTalk sign should not be empty")
	}

	cfg = &config.Config{}
	if _, err := NewSlackNotifier(cfg); err == nil {
		t.Fatalf("empty Slack webhook should fail")
	}
	cfg.Notifications.Slack.Webhook = "http://example.invalid/slack"
	slack, err := NewSlackNotifier(cfg)
	if err != nil {
		t.Fatalf("configured Slack notifier failed: %v", err)
	}
	if slack.Name() != "Slack" {
		t.Fatalf("Slack name = %q", slack.Name())
	}

	cfg = &config.Config{}
	if _, err := NewFeishuNotifier(cfg); err == nil {
		t.Fatalf("empty Feishu webhook should fail")
	}
	cfg.Notifications.Feishu.Webhook = "http://example.invalid/feishu"
	feishu, err := NewFeishuNotifier(cfg)
	if err != nil {
		t.Fatalf("configured Feishu notifier failed: %v", err)
	}
	if feishu.Name() != "Feishu" {
		t.Fatalf("Feishu name = %q", feishu.Name())
	}

	cfg = &config.Config{}
	if _, err := NewWeChatWorkNotifier(cfg); err == nil {
		t.Fatalf("empty WeChat Work webhook should fail")
	}
	cfg.Notifications.WeChatWork.Webhook = "http://example.invalid/wechat"
	wechat, err := NewWeChatWorkNotifier(cfg)
	if err != nil {
		t.Fatalf("configured WeChat Work notifier failed: %v", err)
	}
	if wechat.Name() != "WeChat Work" {
		t.Fatalf("WeChat Work name = %q", wechat.Name())
	}
}

func TestEmailNotifierValidationAndFormatting(t *testing.T) {
	cfg := &config.Config{}
	if _, err := NewEmailNotifier(cfg); err == nil {
		t.Fatalf("disabled email notifier should fail")
	}

	cfg.Notifications.Email.Enabled = true
	if _, err := NewEmailNotifier(cfg); err == nil {
		t.Fatalf("email notifier without addresses should fail")
	}

	cfg.Notifications.Email.From = "from@example.com"
	cfg.Notifications.Email.To = "to@example.com"
	cfg.Notifications.Email.Provider = "smtp"
	if _, err := NewEmailNotifier(cfg); err == nil {
		t.Fatalf("smtp email notifier without host should fail")
	}

	cfg.Notifications.Email.SMTP.Host = "smtp.example.com"
	email, err := NewEmailNotifier(cfg)
	if err != nil {
		t.Fatalf("configured smtp email notifier failed: %v", err)
	}
	if email.Name() != "Email (smtp)" {
		t.Fatalf("email name = %q", email.Name())
	}

	cfg.Notifications.Email.Provider = "resend"
	if _, err := NewEmailNotifier(cfg); err == nil {
		t.Fatalf("resend email notifier without api key should fail")
	}
	cfg.Notifications.Email.Resend.APIKey = "test-key"
	if _, err := NewEmailNotifier(cfg); err != nil {
		t.Fatalf("configured resend email notifier failed: %v", err)
	}

	cfg.Notifications.Email.Provider = "mailgun"
	if _, err := NewEmailNotifier(cfg); err == nil {
		t.Fatalf("mailgun email notifier without domain should fail")
	}
	cfg.Notifications.Email.Mailgun.APIKey = "test-key"
	cfg.Notifications.Email.Mailgun.Domain = "mg.example.com"
	if _, err := NewEmailNotifier(cfg); err != nil {
		t.Fatalf("configured mailgun email notifier failed: %v", err)
	}

	cfg.Notifications.Email.Provider = "unknown"
	if _, err := NewEmailNotifier(cfg); err == nil {
		t.Fatalf("unknown email provider should fail")
	}

	msg := formatEmailMessage(testNotifyEvent(event.EventTypeInspectorReport, map[string]interface{}{
		"title": "巡检摘要",
		"body":  "账户风险正常",
	}))
	if !strings.Contains(msg, "巡检摘要") || !strings.Contains(msg, "账户风险正常") || strings.Contains(msg, "详细信息") {
		t.Fatalf("unexpected inspector email message: %q", msg)
	}
}

func TestWebhookMessageFormattersCoverAllocationModesAndDefaults(t *testing.T) {
	cases := []struct {
		name   string
		format func(*event.Event) string
	}{
		{name: "dingtalk", format: formatDingTalkMessage},
		{name: "feishu", format: formatFeishuMessage},
		{name: "wechat", format: formatWeChatWorkMessage},
		{name: "slack", format: formatSlackMessage},
	}

	for _, tc := range cases {
		t.Run(tc.name+"_emergency", func(t *testing.T) {
			msg := tc.format(testNotifyEvent(event.EventTypeAllocationLimitChanged, map[string]interface{}{"mode": "emergency"}))
			if !strings.Contains(msg, "2026-06-04 09:30:00") {
				t.Fatalf("formatted message missing timestamp: %q", msg)
			}
			if !strings.Contains(msg, "emergency") && !strings.Contains(msg, "紧急模式") {
				t.Fatalf("formatted message missing emergency mode: %q", msg)
			}
		})

		t.Run(tc.name+"_normal", func(t *testing.T) {
			msg := tc.format(testNotifyEvent(event.EventTypeAllocationLimitChanged, map[string]interface{}{"mode": "normal"}))
			if !strings.Contains(msg, "normal") && !strings.Contains(msg, "正常模式") {
				t.Fatalf("formatted message missing normal mode: %q", msg)
			}
		})

		t.Run(tc.name+"_default", func(t *testing.T) {
			msg := tc.format(testNotifyEvent(event.EventType("custom"), map[string]interface{}{"symbol": "BTCUSDT"}))
			if !strings.Contains(msg, "BTCUSDT") {
				t.Fatalf("formatted message missing details: %q", msg)
			}
		})
	}
}
