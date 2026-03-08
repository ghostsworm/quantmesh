package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"quantmesh/agent/types"
)

const (
	defaultTimeout = 60 * time.Second
)

// OpenAIClient OpenAI API 客户端
type OpenAIClient struct {
	apiKey     string
	model      string
	baseURL    string
	httpClient *http.Client
	mu         sync.Mutex
}

// NewOpenAIClient 创建 OpenAI 客户端
func NewOpenAIClient(apiKey, model string) (types.LLMClient, error) {
	return &OpenAIClient{
		apiKey:  apiKey,
		model:   model,
		baseURL: "https://api.openai.com/v1/chat/completions",
		httpClient: &http.Client{
			Timeout: defaultTimeout,
		},
	}, nil
}

// openaiRequest OpenAI API 请求
type openaiRequest struct {
	Model       string              `json:"model"`
	Messages    []openaiMsg         `json:"messages"`
	Tools       []openaiTool        `json:"tools,omitempty"`
	Temperature float64             `json:"temperature,omitempty"`
	MaxTokens   int                 `json:"max_tokens,omitempty"`
	Stream      bool                `json:"stream,omitempty"`
}

type openaiMsg struct {
	Role      string         `json:"role"` // system, user, assistant, tool
	Content   string         `json:"content"`
	ToolCalls []openaiToolCall `json:"tool_calls,omitempty"`
	ToolID    string         `json:"tool_call_id,omitempty"`
}

type openaiTool struct {
	Type     string                 `json:"type"` // "function"
	Function openaiToolFunction     `json:"function"`
}

type openaiToolFunction struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters"`
}

type openaiToolCall struct {
	ID       string                  `json:"id"`
	Type     string                  `json:"type"`
	Function openaiToolFunctionCall  `json:"function"`
}

type openaiToolFunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// openaiResponse OpenAI API 响应
type openaiResponse struct {
	ID      string          `json:"id"`
	Object  string          `json:"object"`
	Created int64           `json:"created"`
	Model   string          `json:"model"`
	Choices []openaiChoice  `json:"choices"`
	Usage   openaiUsage     `json:"usage"`
}

type openaiChoice struct {
	Index        int             `json:"index"`
	Message      openaiMsg       `json:"message"`
	FinishReason string          `json:"finish_reason"`
}

type openaiUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// Generate 生成响应
func (c *OpenAIClient) Generate(ctx context.Context, req types.GenerateRequest) (types.GenerateResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// 构建 OpenAI 请求
	openaiReq := openaiRequest{
		Model:       c.model,
		Messages:    convertToOpenAIMessages(req.Messages),
		Temperature: req.Temperature,
		MaxTokens:   req.MaxTokens,
	}

	// 添加工具定义
	if len(req.Tools) > 0 {
		openaiReq.Tools = make([]openaiTool, len(req.Tools))
		for i, tool := range req.Tools {
			openaiReq.Tools[i] = openaiTool{
				Type: "function",
				Function: openaiToolFunction{
					Name:        tool.Name,
					Description: tool.Description,
					Parameters:  tool.Parameters,
				},
			}
		}
	}

	body, err := json.Marshal(openaiReq)
	if err != nil {
		return types.GenerateResponse{}, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL, bytes.NewReader(body))
	if err != nil {
		return types.GenerateResponse{}, err
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return types.GenerateResponse{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return types.GenerateResponse{}, fmt.Errorf("OpenAI API error: %s", string(respBody))
	}

	var openaiResp openaiResponse
	if err := json.NewDecoder(resp.Body).Decode(&openaiResp); err != nil {
		return types.GenerateResponse{}, err
	}

	// 转换响应
	return convertFromOpenAIResponse(openaiResp), nil
}

// GenerateStream 流式生成
func (c *OpenAIClient) GenerateStream(ctx context.Context, req types.GenerateRequest) (<-chan types.GenerateChunk, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	ch := make(chan types.GenerateChunk)

	go func() {
		defer close(ch)

		// 构建请求
		openaiReq := openaiRequest{
			Model:       c.model,
			Messages:    convertToOpenAIMessages(req.Messages),
			Temperature: req.Temperature,
			MaxTokens:   req.MaxTokens,
			Stream:      true,
		}

		body, err := json.Marshal(openaiReq)
		if err != nil {
			ch <- types.GenerateChunk{IsComplete: true}
			return
		}

		httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL, bytes.NewReader(body))
		if err != nil {
			ch <- types.GenerateChunk{IsComplete: true}
			return
		}

		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)

		resp, err := c.httpClient.Do(httpReq)
		if err != nil {
			ch <- types.GenerateChunk{IsComplete: true}
			return
		}
		defer resp.Body.Close()

		// 处理 SSE 流
		decoder := json.NewDecoder(resp.Body)
		for {
			var chunk map[string]interface{}
			if err := decoder.Decode(&chunk); err != nil {
				if err == io.EOF {
					break
				}
				ch <- types.GenerateChunk{IsComplete: true}
				return
			}

			// 解析 SSE 格式
			delta, ok := chunk["choices"].([]interface{})
			if !ok || len(delta) == 0 {
				continue
			}

			// 发送块
			ch <- types.GenerateChunk{
				Delta:      fmt.Sprintf("%v", delta),
				IsComplete: false,
			}
		}

		ch <- types.GenerateChunk{IsComplete: true}
	}()

	return ch, nil
}

