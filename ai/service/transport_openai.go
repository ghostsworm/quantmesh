package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// openAITransport OpenAI Chat Completions 兼容协议适配器。
// 覆盖 OpenAI / DeepSeek / Moonshot / 智谱 / Ollama / OneAPI 中转 / Poe 等绝大多数 provider。
type openAITransport struct{}

const openAIDefaultBaseURL = "https://api.openai.com/v1"
const openAIDefaultModel = "gpt-4o-mini"

func defaultOpenAICompatibleBaseURL(provider string) string {
	switch NormalizeProvider(provider) {
	case "dashscope":
		return "https://dashscope.aliyuncs.com/compatible-mode/v1"
	case "dashscope_sg":
		return "https://{WorkspaceId}.ap-southeast-1.maas.aliyuncs.com/compatible-mode/v1"
	case "kimi":
		return "https://api.moonshot.cn/v1"
	case "kimi_intl":
		return "https://api.moonshot.ai/v1"
	case "deepseek":
		return "https://api.deepseek.com"
	default:
		return openAIDefaultBaseURL
	}
}

func (openAITransport) Do(ctx context.Context, httpClient *http.Client, req chatRequest) (chatResult, error) {
	prompt := req.Prompt

	// OpenAI 兼容协议不原生支持 Google Search，通过 prompt 增强
	if req.UseWebSearch {
		prompt = "请基于最新的网络信息进行分析。\n\n" + prompt
	}

	wantJSON := req.JSONSchema != nil || strings.EqualFold(strings.TrimSpace(req.ResponseMimeType), "application/json")
	if req.JSONSchema != nil {
		// 在 prompt 中强调 JSON 格式要求（JSON mode 只保证合法 JSON，不保证 schema）
		prompt = prompt + "\n\n请严格按照提供的 JSON schema 结构以 JSON 格式输出结果。"
	}

	messages := make([]map[string]interface{}, 0, 2)
	if req.SystemInstruction != "" {
		messages = append(messages, map[string]interface{}{
			"role":    "system",
			"content": req.SystemInstruction,
		})
	}
	messages = append(messages, map[string]interface{}{
		"role":    "user",
		"content": prompt,
	})

	model := req.Model
	if model == "" {
		model = openAIDefaultModel
	}

	requestBody := map[string]interface{}{
		"model":       model,
		"messages":    messages,
		"temperature": 0.7,
	}
	if wantJSON {
		requestBody["response_format"] = map[string]interface{}{"type": "json_object"}
	}

	jsonData, err := json.Marshal(requestBody)
	if err != nil {
		return chatResult{}, fmt.Errorf("serialize request failed: %w", err)
	}

	baseURL := req.BaseURL
	if baseURL == "" {
		baseURL = defaultOpenAICompatibleBaseURL(req.Provider)
	}
	url := fmt.Sprintf("%s/chat/completions", strings.TrimRight(baseURL, "/"))

	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return chatResult{}, fmt.Errorf("create request failed: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+req.APIKey)

	resp, err := httpClient.Do(httpReq)
	if err != nil {
		return chatResult{}, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return chatResult{}, fmt.Errorf("OpenAI API error: %d - %s", resp.StatusCode, string(body))
	}

	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int64 `json:"prompt_tokens"`
			CompletionTokens int64 `json:"completion_tokens"`
			TotalTokens      int64 `json:"total_tokens"`
		} `json:"usage"`
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return chatResult{}, fmt.Errorf("parse response failed: %w (body: %s)", err, string(body))
	}

	if result.Error.Message != "" {
		return chatResult{}, fmt.Errorf("OpenAI API error: %s", result.Error.Message)
	}
	if len(result.Choices) == 0 {
		return chatResult{}, fmt.Errorf("AI returned no response")
	}

	text := trimCodeFence(result.Choices[0].Message.Content)
	return chatResult{
		Text:         text,
		InputTokens:  result.Usage.PromptTokens,
		OutputTokens: result.Usage.CompletionTokens,
	}, nil
}
