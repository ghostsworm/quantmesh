package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"time"

	"quantmesh/logger"
)

// ClaudeProvider Anthropic Claude AI Provider实现
type ClaudeProvider struct {
	apiKey  string
	model   string
	baseURL string
	client  *http.Client
}

// NewClaudeProvider 创建Claude Provider
func NewClaudeProvider(apiKey, model, baseURL string) *ClaudeProvider {
	if model == "" {
		model = "claude-3-opus-20240229" // 默认模型
	}
	if baseURL == "" {
		baseURL = "https://api.anthropic.com/v1"
	}

	// 创建支持代理的HTTP客户端
	transport := &http.Transport{}
	proxyURL := os.Getenv("HTTPS_PROXY")
	if proxyURL == "" {
		proxyURL = os.Getenv("https_proxy")
	}
	if proxyURL == "" {
		proxyURL = os.Getenv("HTTP_PROXY")
	}
	if proxyURL == "" {
		proxyURL = os.Getenv("http_proxy")
	}
	if proxyURL != "" {
		if proxy, err := url.Parse(proxyURL); err == nil {
			transport.Proxy = http.ProxyURL(proxy)
		}
	}

	return &ClaudeProvider{
		apiKey:  apiKey,
		model:   model,
		baseURL: baseURL,
		client: &http.Client{
			Timeout:   600 * time.Second,
			Transport: transport,
		},
	}
}

// GenerateContent 实现AIClient接口
func (p *ClaudeProvider) GenerateContent(ctx context.Context, prompt string, schema map[string]interface{}, useGoogleSearch bool) (string, error) {
	// Claude不支持Google Search，通过prompt增强
	if useGoogleSearch {
		prompt = "请基于最新的网络信息进行分析。\n\n" + prompt
	}

	// 构建Anthropic Messages API请求
	requestBody := map[string]interface{}{
		"model":     p.model,
		"max_tokens": 4096,
		"messages": []map[string]interface{}{
			{
				"role":    "user",
				"content": prompt,
			},
		},
	}

	// 如果提供了schema，在prompt中强调JSON格式要求
	if schema != nil {
		prompt = prompt + "\n\n请以JSON格式输出结果，严格按照提供的schema结构。"
		requestBody["messages"] = []map[string]interface{}{
			{
				"role":    "user",
				"content": prompt,
			},
		}
	}

	jsonData, err := json.Marshal(requestBody)
	if err != nil {
		return "", fmt.Errorf("序列化请求失败: %w", err)
	}

	url := fmt.Sprintf("%s/messages", p.baseURL)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return "", fmt.Errorf("创建请求失败: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", p.apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := p.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("请求失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("Claude API错误: HTTP %d, %s", resp.StatusCode, string(body))
	}

	var result struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("解析响应失败: %w", err)
	}

	if result.Error.Message != "" {
		return "", fmt.Errorf("Claude API错误: %s", result.Error.Message)
	}

	if len(result.Content) == 0 {
		return "", fmt.Errorf("Claude API返回空结果")
	}

	content := result.Content[0].Text
	logger.Info("✅ [Claude] 生成内容完成，模型: %s", p.model)
	return content, nil
}
