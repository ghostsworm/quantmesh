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
	"quantmesh/ai/geminiusage"
	"quantmesh/logger"
)

// GeminiClient Gemini API 客户端（支持多模态）
type GeminiClient struct {
	apiKey     string
	model      string
	baseURL    string
	httpClient *http.Client
}

// NewGeminiClient 创建 Gemini 客户端
func NewGeminiClient(apiKey, model string) (types.LLMClient, error) {
	if model == "" {
		model = "gemini-2.0-flash-exp" // 支持多模态的模型
	}
	return &GeminiClient{
		apiKey:  apiKey,
		model:   model,
		baseURL: fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s", model),
		httpClient: &http.Client{
			Timeout: 120 * time.Second, // 多模态可能需要更长时间
		},
	}, nil
}

// geminiRequest Gemini API 请求
type geminiRequest struct {
	Contents         []geminiContent `json:"contents"`
	Tools            []geminiTool    `json:"tools,omitempty"`
	GenerationConfig geminiConfig    `json:"generationConfig,omitempty"`
}

type geminiContent struct {
	Role  string       `json:"role,omitempty"` // user, model
	Parts []geminiPart `json:"parts"`
}

type geminiPart struct {
	Text             string              `json:"text,omitempty"`
	InlineData       []geminiBlob        `json:"inlineData,omitempty"`
	FunctionCall     *geminiFuncCall     `json:"functionCall,omitempty"`
	FunctionResponse *geminiFuncResponse `json:"functionResponse,omitempty"`
}

type geminiBlob struct {
	MimeType string `json:"mimeType"`
	Data     string `json:"data"` // base64 encoded
}

type geminiFuncCall struct {
	Name string                 `json:"name"`
	Args map[string]interface{} `json:"args"`
}

type geminiFuncResponse struct {
	Name     string                 `json:"name"`
	Response map[string]interface{} `json:"response"`
}

type geminiTool struct {
	FunctionDeclarations []geminiFunctionDecl `json:"functionDeclarations"`
}

type geminiFunctionDecl struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters"`
}

type geminiConfig struct {
	Temperature     float64 `json:"temperature,omitempty"`
	MaxOutputTokens int     `json:"maxOutputTokens,omitempty"`
}

// geminiResponse Gemini API 响应
type geminiResponse struct {
	Candidates    []geminiCandidate `json:"candidates"`
	UsageMetadata struct {
		PromptTokenCount     int64 `json:"promptTokenCount"`
		CandidatesTokenCount int64 `json:"candidatesTokenCount"`
		TotalTokenCount      int64 `json:"totalTokenCount"`
	} `json:"usageMetadata"`
}

type geminiCandidate struct {
	Content       geminiContent `json:"content"`
	FinishReason  string        `json:"finishReason"`
	UsageMetadata struct {
		PromptTokenCount     int64 `json:"promptTokenCount"`
		CandidatesTokenCount int64 `json:"candidatesTokenCount"`
		TotalTokenCount      int64 `json:"totalTokenCount"`
	} `json:"usageMetadata"`
}

// Generate 生成响应（支持多模态）
func (c *GeminiClient) Generate(ctx context.Context, req types.GenerateRequest) (types.GenerateResponse, error) {
	started := time.Now()
	// 构建 Gemini 请求
	geminiReq := c.buildRequest(req)

	body, err := json.Marshal(geminiReq)
	if err != nil {
		return types.GenerateResponse{}, err
	}

	// 添加 API Key
	url := fmt.Sprintf("%s:generateContent?key=%s", c.baseURL, c.apiKey)

	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return types.GenerateResponse{}, err
	}

	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return types.GenerateResponse{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return types.GenerateResponse{}, fmt.Errorf("Gemini API error: %s", string(respBody))
	}

	var geminiResp geminiResponse
	if err := json.NewDecoder(resp.Body).Decode(&geminiResp); err != nil {
		return types.GenerateResponse{}, err
	}

	out, err := c.parseResponse(geminiResp)
	if err != nil {
		return types.GenerateResponse{}, err
	}
	geminiusage.Record(geminiusage.Entry{
		At:           time.Now(),
		Model:        c.model,
		Source:       "agent_llm",
		InputTokens:  int64(out.Usage.PromptTokens),
		OutputTokens: int64(out.Usage.CompletionTokens),
		DurationMs:   time.Since(started).Milliseconds(),
	})
	return out, nil
}

