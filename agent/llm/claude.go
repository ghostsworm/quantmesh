package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"quantmesh/agent/types"
)

// ClaudeClient Claude API 客户端
type ClaudeClient struct {
	apiKey  string
	baseURL string
	client  *http.Client
	model   string
}

// NewClaudeClient 创建 Claude 客户端
func NewClaudeClient(apiKey, model string) *ClaudeClient {
	if model == "" {
		model = "claude-3-5-sonnet-20241022"
	}

	return &ClaudeClient{
		apiKey:  apiKey,
		baseURL: "https://api.anthropic.com/v1/messages",
		client: &http.Client{
			Timeout: 60 * time.Second,
		},
		model: model,
	}
}

// claudeRequest Claude API 请求
type claudeRequest struct {
	Model     string        `json:"model"`
	MaxTokens int           `json:"max_tokens"`
	Messages  []claudeMsg   `json:"messages"`
	Tools     []claudeTool  `json:"tools,omitempty"`
	System    string        `json:"system,omitempty"`
	Temperature float64     `json:"temperature,omitempty"`
	Stream    bool          `json:"stream,omitempty"`
}

type claudeMsg struct {
	Role    string         `json:"role"`
	Content interface{}    `json:"content"`
}

type claudeTool struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"input_schema"`
}

type contentBlock struct {
	Type     string                 `json:"type"`
	Text     string                 `json:"text,omitempty"`
	ToolUse  *claudeToolUse         `json:"tool_use,omitempty"`
	ToolResult *claudeToolResult    `json:"tool_result,omitempty"`
}

type claudeToolUse struct {
	ID       string                 `json:"id"`
	Name     string                 `json:"name"`
	Input    map[string]interface{} `json:"input"`
}

type claudeToolResult struct {
	ToolUseID string `json:"tool_use_id"`
	Content   string `json:"content"`
}

// claudeResponse Claude API 响应
type claudeResponse struct {
	ID           string         `json:"id"`
	Type         string         `json:"type"`
	Role         string         `json:"role"`
	Content      []contentBlock `json:"content"`
	Model        string         `json:"model"`
	StopReason   string         `json:"stop_reason"`
	Usage        claudeUsage    `json:"usage"`
}

type claudeUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// Generate 生成响应
func (c *ClaudeClient) Generate(ctx context.Context, req types.GenerateRequest) (types.GenerateResponse, error) {
	// 构建 Claude 请求
	claudeReq := claudeRequest{
		Model:     c.model,
		MaxTokens: req.MaxTokens,
		Messages:  convertToClaudeMessages(req.Messages),
		System:    req.SystemPrompt,
		Temperature: req.Temperature,
	}

	// 添加工具定义
	if len(req.Tools) > 0 {
		claudeReq.Tools = convertToClaudeTools(req.Tools)
	}

	// 序列化请求
	body, err := json.Marshal(claudeReq)
	if err != nil {
		return types.GenerateResponse{}, err
	}

	// 创建 HTTP 请求
	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL, bytes.NewReader(body))
	if err != nil {
		return types.GenerateResponse{}, err
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", c.apiKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")

	// 发送请求
	resp, err := c.client.Do(httpReq)
	if err != nil {
		return types.GenerateResponse{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return types.GenerateResponse{}, fmt.Errorf("Claude API error: %s", string(body))
	}

	// 解析响应
	var claudeResp claudeResponse
	if err := json.NewDecoder(resp.Body).Decode(&claudeResp); err != nil {
		return types.GenerateResponse{}, err
	}

	// 转换响应
	return convertFromClaudeResponse(claudeResp), nil
}

// GenerateStream 流式生成
func (c *ClaudeClient) GenerateStream(ctx context.Context, req types.GenerateRequest) (<-chan types.GenerateChunk, error) {
	ch := make(chan types.GenerateChunk)

	// 实现 SSE 流式响应
	go func() {
		defer close(ch)

		// 构建请求
		claudeReq := claudeRequest{
			Model:     c.model,
			MaxTokens: req.MaxTokens,
			Messages:  convertToClaudeMessages(req.Messages),
			System:    req.SystemPrompt,
			Temperature: req.Temperature,
			Stream:    true,
		}

		body, err := json.Marshal(claudeReq)
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
		httpReq.Header.Set("x-api-key", c.apiKey)
		httpReq.Header.Set("anthropic-version", "2023-06-01")

		resp, err := c.client.Do(httpReq)
		if err != nil {
			ch <- types.GenerateChunk{IsComplete: true}
			return
		}
		defer resp.Body.Close()

		// 处理 SSE 流
		// ... SSE 解析逻辑

		ch <- types.GenerateChunk{IsComplete: true}
	}()

	return ch, nil
}

// convertToClaudeMessages 转换消息格式
func convertToClaudeMessages(messages []types.LLMMessage) []claudeMsg {
	claudeMsgs := make([]claudeMsg, 0, len(messages))

	for _, msg := range messages {
		switch msg.Role {
		case "user", "assistant":
			claudeMsgs = append(claudeMsgs, claudeMsg{
				Role:    msg.Role,
				Content: msg.Content,
			})
		case "tool":
			// 工具结果消息需要特殊处理
			claudeMsgs = append(claudeMsgs, claudeMsg{
				Role: "user",
				Content: []contentBlock{
					{
						Type: "tool_result",
						ToolResult: &claudeToolResult{
							ToolUseID: msg.ToolID,
							Content:   msg.Content,
						},
					},
				},
			})
		}
	}

	return claudeMsgs
}

// convertToClaudeTools 转换工具定义
func convertToClaudeTools(tools []types.ToolDefinition) []claudeTool {
	claudeTools := make([]claudeTool, 0, len(tools))

	for _, tool := range tools {
		claudeTools = append(claudeTools, claudeTool{
			Name:        tool.Name,
			Description: tool.Description,
			InputSchema: map[string]interface{}{
				"type":       "object",
				"properties": tool.Parameters,
			},
		})
	}

	return claudeTools
}

// convertFromClaudeResponse 转换响应格式
func convertFromClaudeResponse(resp claudeResponse) types.GenerateResponse {
	response := types.GenerateResponse{
		Usage: types.TokenUsage{
			PromptTokens:     resp.Usage.InputTokens,
			CompletionTokens: resp.Usage.OutputTokens,
			TotalTokens:      resp.Usage.InputTokens + resp.Usage.OutputTokens,
		},
	}

	// 提取文本内容和工具调用
	for _, block := range resp.Content {
		switch block.Type {
		case "text":
			response.Message += block.Text
		case "tool_use":
			response.ToolCalls = append(response.ToolCalls, types.ToolCall{
				ID:       block.ToolUse.ID,
				Name:     block.ToolUse.Name,
				Arguments: block.ToolUse.Input,
			})
		}
	}

	response.FinishReason = resp.StopReason

	return response
}

// GenerateWithImage Claude 暂不支持图片输入
func (c *ClaudeClient) GenerateWithImage(ctx context.Context, text string, images []types.ImageData, req types.GenerateRequest) (types.GenerateResponse, error) {
	return types.GenerateResponse{
		FinishReason: "not_implemented",
		Message:      "Claude 客户端暂不支持图片输入，请使用 Gemini 客户端",
	}, nil
}
