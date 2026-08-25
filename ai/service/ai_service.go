package service

import (
	"context"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"quantmesh/ai/geminiusage"
)

type AIService struct {
	httpClient *http.Client
}

func NewAIService() *AIService {
	// 創建支援代理的 HTTP 客戶端
	transport := &http.Transport{}

	// 從环境变量读取代理配置（优先级：HTTPS_PROXY > https_proxy > HTTP_PROXY > http_proxy）
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

	return &AIService{
		httpClient: &http.Client{
			Timeout:   600 * time.Second, // 10 分钟，複杂请求可能需要较长時间
			Transport: transport,
		},
	}
}

type AIRequest struct {
	Prompt            string                 `json:"prompt"`
	SystemInstruction string                 `json:"system_instruction"`
	Model             string                 `json:"model"`
	Provider          string                 `json:"provider"`       // gemini(默认) / openai / claude / dashscope / kimi / deepseek / custom
	APIKey            string                 `json:"api_key"`        // 通用 API Key（优先于 GeminiAPIKey）
	BaseURL           string                 `json:"base_url"`       // 可選，自定义 API 端点
	GeminiAPIKey      string                 `json:"gemini_api_key"` // 向后兼容字段，APIKey 为空时回退
	JSONSchema        map[string]interface{} `json:"json_schema"`
	UseGoogleSearch   bool                   `json:"use_google_search"`  // 是否啟用實時搜索（仅 Gemini 原生支持）
	ResponseMimeType  string                 `json:"response_mime_type"` // 响应格式，默认 "application/json"，可设为 "text/plain"
}

// apiKey 返回实际使用的 key：APIKey 优先，回退 GeminiAPIKey（向后兼容）
func (r AIRequest) apiKey() string {
	if strings.TrimSpace(r.APIKey) != "" {
		return r.APIKey
	}
	return r.GeminiAPIKey
}

type AIResponse struct {
	Success          bool   `json:"success"`
	Content          string `json:"content"`
	Error            string `json:"error"`
	InputTokens      int64  `json:"input_tokens"`
	OutputTokens     int64  `json:"output_tokens"`
	ProcessingTimeMs int64  `json:"processing_time_ms"`
	UsedAPIKey       string `json:"used_api_key"`
	AIInput          string `json:"ai_input"`
	AIOutput         string `json:"ai_output"`
}

func (s *AIService) GenerateContent(ctx context.Context, req AIRequest) (*AIResponse, error) {
	startTime := time.Now()

	provider := NormalizeProvider(req.Provider)

	chatReq := chatRequest{
		Prompt:            req.Prompt,
		SystemInstruction: req.SystemInstruction,
		Provider:          provider,
		Model:             req.Model,
		APIKey:            req.apiKey(),
		BaseURL:           req.BaseURL,
		JSONSchema:        req.JSONSchema,
		UseWebSearch:      req.UseGoogleSearch,
		ResponseMimeType:  req.ResponseMimeType,
	}

	transport := resolveTransport(provider)
	result, err := transport.Do(ctx, s.httpClient, chatReq)
	if err != nil {
		// 与历史行为一致：协议层返回的非 200 等错误归一化为 Success=false 的响应；
		// 仅请求构造/网络层错误以 error 形式上抛（保留 context 取消语义供上层判断超时）。
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return &AIResponse{
			Success: false,
			Error:   err.Error(),
		}, nil
	}

	model := req.Model
	if model == "" {
		model = defaultModelForProvider(provider)
	}

	elapsed := time.Since(startTime).Milliseconds()
	geminiusage.Record(geminiusage.Entry{
		At:           time.Now(),
		Model:        usageModelLabel(provider, model),
		Source:       "ai_service",
		InputTokens:  result.InputTokens,
		OutputTokens: result.OutputTokens,
		DurationMs:   elapsed,
	})

	return &AIResponse{
		Success:          true,
		Content:          result.Text,
		InputTokens:      result.InputTokens,
		OutputTokens:     result.OutputTokens,
		ProcessingTimeMs: elapsed,
		UsedAPIKey:       maskAPIKey(chatReq.APIKey),
		AIInput:          req.Prompt,
		AIOutput:         result.Text,
	}, nil
}

// defaultModelForProvider 仅用于 usage 记录的模型标签兜底
func defaultModelForProvider(provider string) string {
	switch NormalizeProvider(provider) {
	case "openai", "poe", "openai_compatible":
		return openAIDefaultModel
	case "dashscope", "dashscope_sg":
		return "qwen-plus"
	case "kimi", "kimi_intl":
		return "kimi-k2.6"
	case "deepseek":
		return "deepseek-v4-pro"
	case "claude", "anthropic":
		return claudeDefaultModel
	default:
		return geminiDefaultModel
	}
}

// usageModelLabel 在 usage 日志中区分 provider（gemini 维持原裸模型名，保证既有统计零回归）
func usageModelLabel(provider, model string) string {
	normalized := NormalizeProvider(provider)
	if normalized == "gemini" || normalized == "" {
		return model
	}
	return normalized + ":" + model
}

func maskAPIKey(key string) string {
	if len(key) <= 10 {
		return "****"
	}
	return key[:6] + "...." + key[len(key)-4:]
}
