package notify

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"quantmesh/config"
	"quantmesh/event"
)

// DingTalkNotifier 钉钉通知器
type DingTalkNotifier struct {
	webhook string
	secret  string
	client  *http.Client
}

// NewDingTalkNotifier 创建钉钉通知器
func NewDingTalkNotifier(cfg *config.Config) (*DingTalkNotifier, error) {
	if cfg.Notifications.DingTalk.Webhook == "" {
		return nil, fmt.Errorf("钉钉 Webhook URL 未配置")
	}

	return &DingTalkNotifier{
		webhook: cfg.Notifications.DingTalk.Webhook,
		secret:  cfg.Notifications.DingTalk.Secret,
		client: &http.Client{
			Timeout: 3 * time.Second,
		},
	}, nil
}

// Name 返回通知器名称
func (dn *DingTalkNotifier) Name() string {
	return "DingTalk"
}

// Send 发送通知
func (dn *DingTalkNotifier) Send(evt *event.Event) error {
	message := formatDingTalkMessage(evt)
	
	// 构建请求 URL（如果配置了签名密钥，需要添加签名参数）
	requestURL := dn.webhook
	if dn.secret != "" {
		timestamp := time.Now().UnixNano() / 1e6 // 毫秒时间戳
		sign := dn.generateSign(timestamp)
		requestURL = fmt.Sprintf("%s&timestamp=%d&sign=%s", dn.webhook, timestamp, sign)
	}

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

	req, err := http.NewRequestWithContext(ctx, "POST", requestURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("创建请求失败: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := dn.client.Do(req)
	if err != nil {
		return fmt.Errorf("发送请求失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("钉钉 API 返回错误: %d", resp.StatusCode)
	}

	return nil
}

// generateSign 生成钉钉签名
func (dn *DingTalkNotifier) generateSign(timestamp int64) string {
	stringToSign := fmt.Sprintf("%d\n%s", timestamp, dn.secret)
	h := hmac.New(sha256.New, []byte(dn.secret))
	h.Write([]byte(stringToSign))
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

// formatDingTalkMessage 格式化钉钉消息
func formatDingTalkMessage(evt *event.Event) string {
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
		// 根据模式选择不同的标题
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
