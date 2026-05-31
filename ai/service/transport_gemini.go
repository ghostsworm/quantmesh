package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// geminiTransport Google Gemini 协议适配器（generativelanguage）。
// 逻辑逐字迁移自旧 ai_service.GenerateContent，保证 Gemini 行为零回归。
type geminiTransport struct{}

const geminiDefaultBaseURL = "https://generativelanguage.googleapis.com/v1beta"
const geminiDefaultModel = "gemini-3-flash-preview"

func (geminiTransport) Do(ctx context.Context, httpClient *http.Client, req chatRequest) (chatResult, error) {
	// 确定响应 MIME 类型（默认 application/json，可通过 ResponseMimeType 字段覆盖）
	responseMimeType := req.ResponseMimeType
	if responseMimeType == "" {
		responseMimeType = "application/json"
	}

	generationConfig := map[string]interface{}{
		"temperature": 0.7,
		"topK":        40,
		"topP":        0.95,
	}
	// 仅在需要 JSON 格式时设置 responseMimeType（纯文本模式不设置，让模型自由输出）
	if responseMimeType == "application/json" {
		generationConfig["responseMimeType"] = responseMimeType
	}

	geminiReq := map[string]interface{}{
		"contents": []map[string]interface{}{
			{
				"parts": []map[string]interface{}{
					{"text": req.Prompt},
				},
			},
		},
		"generationConfig": generationConfig,
	}

	if req.SystemInstruction != "" {
		geminiReq["system_instruction"] = map[string]interface{}{
			"parts": []map[string]interface{}{
				{"text": req.SystemInstruction},
			},
		}
	}

	// 注意：當啟用 Google Search tool 時，Gemini API 可能無法同時強制 responseSchema
	// 因此我們需要在 prompt 中明確要求 JSON 格式輸出
	if req.JSONSchema != nil {
		// 僅在不使用 Google Search 時設置 responseSchema（因為 tools 和 responseSchema 可能衝突）
		if !req.UseWebSearch {
			generationConfig["responseSchema"] = req.JSONSchema
		}
		// 無論是否使用 Google Search，都保持 responseMimeType 為 application/json
	}

	// 啟用 Google Search 實時搜索（用於新聞分析等场景）
	if req.UseWebSearch {
		geminiReq["tools"] = []map[string]interface{}{
			{"google_search": map[string]interface{}{}},
		}
	}

	jsonData, err := json.Marshal(geminiReq)
	if err != nil {
		return chatResult{}, fmt.Errorf("serialize request failed: %w", err)
	}

	model := req.Model
	if model == "" {
		model = geminiDefaultModel
	}

	baseURL := req.BaseURL
	if baseURL == "" {
		baseURL = geminiDefaultBaseURL
	}
	url := fmt.Sprintf("%s/models/%s:generateContent?key=%s", baseURL, model, req.APIKey)

	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return chatResult{}, fmt.Errorf("create request failed: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(httpReq)
	if err != nil {
		return chatResult{}, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return chatResult{}, fmt.Errorf("API error: %d - %s", resp.StatusCode, string(body))
	}

	var geminiResp struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
			UsageMetadata struct {
				PromptTokenCount     int64 `json:"promptTokenCount"`
				CandidatesTokenCount int64 `json:"candidatesTokenCount"`
				TotalTokenCount      int64 `json:"totalTokenCount"`
			} `json:"usageMetadata"`
		} `json:"candidates"`
		UsageMetadata struct {
			PromptTokenCount     int64 `json:"promptTokenCount"`
			CandidatesTokenCount int64 `json:"candidatesTokenCount"`
			TotalTokenCount      int64 `json:"totalTokenCount"`
		} `json:"usageMetadata"`
	}

	if err := json.Unmarshal(body, &geminiResp); err != nil {
		return chatResult{}, fmt.Errorf("parse response failed: %w (body: %s)", err, string(body))
	}

	if len(geminiResp.Candidates) == 0 || len(geminiResp.Candidates[0].Content.Parts) == 0 {
		return chatResult{}, fmt.Errorf("AI returned no response")
	}

	aiText := trimCodeFence(geminiResp.Candidates[0].Content.Parts[0].Text)

	// 獲取 token 使用情况
	inputTokens := geminiResp.UsageMetadata.PromptTokenCount
	outputTokens := geminiResp.UsageMetadata.CandidatesTokenCount
	if inputTokens == 0 && len(geminiResp.Candidates) > 0 {
		inputTokens = geminiResp.Candidates[0].UsageMetadata.PromptTokenCount
		outputTokens = geminiResp.Candidates[0].UsageMetadata.CandidatesTokenCount
	}

	return chatResult{Text: aiText, InputTokens: inputTokens, OutputTokens: outputTokens}, nil
}
