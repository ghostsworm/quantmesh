package notify

import (
	"context"
	"sync"

	"quantmesh/cfgmgr"
	"quantmesh/config"
	"quantmesh/event"
	"quantmesh/logger"
	"quantmesh/storage"
)

// Notifier 通知接口
type Notifier interface {
	Send(event *event.Event) error
	Name() string
}

// NotificationService 通知服務
type NotificationService struct {
	notifiers []Notifier
	cfg       *config.Config
	configMgr *cfgmgr.ConfigManager
	ctx       context.Context
	mu        sync.RWMutex
}

// NewNotificationService 創建通知服務
func NewNotificationService(cfg *config.Config, configMgr *cfgmgr.ConfigManager) *NotificationService {
	ctx := context.Background()
	ns := &NotificationService{
		cfg:       cfg,
		configMgr: configMgr,
		ctx:       ctx,
	}

	// 初始化啟用的通知渠道
	if cfg.Notifications.Enabled {
		if cfg.Notifications.Telegram.Enabled && cfg.Notifications.Telegram.BotToken != "" {
			telegramNotifier, err := NewTelegramNotifier(cfg)
			if err != nil {
				logger.Warn("⚠️ 初始化 Telegram 通知失败: %v", err)
			} else {
				ns.notifiers = append(ns.notifiers, telegramNotifier)
				logger.Info("✅ Telegram 通知已啟用")
			}
		}

		if cfg.Notifications.Webhook.Enabled && cfg.Notifications.Webhook.URL != "" {
			webhookNotifier, err := NewWebhookNotifier(cfg)
			if err != nil {
				logger.Warn("⚠️ 初始化 Webhook 通知失败: %v", err)
			} else {
				ns.notifiers = append(ns.notifiers, webhookNotifier)
				logger.Info("✅ Webhook 通知已啟用")
			}
		}

		if cfg.Notifications.Email.Enabled {
			emailNotifier, err := NewEmailNotifier(cfg)
			if err != nil {
				logger.Warn("⚠️ 初始化邮件通知失败: %v", err)
			} else {
				ns.notifiers = append(ns.notifiers, emailNotifier)
				logger.Info("✅ 邮件通知已啟用 (Provider: %s)", cfg.Notifications.Email.Provider)
			}
		}

		if cfg.Notifications.Feishu.Enabled && cfg.Notifications.Feishu.Webhook != "" {
			feishuNotifier, err := NewFeishuNotifier(cfg)
			if err != nil {
				logger.Warn("⚠️ 初始化飞书通知失败: %v", err)
			} else {
				ns.notifiers = append(ns.notifiers, feishuNotifier)
				logger.Info("✅ 飞书通知已啟用")
			}
		}

		if cfg.Notifications.DingTalk.Enabled && cfg.Notifications.DingTalk.Webhook != "" {
			dingTalkNotifier, err := NewDingTalkNotifier(cfg)
			if err != nil {
				logger.Warn("⚠️ 初始化钉钉通知失败: %v", err)
			} else {
				ns.notifiers = append(ns.notifiers, dingTalkNotifier)
				logger.Info("✅ 钉钉通知已啟用")
			}
		}

		if cfg.Notifications.WeChatWork.Enabled && cfg.Notifications.WeChatWork.Webhook != "" {
			weChatWorkNotifier, err := NewWeChatWorkNotifier(cfg)
			if err != nil {
				logger.Warn("⚠️ 初始化企业微信通知失败: %v", err)
			} else {
				ns.notifiers = append(ns.notifiers, weChatWorkNotifier)
				logger.Info("✅ 企业微信通知已啟用")
			}
		}

		if cfg.Notifications.Slack.Enabled && cfg.Notifications.Slack.Webhook != "" {
			slackNotifier, err := NewSlackNotifier(cfg)
			if err != nil {
				logger.Warn("⚠️ 初始化 Slack 通知失败: %v", err)
			} else {
				ns.notifiers = append(ns.notifiers, slackNotifier)
				logger.Info("✅ Slack 通知已啟用")
			}
		}
	}

	return ns
}

