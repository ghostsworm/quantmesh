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

// WeChatWorkNotifier 企业微信通知器
type WeChatWorkNotifier struct {
	webhook string
	client  *http.Client
}

// NewWeChatWorkNotifier 创建企业微信通知器
func NewWeChatWorkNotifier(cfg *config.Config) (*WeChatWorkNotifier, error) {
	if cfg.Notifications.WeChatWork.Webhook == "" {
		return nil, fmt.Errorf("企业微信 Webhook URL 未配置")
	}

	return &WeChatWorkNotifier{
		webhook: cfg.Notifications.WeChatWork.Webhook,
		client: &http.Client{
			Timeout: 3 * time.Second,
		},
	}, nil
}

// Name 返回通知器名称
func (wn *WeChatWorkNotifier) Name() string {
	return "WeChat Work"
}

// Send 发送通知
func (wn *WeChatWorkNotifier) Send(evt *event.Event) error {
	message := formatWeChatWorkMessage(evt)
	
	payload := map[string]interface{}{
		"msgtype": "text",
		"text": map[string]string{
			"content": message,
		},
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("序列化消息失败: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "POST", wn.webhook, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("创建请求失败: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := wn.client.Do(req)
	if err != nil {
		return fmt.Errorf("发送请求失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("企业微信 API 返回错误: %d", resp.StatusCode)
	}

	return nil
}

// formatWeChatWorkMessage 格式化企业微信消息
func formatWeChatWorkMessage(evt *event.Event) string {
	var title string
	switch evt.Type {
	case event.EventTypeOrderPlaced:
		title = "📝 订单已下单"
	case event.EventTypeOrderFilled:
		title = "✅ 订单已成交"
	case event.EventTypeOrderCanceled:
		title = "❌ 订单已取消"
	case event.EventTypeRiskTriggered:
		title = "⚠️ 风控触发"
	case event.EventTypeRiskRecovered:
		title = "✅ 风控解除"
	case event.EventTypeStopLoss:
		title = "🛑 止损触发"
	case event.EventTypeTakeProfit:
		title = "💰 止盈触发"
	case event.EventTypeError:
		title = "❌ 系统错误"
	case event.EventTypeSystemStart:
		title = "🚀 系统启动"
	case event.EventTypeSystemStop:
		title = "🛑 系统停止"
	case event.EventTypeMarginInsufficient:
		title = "⚠️ 保证金不足告警"
	case event.EventTypeAllocationExceeded:
		title = "⚠️ 超出资金分配限制"
	default:
		title = "📢 系统通知"
	}

	message := fmt.Sprintf("%s\n\n时间: %s\n\n", title, evt.Timestamp.Format("2006-01-02 15:04:05"))

	if evt.Data != nil {
		message += "详细信息:\n"
		for key, value := range evt.Data {
			message += fmt.Sprintf("  %s: %v\n", key, value)
		}
	}

	return message
}