// convertToOpenAIMessages 转换消息格式
func convertToOpenAIMessages(messages []types.LLMMessage) []openaiMsg {
	result := make([]openaiMsg, len(messages))
	for i, msg := range messages {
		result[i] = openaiMsg{
			Role:    msg.Role,
			Content: msg.Content,
			ToolID:  msg.ToolID,
		}

		if len(msg.ToolCalls) > 0 {
			result[i].ToolCalls = make([]openaiToolCall, len(msg.ToolCalls))
			for j, tc := range msg.ToolCalls {
				args, _ := json.Marshal(tc.Arguments)
				result[i].ToolCalls[j] = openaiToolCall{
					ID:   tc.ID,
					Type: "function",
					Function: openaiToolFunctionCall{
						Name:      tc.Name,
						Arguments: string(args),
					},
				}
			}
		}
	}
	return result
}

// convertFromOpenAIResponse 转换 OpenAI 响应
func convertFromOpenAIResponse(resp openaiResponse) types.GenerateResponse {
	if len(resp.Choices) == 0 {
		return types.GenerateResponse{
			FinishReason: "no_choices",
		}
	}

	choice := resp.Choices[0]
	toolCalls := make([]types.ToolCall, 0)

	for _, tc := range choice.Message.ToolCalls {
		var args map[string]interface{}
		json.Unmarshal([]byte(tc.Function.Arguments), &args)

		toolCalls = append(toolCalls, types.ToolCall{
			ID:        tc.ID,
			Name:      tc.Function.Name,
			Arguments: args,
		})
	}

	return types.GenerateResponse{
		Message:      choice.Message.Content,
		ToolCalls:    toolCalls,
		FinishReason: choice.FinishReason,
		Usage: types.TokenUsage{
			PromptTokens:     resp.Usage.PromptTokens,
			CompletionTokens: resp.Usage.CompletionTokens,
			TotalTokens:      resp.Usage.TotalTokens,
		},
	}
}

// GenerateWithImage OpenAI 暂不支持图片输入
func (c *OpenAIClient) GenerateWithImage(ctx context.Context, text string, images []types.ImageData, req types.GenerateRequest) (types.GenerateResponse, error) {
	return types.GenerateResponse{
		FinishReason: "not_implemented",
		Message:      "OpenAI 客户端暂不支持图片输入，请使用 Gemini 客户端",
	}, nil
}
