package service

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestResolveTransport(t *testing.T) {
	tests := []struct {
		provider string
		wantType string
	}{
		{provider: "openai", wantType: "service.openAITransport"},
		{provider: "poe", wantType: "service.openAITransport"},
		{provider: "dashscope", wantType: "service.openAITransport"},
		{provider: "dashscope_sg", wantType: "service.openAITransport"},
		{provider: "kimi", wantType: "service.openAITransport"},
		{provider: "kimi_intl", wantType: "service.openAITransport"},
		{provider: "deepseek", wantType: "service.openAITransport"},
		{provider: "custom", wantType: "service.openAITransport"},
		{provider: "claude", wantType: "service.claudeTransport"},
		{provider: "anthropic", wantType: "service.claudeTransport"},
		{provider: "gemini", wantType: "service.geminiTransport"},
		{provider: "", wantType: "service.geminiTransport"},
		{provider: "unknown", wantType: "service.geminiTransport"},
		{provider: "  OPENAI  ", wantType: "service.openAITransport"},
	}

	for _, tt := range tests {
		t.Run(tt.provider, func(t *testing.T) {
			got := resolveTransport(tt.provider)
			if gotType := fmt.Sprintf("%T", got); gotType != tt.wantType {
				t.Fatalf("resolveTransport(%q) = %s, want %s", tt.provider, gotType, tt.wantType)
			}
		})
	}
}

func TestAIServiceGenerateContentSuccessErrorAndCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.String(), "fail=1"):
			http.Error(w, "provider failed", http.StatusBadGateway)
		case strings.HasSuffix(r.URL.Path, "/chat/completions"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"openai text"}}],"usage":{"prompt_tokens":3,"completion_tokens":5}}`))
		case strings.Contains(r.URL.Path, ":generateContent"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"candidates":[{"content":{"parts":[{"text":"gemini text"}]}}],"usageMetadata":{"promptTokenCount":7,"candidatesTokenCount":9}}`))
		default:
			t.Fatalf("unexpected path: %s", r.URL.String())
		}
	}))
	defer server.Close()

	service := &AIService{httpClient: server.Client()}
	ctx := context.Background()

	openAIResp, err := service.GenerateContent(ctx, AIRequest{
		Prompt:            "prompt",
		SystemInstruction: "system",
		Provider:          " openai ",
		APIKey:            "1234567890abcdef",
		BaseURL:           server.URL,
		Model:             "test-model",
		JSONSchema:        map[string]interface{}{"type": "object"},
		UseGoogleSearch:   true,
	})
	if err != nil {
		t.Fatalf("GenerateContent openai returned error: %v", err)
	}
	if !openAIResp.Success || openAIResp.Content != "openai text" || openAIResp.InputTokens != 3 || openAIResp.OutputTokens != 5 {
		t.Fatalf("unexpected openai response: %#v", openAIResp)
	}
	if openAIResp.UsedAPIKey != "123456....cdef" || openAIResp.AIInput != "prompt" || openAIResp.AIOutput != "openai text" {
		t.Fatalf("unexpected metadata: %#v", openAIResp)
	}

	dashScopeResp, err := service.GenerateContent(ctx, AIRequest{
		Prompt:   "prompt",
		Provider: "dashscope",
		APIKey:   "1234567890abcdef",
		BaseURL:  server.URL,
	})
	if err != nil {
		t.Fatalf("GenerateContent dashscope returned error: %v", err)
	}
	if !dashScopeResp.Success || dashScopeResp.Content != "openai text" {
		t.Fatalf("unexpected dashscope response: %#v", dashScopeResp)
	}

	geminiResp, err := service.GenerateContent(ctx, AIRequest{
		Prompt:       "prompt",
		Provider:     "unknown",
		GeminiAPIKey: "legacy-key",
		BaseURL:      server.URL,
	})
	if err != nil {
		t.Fatalf("GenerateContent gemini fallback returned error: %v", err)
	}
	if !geminiResp.Success || geminiResp.Content != "gemini text" || geminiResp.UsedAPIKey != "****" {
		t.Fatalf("unexpected gemini fallback response: %#v", geminiResp)
	}

	failedResp, err := service.GenerateContent(ctx, AIRequest{
		Prompt:   "prompt",
		Provider: "gemini",
		APIKey:   "key",
		BaseURL:  server.URL + "?fail=1",
	})
	if err != nil {
		t.Fatalf("provider status errors should be normalized into response: %v", err)
	}
	if failedResp.Success || failedResp.Error == "" {
		t.Fatalf("expected failed response, got %#v", failedResp)
	}

	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	if resp, err := service.GenerateContent(canceledCtx, AIRequest{
		Prompt:   "prompt",
		Provider: "openai",
		APIKey:   "key",
		BaseURL:  server.URL,
	}); err == nil || resp != nil {
		t.Fatalf("expected context cancellation error, resp=%#v err=%v", resp, err)
	}
}

