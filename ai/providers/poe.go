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

// PoeProvider Poe代理AI Provider实现
type PoeProvider struct {
	apiKey  string
	model   string
	baseURL string
	client  *http.Client
}

// NewPoeProvider 创建Poe Provider
func NewPoeProvider(apiKey, model, baseURL string) *PoeProvider {
	if model == "" {
		model = "gpt-4" // 默认模型
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

	return &PoeProvider{
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
// Poe通常使用OpenAI兼容的API格式
func (p *PoeProvider) GenerateContent(ctx context.Context, prompt string, schema map[string]interface{}, useGoogleSearch bool) (string, error) {
	// Poe不支持Google Search，通过prompt增强
	if useGoogleSearch {
		prompt = "请基于最新的网络信息进行分析。\n\n" + prompt
	}

	// 构建OpenAI兼容的Chat Completions API请求
	requestBody := map[string]interface{}{
		"model": p.model,
		"messages": []map[string]interface{}{
			{
				"role":    "user",
				"content": prompt,
			},
		},
		"temperature": 0.7,
	}

	// 如果提供了schema，尝试使用JSON mode
	if schema != nil {
		requestBody["response_format"] = map[string]interface{}{
			"type": "json_object",
		}
		// 在prompt中强调JSON格式要求
		prompt = prompt + "\n\n请以JSON格式输出结果。"
	}

	jsonData, err := json.Marshal(requestBody)
	if err != nil {
		return "", fmt.Errorf("序列化请求失败: %w", err)
	}

	// Poe通常使用 /v1/chat/completions 端点
	url := fmt.Sprintf("%s/v1/chat/completions", p.baseURL)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return "", fmt.Errorf("创建请求失败: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", p.apiKey))

	resp, err := p.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("请求失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("Poe API错误: HTTP %d, %s", resp.StatusCode, string(body))
	}

	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("解析响应失败: %w", err)
	}

	if result.Error.Message != "" {
		return "", fmt.Errorf("Poe API错误: %s", result.Error.Message)
	}

	if len(result.Choices) == 0 {
		return "", fmt.Errorf("Poe API返回空结果")
	}

	content := result.Choices[0].Message.Content
	logger.Info("✅ [Poe] 生成内容完成，模型: %s", p.model)
	return content, nil
}
