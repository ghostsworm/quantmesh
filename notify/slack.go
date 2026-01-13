package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"quantmesh/config"
	"quantmesh/event"
)

// SlackNotifier Slack 通知器
type SlackNotifier struct {
	webhook string
	client  *http.Client
}

// NewSlackNotifier 创建 Slack 通知器
func NewSlackNotifier(cfg *config.Config) (*SlackNotifier, error) {
	if cfg.Notifications.Slack.Webhook == "" {
		return nil, fmt.Errorf("Slack Webhook URL 未配置")
	}

	return &SlackNotifier{
		webhook: cfg.Notifications.Slack.Webhook,
		client: &http.Client{
			Timeout: 3 * time.Second,
		},
	}, nil
}

// Name 返回通知器名称
func (sn *SlackNotifier) Name() string {
	return "Slack"
}

// Send 发送通知
func (sn *SlackNotifier) Send(evt *event.Event) error {
	message := formatSlackMessage(evt)
	
	payload := map[string]interface{}{
		"text": message,
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("序列化消息失败: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "POST", sn.webhook, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("创建请求失败: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := sn.client.Do(req)
	if err != nil {
		return fmt.Errorf("发送请求失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Slack API 返回错误: %d", resp.StatusCode)
	}

	return nil
}

// formatSlackMessage 格式化 Slack 消息
func formatSlackMessage(evt *event.Event) string {
	var title string
	var emoji string
	switch evt.Type {
	case event.EventTypeOrderPlaced:
		title = "Order Placed"
		emoji = ":memo:"
	case event.EventTypeOrderFilled:
		title = "Order Filled"
		emoji = ":white_check_mark:"
	case event.EventTypeOrderCanceled:
		title = "Order Canceled"
		emoji = ":x:"
	case event.EventTypeRiskTriggered:
		title = "Risk Triggered"
		emoji = ":warning:"
	case event.EventTypeRiskRecovered:
		title = "Risk Recovered"
		emoji = ":white_check_mark:"
	case event.EventTypeStopLoss:
		title = "Stop Loss Triggered"
		emoji = ":stop_sign:"
	case event.EventTypeTakeProfit:
		title = "Take Profit Triggered"
		emoji = ":moneybag:"
	case event.EventTypeError:
		title = "System Error"
		emoji = ":x:"
	case event.EventTypeSystemStart:
		title = "System Started"
		emoji = ":rocket:"
	case event.EventTypeSystemStop:
		title = "System Stopped"
		emoji = ":stop_sign:"
	case event.EventTypeMarginInsufficient:
		title = "Margin Insufficient Warning"
		emoji = ":warning:"
	case event.EventTypeAllocationExceeded:
		title = "Allocation Exceeded"
		emoji = ":warning:"
	default:
		title = "System Notification"
		emoji = ":bell:"
	}

	message := fmt.Sprintf("%s *%s*\n\n*Time:* %s\n\n", emoji, title, evt.Timestamp.Format("2006-01-02 15:04:05"))

	if evt.Data != nil {
		message += "*Details:*\n"
		for key, value := range evt.Data {
			message += fmt.Sprintf("  • %s: %v\n", key, value)
		}
	}

	return message
}
