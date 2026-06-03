package llm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"quantmesh/agent/types"
)

func TestOpenAIConversionsAndGenerate(t *testing.T) {
	messages := []types.LLMMessage{{
		Role: "assistant", Content: "call tool", ToolID: "tool-msg",
		ToolCalls: []types.ToolCall{{ID: "call-1", Name: "get_status", Arguments: map[string]interface{}{"symbol": "BTCUSDT"}}},
	}}
	converted := convertToOpenAIMessages(messages)
	if len(converted) != 1 || len(converted[0].ToolCalls) != 1 || !strings.Contains(converted[0].ToolCalls[0].Function.Arguments, "BTCUSDT") {
		t.Fatalf("converted OpenAI messages = %#v", converted)
	}

	resp := convertFromOpenAIResponse(openaiResponse{
		Choices: []openaiChoice{{
			Message: openaiMsg{Content: "done", ToolCalls: []openaiToolCall{{
				ID: "call-1", Function: openaiToolFunctionCall{Name: "get_status", Arguments: `{"ok":true}`},
			}}},
			FinishReason: "tool_calls",
		}},
		Usage: openaiUsage{PromptTokens: 1, CompletionTokens: 2, TotalTokens: 3},
	})
	if resp.Message != "done" || len(resp.ToolCalls) != 1 || resp.FinishReason != "tool_calls" || resp.Usage.TotalTokens != 3 {
		t.Fatalf("OpenAI response = %#v", resp)
	}
	if noChoices := convertFromOpenAIResponse(openaiResponse{}); noChoices.FinishReason != "no_choices" {
		t.Fatalf("no choices response = %#v", noChoices)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer key" {
			t.Fatalf("authorization = %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":4,"completion_tokens":5,"total_tokens":9}}`))
	}))
	defer server.Close()

	client := &OpenAIClient{apiKey: "key", model: "gpt-test", baseURL: server.URL, httpClient: server.Client()}
	got, err := client.Generate(context.Background(), types.GenerateRequest{
		Messages:    []types.LLMMessage{{Role: "user", Content: "hello"}},
		Tools:       []types.ToolDefinition{{Name: "get_status", Description: "status", Parameters: map[string]interface{}{"type": "object"}}},
		Temperature: 0.2, MaxTokens: 16,
	})
	if err != nil {
		t.Fatalf("OpenAI Generate returned error: %v", err)
	}
	if got.Message != "ok" || got.Usage.TotalTokens != 9 {
		t.Fatalf("OpenAI Generate = %#v", got)
	}
	imageResp, err := client.GenerateWithImage(context.Background(), "text", nil, types.GenerateRequest{})
	if err != nil || imageResp.FinishReason != "not_implemented" {
		t.Fatalf("OpenAI image response = %#v, err=%v", imageResp, err)
	}
}

func TestClaudeConversionsAndGenerate(t *testing.T) {
	msgs := convertToClaudeMessages([]types.LLMMessage{
		{Role: "system", Content: "ignored"},
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "hi"},
		{Role: "tool", ToolID: "tool-1", Content: "tool result"},
	})
	if len(msgs) != 3 || msgs[0].Role != "user" || msgs[2].Role != "user" {
		t.Fatalf("Claude messages = %#v", msgs)
	}

	tools := convertToClaudeTools([]types.ToolDefinition{{Name: "risk", Description: "risk check", Parameters: map[string]interface{}{"score": map[string]interface{}{"type": "number"}}}})
	if len(tools) != 1 || tools[0].InputSchema["type"] != "object" {
		t.Fatalf("Claude tools = %#v", tools)
	}

	resp := convertFromClaudeResponse(claudeResponse{
		Content: []contentBlock{
			{Type: "text", Text: "hello "},
			{Type: "tool_use", ToolUse: &claudeToolUse{ID: "tool-1", Name: "risk", Input: map[string]interface{}{"score": 1}}},
			{Type: "text", Text: "world"},
		},
		StopReason: "end_turn",
		Usage:      claudeUsage{InputTokens: 6, OutputTokens: 7},
	})
	if resp.Message != "hello world" || len(resp.ToolCalls) != 1 || resp.Usage.TotalTokens != 13 {
		t.Fatalf("Claude response = %#v", resp)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != "key" {
			t.Fatalf("x-api-key = %q", r.Header.Get("x-api-key"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":8,"output_tokens":9}}`))
	}))
	defer server.Close()

	client := NewClaudeClient("key", "")
	client.baseURL = server.URL
	client.client = server.Client()
	got, err := client.Generate(context.Background(), types.GenerateRequest{
		Messages:  []types.LLMMessage{{Role: "user", Content: "hello"}},
		Tools:     []types.ToolDefinition{{Name: "risk", Description: "risk", Parameters: map[string]interface{}{"type": "object"}}},
		MaxTokens: 32,
	})
	if err != nil {
		t.Fatalf("Claude Generate returned error: %v", err)
	}
	if got.Message != "ok" || got.Usage.TotalTokens != 17 {
		t.Fatalf("Claude Generate = %#v", got)
	}
	imageResp, err := client.GenerateWithImage(context.Background(), "text", nil, types.GenerateRequest{})
	if err != nil || imageResp.FinishReason != "not_implemented" {
		t.Fatalf("Claude image response = %#v, err=%v", imageResp, err)
	}
}