// shouldNotify 检查是否需要通知
func (ns *NotificationService) shouldNotify(eventType event.EventType) bool {
	// 从配置管理器获取通知开关
	enabled := true
	if ns.configMgr != nil {
		enabled = ns.configMgr.GetBool(storage.ScopeGlobal, "", "notifications.enabled")
	}

	if !enabled {
		return false
	}

	// 从配置管理器获取通知规则
	var ruleEnabled bool
	if ns.configMgr != nil {
		switch eventType {
		case event.EventTypeOrderPlaced:
			ruleEnabled = ns.configMgr.GetBool(storage.ScopeGlobal, "", "notifications.rules.order_placed")
		case event.EventTypeOrderFilled:
			ruleEnabled = ns.configMgr.GetBool(storage.ScopeGlobal, "", "notifications.rules.order_filled")
		case event.EventTypeRiskTriggered:
			ruleEnabled = ns.configMgr.GetBool(storage.ScopeGlobal, "", "notifications.rules.risk_triggered")
		case event.EventTypeStopLoss:
			ruleEnabled = ns.configMgr.GetBool(storage.ScopeGlobal, "", "notifications.rules.stop_loss")
		case event.EventTypeError:
			ruleEnabled = ns.configMgr.GetBool(storage.ScopeGlobal, "", "notifications.rules.error")
		case event.EventTypeMarginInsufficient:
			ruleEnabled = ns.configMgr.GetBool(storage.ScopeGlobal, "", "notifications.rules.margin_insufficient")
		case event.EventTypeAllocationExceeded:
			ruleEnabled = ns.configMgr.GetBool(storage.ScopeGlobal, "", "notifications.rules.allocation_exceeded")
		case event.EventTypeAllocationLimitChanged:
			ruleEnabled = ns.configMgr.GetBool(storage.ScopeGlobal, "", "notifications.rules.allocation_exceeded")
		case event.EventTypePrecisionAdjustment:
			return true // 精度异常始终通知
		case event.EventTypeInspectorReport:
			ruleEnabled = ns.configMgr.GetBool(storage.ScopeGlobal, "", "notifications.rules.inspector_report")
		case event.EventTypeTradingStarted, event.EventTypeTradingStopped:
			return true // Bot 启动/停止始终通知
		case event.EventTypeTradingStartFailed, event.EventTypeTradingStopFailed:
			return true // Bot 启动/停止失败始终通知
		default:
			// 其他事件默认通知
			return true
		}
	} else {
		// 如果没有配置管理器，使用配置文件的值
		rules := ns.cfg.Notifications.Rules
		switch eventType {
		case event.EventTypeOrderPlaced:
			ruleEnabled = rules.OrderPlaced
		case event.EventTypeOrderFilled:
			ruleEnabled = rules.OrderFilled
		case event.EventTypeRiskTriggered:
			ruleEnabled = rules.RiskTriggered
		case event.EventTypeStopLoss:
			ruleEnabled = rules.StopLoss
		case event.EventTypeError:
			ruleEnabled = rules.Error
		case event.EventTypeMarginInsufficient:
			ruleEnabled = rules.MarginInsufficient
		case event.EventTypeAllocationExceeded:
			ruleEnabled = rules.AllocationExceeded
		case event.EventTypeAllocationLimitChanged:
			ruleEnabled = rules.AllocationExceeded
		case event.EventTypePrecisionAdjustment:
			return true
		case event.EventTypeInspectorReport:
			ruleEnabled = rules.InspectorReport
		case event.EventTypeTradingStarted, event.EventTypeTradingStopped:
			return true
		case event.EventTypeTradingStartFailed, event.EventTypeTradingStopFailed:
			return true
		default:
			return true
		}
	}

	return ruleEnabled
}

// GetNotifiers 获取所有已初始化的通知器
func (ns *NotificationService) GetNotifiers() []Notifier {
	return ns.notifiers
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
		// 並发发送到所有啟用的通知渠道
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
