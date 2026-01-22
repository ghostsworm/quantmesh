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

// FeishuNotifier 飞书通知器
type FeishuNotifier struct {
	webhook string
	client  *http.Client
}

// NewFeishuNotifier 创建飞书通知器
func NewFeishuNotifier(cfg *config.Config) (*FeishuNotifier, error) {
	if cfg.Notifications.Feishu.Webhook == "" {
		return nil, fmt.Errorf("飞书 Webhook URL 未配置")
	}

	return &FeishuNotifier{
		webhook: cfg.Notifications.Feishu.Webhook,
		client: &http.Client{
			Timeout: 3 * time.Second,
		},
	}, nil
}

// Name 返回通知器名称
func (fn *FeishuNotifier) Name() string {
	return "Feishu"
}

// Send 发送通知
func (fn *FeishuNotifier) Send(evt *event.Event) error {
	message := formatFeishuMessage(evt)
	
	payload := map[string]interface{}{
		"msg_type": "text",
		"content": map[string]string{
			"text": message,
		},
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("序列化消息失败: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "POST", fn.webhook, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("创建请求失败: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := fn.client.Do(req)
	if err != nil {
		return fmt.Errorf("发送请求失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("飞书 API 返回错误: %d", resp.StatusCode)
	}

	return nil
}

// formatFeishuMessage 格式化飞书消息
func formatFeishuMessage(evt *event.Event) string {
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
	case event.EventTypeAllocationLimitChanged:
		if mode, ok := evt.Data["mode"].(string); ok {
			if mode == "emergency" {
				title = "🚨 资金限额已提升（紧急模式）"
			} else {
				title = "✅ 资金限额已恢复（正常模式）"
			}
		} else {
			title = "📊 资金限额变更"
		}
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
