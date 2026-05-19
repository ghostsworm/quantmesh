package service

import "testing"

func TestRedactSensitiveRequestDataKeepsGeminiKeyOutOfStoredPayload(t *testing.T) {
	taskID := "task-1"
	input := map[string]interface{}{
		"prompt":         "analyze market",
		"gemini_api_key": "secret-key",
	}

	stored := redactSensitiveRequestData(taskID, input)
	t.Cleanup(func() { ForgetTaskSecrets(taskID) })

	if stored["gemini_api_key"] != RedactedAPIKey {
		t.Fatalf("stored api key was not redacted: %+v", stored)
	}
	if input["gemini_api_key"] != "secret-key" {
		t.Fatalf("input request data should not be mutated: %+v", input)
	}
	if got := ResolveTaskGeminiAPIKey(taskID); got != "secret-key" {
		t.Fatalf("unexpected resolved key: %q", got)
	}

	ForgetTaskSecrets(taskID)
	if got := ResolveTaskGeminiAPIKey(taskID); got != "" {
		t.Fatalf("secret was not forgotten: %q", got)
	}
}
