package service

import (
	"net/http"
	"net/url"
	"testing"
)

func TestAIRequestAPIKeyPrefersGenericKey(t *testing.T) {
	t.Parallel()

	req := AIRequest{APIKey: "  generic-key  ", GeminiAPIKey: "gemini-key"}
	if got := req.apiKey(); got != "  generic-key  " {
		t.Fatalf("apiKey() = %q, want generic key", got)
	}

	req = AIRequest{APIKey: "   ", GeminiAPIKey: "gemini-key"}
	if got := req.apiKey(); got != "gemini-key" {
		t.Fatalf("apiKey() = %q, want Gemini fallback", got)
	}
}

func TestAIServiceProviderLabelsAndMasking(t *testing.T) {
	t.Parallel()

	modelCases := map[string]string{
		"openai":    openAIDefaultModel,
		"poe":       openAIDefaultModel,
		"claude":    claudeDefaultModel,
		"anthropic": claudeDefaultModel,
		"gemini":    geminiDefaultModel,
		"unknown":   geminiDefaultModel,
	}
	for provider, want := range modelCases {
		if got := defaultModelForProvider(provider); got != want {
			t.Fatalf("defaultModelForProvider(%q) = %q, want %q", provider, got, want)
		}
	}

	if got := usageModelLabel("gemini", "gemini-2.5-pro"); got != "gemini-2.5-pro" {
		t.Fatalf("gemini usage label = %q", got)
	}
	if got := usageModelLabel("", "gemini-2.5-flash"); got != "gemini-2.5-flash" {
		t.Fatalf("empty provider usage label = %q", got)
	}
	if got := usageModelLabel("openai", "gpt-5"); got != "openai:gpt-5" {
		t.Fatalf("openai usage label = %q", got)
	}

	if got := maskAPIKey("short"); got != "****" {
		t.Fatalf("short key mask = %q", got)
	}
	if got := maskAPIKey("abcdefghijklmnop"); got != "abcdef....mnop" {
		t.Fatalf("long key mask = %q", got)
	}
}

func TestNewAIServiceUsesProxyEnvironment(t *testing.T) {
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:18080")
	t.Setenv("https_proxy", "")
	t.Setenv("HTTP_PROXY", "")
	t.Setenv("http_proxy", "")

	svc := NewAIService()
	if svc == nil || svc.httpClient == nil {
		t.Fatal("NewAIService returned nil client")
	}
	transport, ok := svc.httpClient.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport type = %T, want *http.Transport", svc.httpClient.Transport)
	}
	if transport.Proxy == nil {
		t.Fatal("expected proxy function to be configured")
	}
	reqURL, _ := url.Parse("https://example.test")
	req := &http.Request{URL: reqURL}
	proxyURL, err := transport.Proxy(req)
	if err != nil {
		t.Fatalf("proxy function returned error: %v", err)
	}
	if proxyURL == nil || proxyURL.String() != "http://127.0.0.1:18080" {
		t.Fatalf("proxy URL = %v", proxyURL)
	}
}

func TestNewAIServiceIgnoresInvalidProxyEnvironment(t *testing.T) {
	t.Setenv("HTTPS_PROXY", "://bad-url")
	t.Setenv("https_proxy", "")
	t.Setenv("HTTP_PROXY", "")
	t.Setenv("http_proxy", "")

	svc := NewAIService()
	transport, ok := svc.httpClient.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport type = %T, want *http.Transport", svc.httpClient.Transport)
	}
	if transport.Proxy != nil {
		t.Fatal("invalid proxy should not configure proxy function")
	}
}
