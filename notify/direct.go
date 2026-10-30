package notify

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"quantmesh/config"
	"quantmesh/event"
)

// directTimeout 是令牌通知这类不走全局规则的直发上限。
const directTimeout = 15 * time.Second

// SendDirect 按指定渠道发送一段纯文本，不经过全局通知开关和事件规则。
// target 为空时使用该渠道已经配置好的全局目的地；邮件和通用 Webhook 必须给出 target。
func SendDirect(cfg *config.Config, channel, target, subject, body string) error {
	if cfg == nil {
		return fmt.Errorf("通知配置未初始化")
	}
	channel = strings.ToLower(strings.TrimSpace(channel))
	target = strings.TrimSpace(target)
	if subject == "" {
		subject = "QuantMesh 通知"
	}
	evt := &event.Event{
		Type:      event.EventTypeMCPWriteCompleted,
		Timestamp: time.Now(),
		Data:      map[string]interface{}{"message": body, "subject": subject},
	}

	switch channel {
	case "email":
		return sendDirectEmail(cfg, target, subject, body)
	case "telegram":
		return sendDirectTelegram(cfg, target, evt)
	case "webhook":
		if target == "" {
			return fmt.Errorf("webhook 地址不能为空")
		}
		return (&WebhookNotifier{url: target, timeout: directTimeout, client: httpClient(directTimeout)}).Send(evt)
	case "feishu":
		return sendDirectWebhook(cfg.Notifications.Feishu.Enabled, cfg.Notifications.Feishu.Webhook, target, "飞书", func(url string) error {
			return (&FeishuNotifier{webhook: url, client: httpClient(directTimeout)}).Send(evt)
		})
	case "dingtalk":
		return sendDirectWebhook(cfg.Notifications.DingTalk.Enabled, cfg.Notifications.DingTalk.Webhook, target, "钉钉", func(url string) error {
			return (&DingTalkNotifier{webhook: url, secret: cfg.Notifications.DingTalk.Secret, client: httpClient(directTimeout)}).Send(evt)
		})
	case "wechat_work":
		return sendDirectWebhook(cfg.Notifications.WeChatWork.Enabled, cfg.Notifications.WeChatWork.Webhook, target, "企业微信", func(url string) error {
			return (&WeChatWorkNotifier{webhook: url, client: httpClient(directTimeout)}).Send(evt)
		})
	case "slack":
		return sendDirectWebhook(cfg.Notifications.Slack.Enabled, cfg.Notifications.Slack.Webhook, target, "Slack", func(url string) error {
			return (&SlackNotifier{webhook: url, client: httpClient(directTimeout)}).Send(evt)
		})
	default:
		return fmt.Errorf("不支持的通知渠道: %s", channel)
	}
}

func sendDirectEmail(cfg *config.Config, to, subject, body string) error {
	email := cfg.Notifications.Email
	if to == "" {
		return fmt.Errorf("通知邮箱不能为空")
	}
	if !email.Enabled {
		return fmt.Errorf("邮件通知未启用")
	}
	if email.From == "" {
		return fmt.Errorf("邮件发件人未配置")
	}
	switch email.Provider {
	case "smtp":
		if email.SMTP.Host == "" {
			return fmt.Errorf("SMTP Host 未配置")
		}
		return NewSMTPProvider(cfg).Send(email.From, to, subject, body)
	case "resend":
		if email.Resend.APIKey == "" {
			return fmt.Errorf("Resend APIKey 未配置")
		}
		return NewResendProvider(cfg).Send(email.From, to, subject, body)
	case "mailgun":
		if email.Mailgun.APIKey == "" || email.Mailgun.Domain == "" {
			return fmt.Errorf("Mailgun APIKey 或 Domain 未配置")
		}
		return NewMailgunProvider(cfg).Send(email.From, to, subject, body)
	default:
		return fmt.Errorf("不支援的邮件服務商: %s", email.Provider)
	}
}

func sendDirectTelegram(cfg *config.Config, chatID string, evt *event.Event) error {
	tg := cfg.Notifications.Telegram
	if !tg.Enabled || tg.BotToken == "" {
		return fmt.Errorf("Telegram 通知未启用")
	}
	if chatID == "" {
		chatID = tg.ChatID
	}
	if chatID == "" {
		return fmt.Errorf("Telegram Chat ID 未配置")
	}
	notifier, err := NewTelegramNotifier(cfg)
	if err != nil {
		return err
	}
	notifier.chatID = chatID
	return notifier.Send(evt)
}

func sendDirectWebhook(enabled bool, configured, override, name string, send func(string) error) error {
	url := strings.TrimSpace(override)
	if url == "" {
		if !enabled {
			return fmt.Errorf("%s 通知未启用", name)
		}
		url = strings.TrimSpace(configured)
	}
	if url == "" {
		return fmt.Errorf("%s Webhook 未配置", name)
	}
	return send(url)
}

func httpClient(timeout time.Duration) *http.Client {
	return &http.Client{Timeout: timeout}
}
