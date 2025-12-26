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

// WebhookNotifier Webhook 通知器
type WebhookNotifier struct {
	url     string
	timeout time.Duration
	client  *http.Client
}

// NewWebhookNotifier 创建 Webhook 通知器
func NewWebhookNotifier(cfg *config.Config) (*WebhookNotifier, error) {
	if cfg.Notifications.Webhook.URL == "" {
		return nil, fmt.Errorf("Webhook URL 未配置")
	}

	timeout := time.Duration(cfg.Notifications.Webhook.Timeout) * time.Second
	if timeout <= 0 {
		timeout = 3 * time.Second
	}

	return &WebhookNotifier{
		url:     cfg.Notifications.Webhook.URL,
		timeout: timeout,
		client: &http.Client{
			Timeout: timeout,
		},
	}, nil
}

// Name 返回通知器名称
func (wn *WebhookNotifier) Name() string {
	return "Webhook"
}

// Send 发送通知
func (wn *WebhookNotifier) Send(evt *event.Event) error {
	payload := map[string]interface{}{
		"type":      string(evt.Type),
		"timestamp": evt.Timestamp.Format(time.RFC3339),
		"data":      evt.Data,
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("序列化消息失败: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), wn.timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "POST", wn.url, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("创建请求失败: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := wn.client.Do(req)
	if err != nil {
		return fmt.Errorf("发送请求失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("Webhook 返回错误状态码: %d", resp.StatusCode)
	}

	return nil
}