// GenerateWithImage 生成响应（带图片）
func (c *GeminiClient) GenerateWithImage(ctx context.Context, text string, images []types.ImageData, req types.GenerateRequest) (types.GenerateResponse, error) {
	started := time.Now()
	// 构建包含图片的内容
	parts := []geminiPart{{Text: text}}

	for _, img := range images {
		parts = append(parts, geminiPart{
			InlineData: []geminiBlob{{
				MimeType: img.MimeType,
				Data:     img.Data, // base64 encoded
			}},
		})
	}

	geminiReq := geminiRequest{
		Contents: []geminiContent{{
			Role:  "user",
			Parts: parts,
		}},
		GenerationConfig: geminiConfig{
			Temperature:     req.Temperature,
			MaxOutputTokens: req.MaxTokens,
		},
	}

	// 添加工具定义
	if len(req.Tools) > 0 {
		geminiReq.Tools = []geminiTool{c.convertTools(req.Tools)}
	}

	body, err := json.Marshal(geminiReq)
	if err != nil {
		return types.GenerateResponse{}, err
	}

	url := fmt.Sprintf("%s:generateContent?key=%s", c.baseURL, c.apiKey)

	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return types.GenerateResponse{}, err
	}

	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return types.GenerateResponse{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return types.GenerateResponse{}, fmt.Errorf("Gemini API error: %s", string(respBody))
	}

	var geminiResp geminiResponse
	if err := json.NewDecoder(resp.Body).Decode(&geminiResp); err != nil {
		return types.GenerateResponse{}, err
	}

	out, err := c.parseResponse(geminiResp)
	if err != nil {
		return types.GenerateResponse{}, err
	}
	geminiusage.Record(geminiusage.Entry{
		At:           time.Now(),
		Model:        c.model,
		Source:       "agent_llm_image",
		InputTokens:  int64(out.Usage.PromptTokens),
		OutputTokens: int64(out.Usage.CompletionTokens),
		DurationMs:   time.Since(started).Milliseconds(),
	})
	return out, nil
}

// GenerateStream 流式生成
func (c *GeminiClient) GenerateStream(ctx context.Context, req types.GenerateRequest) (<-chan types.GenerateChunk, error) {
	ch := make(chan types.GenerateChunk)

	go func() {
		defer close(ch)
		started := time.Now()

		// Gemini 的流式 API 使用不同的端点
		geminiReq := c.buildRequest(req)

		body, err := json.Marshal(geminiReq)
		if err != nil {
			ch <- types.GenerateChunk{IsComplete: true}
			return
		}

		url := fmt.Sprintf("%s:streamGenerateContent?key=%s&alt=sse", c.baseURL, c.apiKey)

		httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
		if err != nil {
			ch <- types.GenerateChunk{IsComplete: true}
			return
		}

		httpReq.Header.Set("Content-Type", "application/json")

		resp, err := c.httpClient.Do(httpReq)
		if err != nil {
			ch <- types.GenerateChunk{IsComplete: true}
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			respBody, _ := io.ReadAll(resp.Body)
			logger.Error("Gemini API error: %s", string(respBody))
			ch <- types.GenerateChunk{IsComplete: true}
			return
		}

		var lastIn, lastOut int64
		// 处理 SSE 流（末帧常帶完整 usageMetadata）
		decoder := json.NewDecoder(resp.Body)
		for {
			var chunk map[string]interface{}
			if err := decoder.Decode(&chunk); err != nil {
				if err == io.EOF {
					break
				}
				continue
			}

			if inTok, outTok, ok := streamChunkUsage(chunk); ok {
				lastIn, lastOut = inTok, outTok
			}

			// 解析增量内容
			if candidates, ok := chunk["candidates"].([]interface{}); ok && len(candidates) > 0 {
				if cand, ok := candidates[0].(map[string]interface{}); ok {
					if content, ok := cand["content"].(map[string]interface{}); ok {
						if parts, ok := content["parts"].([]interface{}); ok && len(parts) > 0 {
							if part, ok := parts[0].(map[string]interface{}); ok {
								if text, ok := part["text"].(string); ok {
									ch <- types.GenerateChunk{Delta: text}
								}
							}
						}
					}
				}
			}
		}

		geminiusage.Record(geminiusage.Entry{
			At:           time.Now(),
			Model:        c.model,
			Source:       "agent_llm_stream",
			InputTokens:  lastIn,
			OutputTokens: lastOut,
			DurationMs:   time.Since(started).Milliseconds(),
		})
		ch <- types.GenerateChunk{IsComplete: true}
	}()

	return ch, nil
}

