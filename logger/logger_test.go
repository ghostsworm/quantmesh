package logger

import (
	"context"
	"testing"
	"time"
)

func TestLogLevelParsingAndString(t *testing.T) {
	tests := []struct {
		input string
		want  LogLevel
	}{
		{"debug", DEBUG},
		{" INFO ", INFO},
		{"warning", WARN},
		{"ERROR", ERROR},
		{"fatal", FATAL},
		{"invalid", INFO},
	}

	for _, tt := range tests {
		if got := ParseLogLevel(tt.input); got != tt.want {
			t.Fatalf("ParseLogLevel(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
	if got := LogLevel(99).String(); got != "UNKNOWN" {
		t.Fatalf("unknown level string = %q", got)
	}
}

func TestLanguageAndTranslateFallbacks(t *testing.T) {
	originalLang := GetLogLanguage()
	originalLevel := GetLevel()
	defer func() {
		SetLogLanguage(originalLang)
		SetTranslateFunc(nil)
		SetLevel(originalLevel)
		Close()
	}()

	SetLogLanguage("en-US")
	if got := GetLogLanguage(); got != "en-US" {
		t.Fatalf("log language = %q, want en-US", got)
	}
	SetLogLanguage("")
	if got := GetLogLanguage(); got != "en-US" {
		t.Fatalf("empty language should be ignored, got %q", got)
	}

	if got := Translate("plain.key"); got != "plain.key" {
		t.Fatalf("Translate without function = %q", got)
	}
	SetTranslateFunc(func(key string, data ...interface{}) string {
		if key == "hello" {
			return "你好"
		}
		return key
	})
	if got := Translate("hello"); got != "你好" {
		t.Fatalf("Translate with function = %q", got)
	}
	if got := Translate("missing"); got != "missing" {
		t.Fatalf("Translate fallback = %q", got)
	}

	SetLevel(WARN)
	if got := GetLevel(); got != WARN {
		t.Fatalf("level = %v, want WARN", got)
	}
}

func TestBotIDContextAndHooks(t *testing.T) {
	ctx := WithBotID(nil, " bot-1 ")
	if got := botIDFromContext(ctx); got != "bot-1" {
		t.Fatalf("bot id = %q, want bot-1", got)
	}
	if got := botIDFromContext(context.Background()); got != "" {
		t.Fatalf("empty bot id = %q", got)
	}

	ch := make(chan string, 1)
	SetErrorHook(func(level, message string) {
		ch <- level + ":" + message
	})
	defer SetErrorHook(nil)

	dispatchErrorHook(INFO, "ignored")
	select {
	case got := <-ch:
		t.Fatalf("INFO hook should not fire, got %q", got)
	default:
	}

	dispatchErrorHook(ERROR, "boom")
	select {
	case got := <-ch:
		if got != "ERROR:boom" {
			t.Fatalf("hook payload = %q", got)
		}
	case <-time.After(time.Second):
		t.Fatal("error hook did not fire")
	}
}
