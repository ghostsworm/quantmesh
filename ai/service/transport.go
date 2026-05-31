package service

import (
	"context"
	"net/http"
	"strings"
)

// chatRequest 协议无关的统一请求（由 AIRequest 归一化而来）
type chatRequest struct {
	Prompt            string
	SystemInstruction string
	Model             string
	APIKey            string
	BaseURL           string
	JSONSchema        map[string]interface{}
	UseWebSearch      bool   // 由原 UseGoogleSearch 泛化；仅 Gemini 原生支持，其余通过 prompt 增强
	ResponseMimeType  string // "application/json"（默认）或 "text/plain"
}

// chatResult transport 返回的统一结果
type chatResult struct {
	Text         string
	InputTokens  int64
	OutputTokens int64
}

// providerTransport 每家协议一个实现：负责构造 HTTP 请求、发送、解析响应/usage、错误映射
type providerTransport interface {
	// Do 执行一次生成调用。httpClient 由 AIService 提供（已带代理/超时）。
	Do(ctx context.Context, httpClient *http.Client, req chatRequest) (chatResult, error)
}

// resolveTransport 按 provider 选择协议适配器。
// 空值或未知 provider 一律回退 gemini，保持历史行为零回归。
// poe 归入 OpenAI 兼容协议（用户填 base_url 即可）。
func resolveTransport(provider string) providerTransport {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "openai", "poe":
		return openAITransport{}
	case "claude", "anthropic":
		return claudeTransport{}
	case "gemini", "":
		return geminiTransport{}
	default:
		return geminiTransport{}
	}
}

// trimCodeFence 去除模型输出里常见的 ```json ... ``` 包裹，统一各协议解析行为
func trimCodeFence(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "```json")
	s = strings.TrimPrefix(s, "```")
	s = strings.TrimSuffix(s, "```")
	return strings.TrimSpace(s)
}
