package notify

import (
	"strings"
	"testing"
	"time"

	"quantmesh/config"
	"quantmesh/event"
)

func TestSendDirectRejectsMissingDestination(t *testing.T) {
	cfg := &config.Config{}
	cfg.Notifications.Enabled = true
	cfg.Notifications.Email.Enabled = true
	cfg.Notifications.Email.Provider = "smtp"
	cfg.Notifications.Email.From = "bot@example.com"

	err := SendDirect(cfg, "email", " ", "主题", "正文")
	if err == nil || !strings.Contains(err.Error(), "邮箱") {
		t.Fatalf("空邮箱应被拒绝，实际: %v", err)
	}
	err = SendDirect(cfg, "webhook", "", "主题", "正文")
	if err == nil {
		t.Fatal("空 webhook 应被拒绝")
	}
	err = SendDirect(cfg, "pager", "x", "主题", "正文")
	if err == nil || !strings.Contains(err.Error(), "不支持") {
		t.Fatalf("未知渠道应被拒绝，实际: %v", err)
	}
}

func TestMessageFormattersUsePreparedBody(t *testing.T) {
	evt := &event.Event{
		Type:      event.EventTypeMCPWriteCompleted,
		Timestamp: time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC),
		Data:      map[string]interface{}{"message": "令牌：只读\n- qm_bot_set_enabled：ok"},
	}
	for name, text := range map[string]string{
		"email":       formatEmailMessage(evt),
		"telegram":    formatTelegramMessage(evt),
		"feishu":      formatFeishuMessage(evt),
		"dingtalk":    formatDingTalkMessage(evt),
		"wechat_work": formatWeChatWorkMessage(evt),
		"slack":       formatSlackMessage(evt),
	} {
		if !strings.Contains(text, "qm_bot_set_enabled") {
			t.Fatalf("%s 没有带上写操作正文: %s", name, text)
		}
		if strings.Contains(text, "subject") {
			t.Fatalf("%s 把内部字段展开了: %s", name, text)
		}
	}
}