func TestTrimCodeFence(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "plain text", input: " value ", want: "value"},
		{name: "json fence", input: "```json\n{\"ok\":true}\n```", want: "{\"ok\":true}"},
		{name: "generic fence", input: "```\nhello\n```", want: "hello"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := trimCodeFence(tt.input); got != tt.want {
				t.Fatalf("trimCodeFence() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestProviderTransportsDoSuccessAndErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/chat/completions"):
			if r.Header.Get("Authorization") != "Bearer test-key" {
				t.Fatalf("authorization header = %q", r.Header.Get("Authorization"))
			}
			if r.URL.Query().Get("fail") == "1" {
				http.Error(w, "openai down", http.StatusBadGateway)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte("{\"choices\":[{\"message\":{\"content\":\"```json\\n{\\\"ok\\\":\\\"openai\\\"}\\n```\"}}],\"usage\":{\"prompt_tokens\":11,\"completion_tokens\":22}}"))
		case strings.HasSuffix(r.URL.Path, "/messages"):
			if r.Header.Get("x-api-key") != "test-key" {
				t.Fatalf("x-api-key header = %q", r.Header.Get("x-api-key"))
			}
			if r.URL.Query().Get("fail") == "1" {
				http.Error(w, "claude down", http.StatusBadGateway)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"content":[{"type":"text","text":"hello "},{"type":"text","text":"claude"}],
				"usage":{"input_tokens":33,"output_tokens":44}
			}`))
		case strings.Contains(r.URL.Path, ":generateContent"):
			if r.URL.Query().Get("key") != "test-key" {
				t.Fatalf("gemini key query = %q", r.URL.RawQuery)
			}
			if r.URL.Query().Get("fail") == "1" {
				http.Error(w, "gemini down", http.StatusBadGateway)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte("{\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"```json\\n{\\\"ok\\\":\\\"gemini\\\"}\\n```\"}]},\"usageMetadata\":{\"promptTokenCount\":55,\"candidatesTokenCount\":66}}],\"usageMetadata\":{\"promptTokenCount\":77,\"candidatesTokenCount\":88}}"))
		default:
			t.Fatalf("unexpected path: %s", r.URL.String())
		}
	}))
	defer server.Close()

	ctx := context.Background()
	httpClient := server.Client()
	schema := map[string]interface{}{"type": "object"}

	openAIResult, err := openAITransport{}.Do(ctx, httpClient, chatRequest{
		Prompt: "prompt", SystemInstruction: "system", APIKey: "test-key",
		BaseURL: server.URL, JSONSchema: schema, UseWebSearch: true,
	})
	if err != nil {
		t.Fatalf("openAITransport.Do returned error: %v", err)
	}
	if openAIResult.Text != `{"ok":"openai"}` || openAIResult.InputTokens != 11 || openAIResult.OutputTokens != 22 {
		t.Fatalf("openAI result = %#v", openAIResult)
	}

	claudeResult, err := claudeTransport{}.Do(ctx, httpClient, chatRequest{
		Prompt: "prompt", SystemInstruction: "system", APIKey: "test-key",
		BaseURL: server.URL, JSONSchema: schema, UseWebSearch: true,
	})
	if err != nil {
		t.Fatalf("claudeTransport.Do returned error: %v", err)
	}
	if claudeResult.Text != "hello claude" || claudeResult.InputTokens != 33 || claudeResult.OutputTokens != 44 {
		t.Fatalf("claude result = %#v", claudeResult)
	}

	geminiResult, err := geminiTransport{}.Do(ctx, httpClient, chatRequest{
		Prompt: "prompt", SystemInstruction: "system", APIKey: "test-key",
		BaseURL: server.URL, JSONSchema: schema, UseWebSearch: true,
	})
	if err != nil {
		t.Fatalf("geminiTransport.Do returned error: %v", err)
	}
	if geminiResult.Text != `{"ok":"gemini"}` || geminiResult.InputTokens != 77 || geminiResult.OutputTokens != 88 {
		t.Fatalf("gemini result = %#v", geminiResult)
	}

	failServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "provider down", http.StatusBadGateway)
	}))
	defer failServer.Close()
	failClient := failServer.Client()

	if _, err := (openAITransport{}).Do(ctx, failClient, chatRequest{Prompt: "x", APIKey: "test-key", BaseURL: failServer.URL}); err == nil {
		t.Fatal("expected OpenAI status error")
	}
	if _, err := (claudeTransport{}).Do(ctx, failClient, chatRequest{Prompt: "x", APIKey: "test-key", BaseURL: failServer.URL}); err == nil {
		t.Fatal("expected Claude status error")
	}
	if _, err := (geminiTransport{}).Do(ctx, failClient, chatRequest{Prompt: "x", APIKey: "test-key", BaseURL: failServer.URL}); err == nil {
		t.Fatal("expected Gemini status error")
	}
}

func TestProviderTransportsRejectMalformedResponses(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/chat/completions"):
			_, _ = w.Write([]byte(`{"choices":[]}`))
		case strings.HasSuffix(r.URL.Path, "/messages"):
			_, _ = w.Write([]byte(`{"content":[]}`))
		case strings.Contains(r.URL.Path, ":generateContent"):
			_, _ = w.Write([]byte(`{"candidates":[]}`))
		default:
			t.Fatalf("unexpected path: %s", r.URL.String())
		}
	}))
	defer server.Close()

	ctx := context.Background()
	httpClient := server.Client()
	if _, err := (openAITransport{}).Do(ctx, httpClient, chatRequest{Prompt: "x", APIKey: "k", BaseURL: server.URL}); err == nil {
		t.Fatal("expected OpenAI empty choices error")
	}
	if _, err := (claudeTransport{}).Do(ctx, httpClient, chatRequest{Prompt: "x", APIKey: "k", BaseURL: server.URL}); err == nil {
		t.Fatal("expected Claude empty content error")
	}
	if _, err := (geminiTransport{}).Do(ctx, httpClient, chatRequest{Prompt: "x", APIKey: "k", BaseURL: server.URL}); err == nil {
		t.Fatal("expected Gemini empty candidates error")
	}
}
