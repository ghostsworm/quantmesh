package notify

import (
	"sync"

	"opensqt/config"
	"opensqt/event"
	"opensqt/logger"
)

// Notifier 通知接口
type Notifier interface {
	Send(event *event.Event) error
	Name() string
}

// NotificationService 通知服务
type NotificationService struct {
	notifiers []Notifier
	cfg       *config.Config
}

// NewNotificationService 创建通知服务
func NewNotificationService(cfg *config.Config) *NotificationService {
	ns := &NotificationService{
		cfg: cfg,
	}

	// 初始化启用的通知渠道
	if cfg.Notifications.Enabled {
		if cfg.Notifications.Telegram.Enabled && cfg.Notifications.Telegram.BotToken != "" {
			telegramNotifier, err := NewTelegramNotifier(cfg)
			if err != nil {
				logger.Warn("⚠️ 初始化 Telegram 通知失败: %v", err)
			} else {
				ns.notifiers = append(ns.notifiers, telegramNotifier)
				logger.Info("✅ Telegram 通知已启用")
			}
		}

		if cfg.Notifications.Webhook.Enabled && cfg.Notifications.Webhook.URL != "" {
			webhookNotifier, err := NewWebhookNotifier(cfg)
			if err != nil {
				logger.Warn("⚠️ 初始化 Webhook 通知失败: %v", err)
			} else {
				ns.notifiers = append(ns.notifiers, webhookNotifier)
				logger.Info("✅ Webhook 通知已启用")
			}
		}

		if cfg.Notifications.Email.Enabled {
			emailNotifier, err := NewEmailNotifier(cfg)
			if err != nil {
				logger.Warn("⚠️ 初始化邮件通知失败: %v", err)
			} else {
				ns.notifiers = append(ns.notifiers, emailNotifier)
				logger.Info("✅ 邮件通知已启用 (Provider: %s)", cfg.Notifications.Email.Provider)
			}
		}
	}

	return ns
}

// shouldNotify 检查是否需要通知
func (ns *NotificationService) shouldNotify(eventType event.EventType) bool {
	if !ns.cfg.Notifications.Enabled {
		return false
	}

	rules := ns.cfg.Notifications.Rules
	switch eventType {
	case event.EventTypeOrderPlaced:
		return rules.OrderPlaced
	case event.EventTypeOrderFilled:
		return rules.OrderFilled
	case event.EventTypeRiskTriggered:
		return rules.RiskTriggered
	case event.EventTypeStopLoss:
		return rules.StopLoss
	case event.EventTypeError:
		return rules.Error
	default:
		// 其他事件默认通知
		return true
	}
}

// Send 发送通知（异步，不阻塞）
func (ns *NotificationService) Send(evt *event.Event) {
	if evt == nil {
		return
	}

	// 检查是否需要通知
	if !ns.shouldNotify(evt.Type) {
		return
	}

	// 异步发送，不阻塞
	go func() {
		// 并发发送到所有启用的通知渠道
		var wg sync.WaitGroup
		for _, notifier := range ns.notifiers {
			wg.Add(1)
			go func(n Notifier) {
				defer wg.Done()
				if err := n.Send(evt); err != nil {
					logger.Warn("⚠️ [%s] 通知发送失败: %v", n.Name(), err)
				}
			}(notifier)
		}
		wg.Wait()
	}()
}

