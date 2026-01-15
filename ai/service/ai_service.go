package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type AIService struct {
	httpClient *http.Client
}

func NewAIService() *AIService {
	return &AIService{
		httpClient: &http.Client{Timeout: 120 * time.Second},
	}
}

type AIRequest struct {
	Prompt            string                 `json:"prompt"`
	SystemInstruction string                 `json:"system_instruction"`
	Model             string                 `json:"model"`
	GeminiAPIKey      string                 `json:"gemini_api_key"`
	JSONSchema        map[string]interface{} `json:"json_schema"`
}

type AIResponse struct {
	Success          bool    `json:"success"`
	Content          string  `json:"content"`
	Error            string  `json:"error"`
	InputTokens      int64   `json:"input_tokens"`
	OutputTokens     int64   `json:"output_tokens"`
	ProcessingTimeMs int64   `json:"processing_time_ms"`
	UsedAPIKey       string  `json:"used_api_key"`
	AIInput          string  `json:"ai_input"`
	AIOutput         string  `json:"ai_output"`
}

func (s *AIService) GenerateContent(ctx context.Context, req AIRequest) (*AIResponse, error) {
	startTime := time.Now()
	
	geminiReq := map[string]interface{}{
		"contents": []map[string]interface{}{
			{
				"parts": []map[string]interface{}{
					{"text": req.Prompt},
				},
			},
		},
		"generationConfig": map[string]interface{}{
			"temperature":      0.7,
			"topK":             40,
			"topP":             0.95,
			"responseMimeType": "application/json",
		},
	}

	if req.SystemInstruction != "" {
		geminiReq["system_instruction"] = map[string]interface{}{
			"parts": []map[string]interface{}{
				{"text": req.SystemInstruction},
			},
		}
	}

	if req.JSONSchema != nil {
		geminiReq["generationConfig"].(map[string]interface{})["responseSchema"] = req.JSONSchema
	}

	jsonData, err := json.Marshal(geminiReq)
	if err != nil {
		return nil, fmt.Errorf("serialize request failed: %w", err)
	}

	model := req.Model
	if model == "" {
		model = "gemini-3-flash-preview"
	}

	baseURL := "https://generativelanguage.googleapis.com/v1beta"
	url := fmt.Sprintf("%s/models/%s:generateContent?key=%s", baseURL, model, req.GeminiAPIKey)
	
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("create request failed: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return &AIResponse{
			Success: false,
			Error:   fmt.Sprintf("API error: %d - %s", resp.StatusCode, string(body)),
		}, nil
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
		return nil, fmt.Errorf("parse response failed: %w (body: %s)", err, string(body))
	}

	if len(geminiResp.Candidates) == 0 || len(geminiResp.Candidates[0].Content.Parts) == 0 {
		return &AIResponse{
			Success: false,
			Error:   "AI returned no response",
		}, nil
	}

	aiText := geminiResp.Candidates[0].Content.Parts[0].Text
	aiText = strings.TrimPrefix(aiText, "```json")
	aiText = strings.TrimPrefix(aiText, "```")
	aiText = strings.TrimSuffix(aiText, "```")
	aiText = strings.TrimSpace(aiText)

	// 获取 token 使用情况
	inputTokens := geminiResp.UsageMetadata.PromptTokenCount
	outputTokens := geminiResp.UsageMetadata.CandidatesTokenCount
	if inputTokens == 0 && len(geminiResp.Candidates) > 0 {
		inputTokens = geminiResp.Candidates[0].UsageMetadata.PromptTokenCount
		outputTokens = geminiResp.Candidates[0].UsageMetadata.CandidatesTokenCount
	}

	return &AIResponse{
		Success:          true,
		Content:          aiText,
		InputTokens:      inputTokens,
		OutputTokens:     outputTokens,
		ProcessingTimeMs: time.Since(startTime).Milliseconds(),
		UsedAPIKey:       maskAPIKey(req.GeminiAPIKey),
		AIInput:          req.Prompt,
		AIOutput:         aiText,
	}, nil
}

func maskAPIKey(key string) string {
	if len(key) <= 10 {
		return "****"
	}
	return key[:6] + "...." + key[len(key)-4:]
}