// buildRequest 构建 Gemini 请求
func (c *GeminiClient) buildRequest(req types.GenerateRequest) geminiRequest {
	contents := make([]geminiContent, 0, len(req.Messages))

	for _, msg := range req.Messages {
		parts := []geminiPart{{Text: msg.Content}}

		// 处理工具调用响应
		if len(msg.ToolCalls) > 0 {
			for _, tc := range msg.ToolCalls {
				argsJSON, _ := json.Marshal(tc.Arguments)
				parts = append(parts, geminiPart{
					FunctionCall: &geminiFuncCall{
						Name: tc.Name,
						Args: map[string]interface{}{
							"args": string(argsJSON),
						},
					},
				})
			}
		}

		contents = append(contents, geminiContent{
			Role:  msg.Role,
			Parts: parts,
		})
	}

	geminiReq := geminiRequest{
		Contents: contents,
		GenerationConfig: geminiConfig{
			Temperature:     req.Temperature,
			MaxOutputTokens: req.MaxTokens,
		},
	}

	// 添加工具定义
	if len(req.Tools) > 0 {
		geminiReq.Tools = []geminiTool{c.convertTools(req.Tools)}
	}

	return geminiReq
}

// convertTools 转换工具定义
func (c *GeminiClient) convertTools(tools []types.ToolDefinition) geminiTool {
	declarations := make([]geminiFunctionDecl, len(tools))

	for i, tool := range tools {
		declarations[i] = geminiFunctionDecl{
			Name:        tool.Name,
			Description: tool.Description,
			Parameters:  tool.Parameters,
		}
	}

	return geminiTool{
		FunctionDeclarations: declarations,
	}
}

func jsonNumberToInt64(v interface{}) (int64, bool) {
	switch x := v.(type) {
	case float64:
		return int64(x), true
	case int64:
		return x, true
	case int:
		return int64(x), true
	case json.Number:
		n, err := x.Int64()
		return n, err == nil
	default:
		return 0, false
	}
}

func readUsagePairFromMap(um map[string]interface{}) (in, out int64) {
	if v, ok := um["promptTokenCount"]; ok {
		if n, ok := jsonNumberToInt64(v); ok {
			in = n
		}
	}
	if v, ok := um["candidatesTokenCount"]; ok {
		if n, ok := jsonNumberToInt64(v); ok {
			out = n
		}
	}
	return in, out
}

// streamChunkUsage 從流式單帧解析 token 用量（與 ai_service 一致：根級優先，候選補全）
func streamChunkUsage(chunk map[string]interface{}) (in, out int64, has bool) {
	if um, ok := chunk["usageMetadata"].(map[string]interface{}); ok {
		in, out = readUsagePairFromMap(um)
		has = true
	}
	if candidates, ok := chunk["candidates"].([]interface{}); ok && len(candidates) > 0 {
		if c0, ok := candidates[0].(map[string]interface{}); ok {
			if um, ok := c0["usageMetadata"].(map[string]interface{}); ok {
				in2, out2 := readUsagePairFromMap(um)
				if in == 0 {
					in = in2
				}
				if out == 0 {
					out = out2
				}
				has = true
			}
		}
	}
	return in, out, has
}

// parseResponse 解析 Gemini 响应
func (c *GeminiClient) parseResponse(resp geminiResponse) (types.GenerateResponse, error) {
	if len(resp.Candidates) == 0 {
		return types.GenerateResponse{
			FinishReason: "no_candidates",
		}, nil
	}

	candidate := resp.Candidates[0]

	inTok := int(resp.UsageMetadata.PromptTokenCount)
	outTok := int(resp.UsageMetadata.CandidatesTokenCount)
	totTok := int(resp.UsageMetadata.TotalTokenCount)
	if inTok == 0 {
		inTok = int(candidate.UsageMetadata.PromptTokenCount)
		outTok = int(candidate.UsageMetadata.CandidatesTokenCount)
		if totTok == 0 {
			totTok = int(candidate.UsageMetadata.TotalTokenCount)
		}
	}

	// 提取文本内容
	var message string
	var toolCalls []types.ToolCall

	for _, part := range candidate.Content.Parts {
		if part.Text != "" {
			message = part.Text
		}
		if part.FunctionCall != nil {
			// 解析函数调用参数
			var args map[string]interface{}
			if argsStr, ok := part.FunctionCall.Args["args"].(string); ok {
				json.Unmarshal([]byte(argsStr), &args)
			} else {
				args = part.FunctionCall.Args
			}

			toolCalls = append(toolCalls, types.ToolCall{
				ID:        fmt.Sprintf("call_%d", time.Now().UnixNano()),
				Name:      part.FunctionCall.Name,
				Arguments: args,
			})
		}
	}

	return types.GenerateResponse{
		Message:      message,
		ToolCalls:    toolCalls,
		FinishReason: candidate.FinishReason,
		Usage: types.TokenUsage{
			PromptTokens:     inTok,
			CompletionTokens: outTok,
			TotalTokens:      totTok,
		},
	}, nil
}
