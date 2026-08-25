package ai

import (
	"strings"
	"testing"
)

// TestNewAIClient_RequiresBaseURL 需要顯式 base_url 的 provider。
// dashscope_sg 的預設端點含 {WorkspaceId} 佔位符，天生不可直接使用，
// 修復前會一路構造成功、直到第一次請求才拋出看不出根因的 DNS 錯誤。
func TestNewAIClient_RequiresBaseURL(t *testing.T) {
	needBaseURL := []string{"poe", "openai_compatible", "custom", "dashscope_sg"}

	for _, provider := range needBaseURL {
		t.Run(provider+" 缺 base_url 應報錯", func(t *testing.T) {
			client, err := NewAIClient(provider, "some-model", "test-key", "")
			if err == nil {
				t.Fatalf("NewAIClient(%q) 缺 base_url 應報錯，實際返回 client=%v", provider, client)
			}
			if !strings.Contains(err.Error(), "base_url") {
				t.Errorf("錯誤訊息應點名 base_url，實得 %v", err)
			}
		})

		t.Run(provider+" 提供 base_url 應成功", func(t *testing.T) {
			client, err := NewAIClient(provider, "some-model", "test-key", "https://relay.example.com/v1")
			if err != nil {
				t.Fatalf("NewAIClient(%q) 已提供 base_url 不應報錯: %v", provider, err)
			}
			if client == nil {
				t.Fatal("應返回非 nil client")
			}
		})
	}
}

// TestNewAIClient_DefaultEndpointProviders 自帶可用預設端點的 provider
// 不應被要求填 base_url。
func TestNewAIClient_DefaultEndpointProviders(t *testing.T) {
	selfServing := []string{"gemini", "openai", "claude", "anthropic", "dashscope", "kimi", "kimi_intl", "deepseek"}

	for _, provider := range selfServing {
		t.Run(provider, func(t *testing.T) {
			client, err := NewAIClient(provider, "some-model", "test-key", "")
			if err != nil {
				t.Fatalf("NewAIClient(%q) 不應要求 base_url: %v", provider, err)
			}
			if client == nil {
				t.Fatal("應返回非 nil client")
			}
		})
	}
}

// TestNewAIClient_RejectsEmptyAPIKey API Key 缺失時應直接拒絕
func TestNewAIClient_RejectsEmptyAPIKey(t *testing.T) {
	if _, err := NewAIClient("openai", "gpt-4o-mini", "", ""); err == nil {
		t.Fatal("缺 API Key 應報錯")
	}
}
