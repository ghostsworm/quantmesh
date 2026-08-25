package providers

import (
	"context"
	"strings"
	"testing"

	"quantmesh/ai/service"
)

func TestQueueProviderDefaultsAndMissingTaskService(t *testing.T) {
	origGetter := getGlobalTaskService
	t.Cleanup(func() { SetGlobalTaskServiceGetter(origGetter) })
	SetGlobalTaskServiceGetter(func() *service.TaskService { return nil })

	provider := NewQueueProvider("", "api-key", "model-a", "https://example.test")
	if provider.provider != "gemini" || provider.apiKey != "api-key" || provider.model != "model-a" || provider.baseURL == "" {
		t.Fatalf("unexpected provider defaults: %+v", provider)
	}

	text, err := provider.GenerateContent(context.Background(), "hello", map[string]interface{}{"type": "object"}, true)
	if err == nil || text != "" {
		t.Fatalf("GenerateContent without task service = %q/%v", text, err)
	}
	if !strings.Contains(err.Error(), "AI 任務服務未初始化") {
		t.Fatalf("unexpected error: %v", err)
	}

}
