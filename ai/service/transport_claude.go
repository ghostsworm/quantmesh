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

// claudeTransport Anthropic Claude Messages 协议适配器（/v1/messages）。
type claudeTransport struct{}

const claudeDefaultBaseURL = "https://api.anthropic.com/v1"
const claudeDefaultModel = "claude-3-5-sonnet-latest"
const claudeAPIVersion = "2023-06-01"
const claudeDefaultMaxTokens = 4096

func (claudeTransport) Do(ctx context.Context, httpClient *http.Client, req chatRequest) (chatResult, error) {
	prompt := req.Prompt

	// Claude 不原生支持 Google Search，通过 prompt 增强
	if req.UseWebSearch {
		prompt = "请基于最新的网络信息进行分析。\n\n" + prompt
	}
	if req.JSONSchema != nil {
		prompt = prompt + "\n\n请严格按照提供的 JSON schema 结构以 JSON 格式输出结果，不要输出除 JSON 外的任何内容。"
	}

	model := req.Model
	if model == "" {
		model = claudeDefaultModel
	}

	requestBody := map[string]interface{}{
		"model":      model,
		"max_tokens": claudeDefaultMaxTokens,
		"messages": []map[string]interface{}{
			{"role": "user", "content": prompt},
		},
	}
	if req.SystemInstruction != "" {
		requestBody["system"] = req.SystemInstruction
	}

	jsonData, err := json.Marshal(requestBody)
	if err != nil {
		return chatResult{}, fmt.Errorf("serialize request failed: %w", err)
	}

	baseURL := req.BaseURL
	if baseURL == "" {
		baseURL = claudeDefaultBaseURL
	}
	url := fmt.Sprintf("%s/messages", strings.TrimRight(baseURL, "/"))

	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return chatResult{}, fmt.Errorf("create request failed: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", req.APIKey)
	httpReq.Header.Set("anthropic-version", claudeAPIVersion)

	resp, err := httpClient.Do(httpReq)
	if err != nil {
		return chatResult{}, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return chatResult{}, fmt.Errorf("Claude API error: %d - %s", resp.StatusCode, string(body))
	}

	var result struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		Usage struct {
			InputTokens  int64 `json:"input_tokens"`
			OutputTokens int64 `json:"output_tokens"`
		} `json:"usage"`
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return chatResult{}, fmt.Errorf("parse response failed: %w (body: %s)", err, string(body))
	}

	if result.Error.Message != "" {
		return chatResult{}, fmt.Errorf("Claude API error: %s", result.Error.Message)
	}

	// 拼接所有 text 类型 content block
	var sb strings.Builder
	for _, c := range result.Content {
		if c.Type == "text" || c.Type == "" {
			sb.WriteString(c.Text)
		}
	}
	text := trimCodeFence(sb.String())
	if text == "" {
		return chatResult{}, fmt.Errorf("AI returned no response")
	}

	return chatResult{
		Text:         text,
		InputTokens:  result.Usage.InputTokens,
		OutputTokens: result.Usage.OutputTokens,
	}, nil
}
