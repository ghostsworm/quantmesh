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

// telegramRequestTimeout 調用 Telegram Bot API 的總超時（含 TLS、連線與讀取響應）。
// 過短會在跨網或 api.telegram.org 較慢時出現「消息已發出但接口報 context deadline exceeded」的假失敗。
const telegramRequestTimeout = 30 * time.Second

// TelegramNotifier Telegram 通知器
type TelegramNotifier struct {
	botToken string
	chatID   string
	client   *http.Client
}

// NewTelegramNotifier 創建 Telegram 通知器
func NewTelegramNotifier(cfg *config.Config) (*TelegramNotifier, error) {
	if cfg.Notifications.Telegram.BotToken == "" || cfg.Notifications.Telegram.ChatID == "" {
		return nil, fmt.Errorf("Telegram BotToken 或 ChatID 未配置")
	}

	return &TelegramNotifier{
		botToken: cfg.Notifications.Telegram.BotToken,
		chatID:   cfg.Notifications.Telegram.ChatID,
		client: &http.Client{
			Timeout: telegramRequestTimeout,
		},
	}, nil
}

// Name 返回通知器名称
func (tn *TelegramNotifier) Name() string {
	return "Telegram"
}

// Send 发送通知
func (tn *TelegramNotifier) Send(evt *event.Event) error {
	message := formatTelegramMessage(evt)
	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", tn.botToken)

	payload := map[string]interface{}{
		"chat_id":    tn.chatID,
		"text":       message,
		"parse_mode": "Markdown",
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("序列化消息失败: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), telegramRequestTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("創建请求失败: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := tn.client.Do(req)
	if err != nil {
		return fmt.Errorf("发送请求失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Telegram API 返回錯误: %d", resp.StatusCode)
	}

	return nil
}

// formatTelegramMessage 格式化 Telegram 消息
func formatTelegramMessage(evt *event.Event) string {
	var emoji string
	var title string

	switch evt.Type {
	case event.EventTypeOrderPlaced:
		emoji = "📝"
		title = "订單已下單"
	case event.EventTypeOrderFilled:
		emoji = "✅"
		title = "订單已成交"
	case event.EventTypeOrderCanceled:
		emoji = "❌"
		title = "订單已取消"
	case event.EventTypeRiskTriggered:
		emoji = "🚨"
		title = "风控触发"
	case event.EventTypeRiskRecovered:
		emoji = "✅"
		title = "风控解除"
	case event.EventTypeStopLoss:
		emoji = "🛑"
		title = "止损触发"
	case event.EventTypeTakeProfit:
		emoji = "💰"
		title = "止盈触发"
	case event.EventTypeError:
		emoji = "❌"
		title = "系统錯误"
	case event.EventTypeSystemStart:
		emoji = "🚀"
		title = "系统啟动"
	case event.EventTypeSystemStop:
		emoji = "🛑"
		title = "系统停止"
	case event.EventTypeMarginInsufficient:
		emoji = "⚠️"
		title = "保证金不足"
	case event.EventTypeAllocationExceeded:
		emoji = "🚫"
		title = "超出资金分配限制"
	case event.EventTypeAllocationLimitChanged:
		if mode, ok := evt.Data["mode"].(string); ok {
			if mode == "emergency" {
				emoji = "🚨"
				title = "资金限額已提升（紧急模式）"
			} else {
				emoji = "✅"
				title = "资金限額已恢複（正常模式）"
			}
		} else {
			emoji = "📊"
			title = "资金限額变更"
		}
	case event.EventTypeInspectorReport:
		emoji = "📊"
		if t, ok := evt.Data["title"].(string); ok && t != "" {
			title = t
		} else {
			title = "智子巡檢報告"
		}
	default:
		emoji = "ℹ️"
		title = "系统通知"
	}

	message := fmt.Sprintf("%s *%s*\n", emoji, title)
	message += fmt.Sprintf("時间: %s\n", evt.Timestamp.Format("2006-01-02 15:04:05"))

	// 智子巡檢報告：直接使用 body 作為正文
	if evt.Type == event.EventTypeInspectorReport && evt.Data != nil {
		if body, ok := evt.Data["body"].(string); ok && body != "" {
			message += "\n" + body
			return message
		}
	}
	// 添加事件數據
	if evt.Data != nil {
		for key, value := range evt.Data {
			message += fmt.Sprintf("%s: `%v`\n", key, value)
		}
	}

	return message
}
