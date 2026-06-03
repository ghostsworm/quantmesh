package service

import (
	"fmt"
	"testing"
)

func TestResolveTransport(t *testing.T) {
	tests := []struct {
		provider string
		wantType string
	}{
		{provider: "openai", wantType: "service.openAITransport"},
		{provider: "poe", wantType: "service.openAITransport"},
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