func TestGeminiBuildParseAndGenerateWithImage(t *testing.T) {
	client := &GeminiClient{apiKey: "key", model: "gemini-test"}
	req := client.buildRequest(types.GenerateRequest{
		Messages: []types.LLMMessage{{
			Role: "user", Content: "hello",
			ToolCalls: []types.ToolCall{{Name: "risk", Arguments: map[string]interface{}{"score": 1}}},
		}},
		Tools:       []types.ToolDefinition{{Name: "risk", Description: "risk", Parameters: map[string]interface{}{"type": "object"}}},
		Temperature: 0.3, MaxTokens: 64,
	})
	if len(req.Contents) != 1 || len(req.Contents[0].Parts) != 2 || len(req.Tools) != 1 {
		t.Fatalf("Gemini request = %#v", req)
	}

	parsed, err := client.parseResponse(geminiResponse{
		Candidates: []geminiCandidate{{
			Content: geminiContent{Parts: []geminiPart{
				{Text: "hello"},
				{FunctionCall: &geminiFuncCall{Name: "risk", Args: map[string]interface{}{"args": `{"score":2}`}}},
			}},
			FinishReason: "STOP",
			UsageMetadata: struct {
				PromptTokenCount     int64 `json:"promptTokenCount"`
				CandidatesTokenCount int64 `json:"candidatesTokenCount"`
				TotalTokenCount      int64 `json:"totalTokenCount"`
			}{PromptTokenCount: 10, CandidatesTokenCount: 11, TotalTokenCount: 21},
		}},
	})
	if err != nil {
		t.Fatalf("parseResponse returned error: %v", err)
	}
	if parsed.Message != "hello" || len(parsed.ToolCalls) != 1 || parsed.Usage.TotalTokens != 21 {
		t.Fatalf("Gemini parsed response = %#v", parsed)
	}
	noCandidates, err := client.parseResponse(geminiResponse{})
	if err != nil || noCandidates.FinishReason != "no_candidates" {
		t.Fatalf("Gemini no candidates = %#v, err=%v", noCandidates, err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("key") != "key" {
			t.Fatalf("Gemini key query = %q", r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"candidates":[{"content":{"parts":[{"text":"image ok"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":12,"candidatesTokenCount":13,"totalTokenCount":25}}`))
	}))
	defer server.Close()
	client.baseURL = server.URL + "/models/gemini-test"
	client.httpClient = server.Client()

	got, err := client.GenerateWithImage(context.Background(), "describe", []types.ImageData{{MimeType: "image/png", Data: "abc"}}, types.GenerateRequest{
		Tools: []types.ToolDefinition{{Name: "risk", Description: "risk", Parameters: map[string]interface{}{"type": "object"}}},
	})
	if err != nil {
		t.Fatalf("GenerateWithImage returned error: %v", err)
	}
	if got.Message != "image ok" || got.Usage.TotalTokens != 25 {
		t.Fatalf("GenerateWithImage = %#v", got)
	}
}
