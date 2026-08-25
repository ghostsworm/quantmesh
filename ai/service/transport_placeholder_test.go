package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestValidateResolvedBaseURL 佔位符攔截的單元覆蓋
func TestValidateResolvedBaseURL(t *testing.T) {
	tests := []struct {
		name    string
		baseURL string
		wantErr bool
	}{
		{"DashScope 新加坡預設端點未替換", "https://{WorkspaceId}.ap-southeast-1.maas.aliyuncs.com/compatible-mode/v1", true},
		{"照抄文件只替換了一半", "https://{WorkspaceId}.example.com/v1", true},
		{"底線命名的佔位符", "https://{work_space_id}.example.com/v1", true},
		{"已替換為實際值", "https://ws-abc123.ap-southeast-1.maas.aliyuncs.com/compatible-mode/v1", false},
		{"普通端點", "https://api.openai.com/v1", false},
		{"DeepSeek", "https://api.deepseek.com", false},
		{"空字串", "", false},
		// 查詢字串裡的大括號不常見，但不應誤傷正常 URL
		{"路徑含大括號以外的符號", "https://api.example.com/v1?a=b&c=d", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateResolvedBaseURL(tt.baseURL)
			if tt.wantErr && err == nil {
				t.Fatalf("validateResolvedBaseURL(%q) 應該報錯但返回 nil", tt.baseURL)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("validateResolvedBaseURL(%q) 不應報錯，實得 %v", tt.baseURL, err)
			}
			if tt.wantErr && !strings.Contains(err.Error(), "佔位符") {
				t.Errorf("錯誤訊息應點明佔位符，實得 %v", err)
			}
		})
	}
}

// TestOpenAITransport_RejectsUnsubstitutedPlaceholder 回歸測試：
// 修復前 provider=dashscope_sg 且 base_url 為空時，會落到帶 {WorkspaceId}
// 的預設端點並真的發起請求，最終只拋出一句看不出根因的 DNS 錯誤。
func TestOpenAITransport_RejectsUnsubstitutedPlaceholder(t *testing.T) {
	cases := []struct {
		name     string
		provider string
		baseURL  string
	}{
		{"base_url 為空落到帶佔位符的預設值", "dashscope_sg", ""},
		{"使用者照抄文件未替換", "dashscope_sg", "https://{WorkspaceId}.ap-southeast-1.maas.aliyuncs.com/compatible-mode/v1"},
		{"其他 provider 填了帶佔位符的 base_url", "openai", "https://{Region}.example.com/v1"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := openAITransport{}.Do(context.Background(), http.DefaultClient, chatRequest{
				Prompt:   "hi",
				Provider: c.provider,
				APIKey:   "test-key",
				Model:    "qwen-plus",
				BaseURL:  c.baseURL,
			})
			if err == nil {
				t.Fatal("應該在發請求前就攔截，實際卻返回 nil error")
			}
			if !strings.Contains(err.Error(), "佔位符") {
				t.Errorf("錯誤訊息應點明佔位符以便定位，實得 %v", err)
			}
		})
	}
}

// TestOpenAITransport_AcceptsSubstitutedBaseURL 替換後的 base_url 必須照常放行，
// 確保攔截沒有誤傷正常路徑。
func TestOpenAITransport_AcceptsSubstitutedBaseURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}],"usage":{"prompt_tokens":1,"completion_tokens":2}}`))
	}))
	defer server.Close()

	got, err := openAITransport{}.Do(context.Background(), server.Client(), chatRequest{
		Prompt:   "hi",
		Provider: "dashscope_sg",
		APIKey:   "test-key",
		Model:    "qwen-plus",
		BaseURL:  server.URL,
	})
	if err != nil {
		t.Fatalf("已替換佔位符的 base_url 不應被攔截: %v", err)
	}
	if got.Text != "ok" {
		t.Errorf("回應解析錯誤，實得 %q", got.Text)
	}
}
