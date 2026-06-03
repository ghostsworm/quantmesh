package i18n

import "testing"

func TestInitAndTranslateFallback(t *testing.T) {
	if err := Init("en-US"); err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	if got := GetSystemLanguage(); got != "en-US" {
		t.Fatalf("system language = %q, want en-US", got)
	}
	if localizer := GetLocalizer(""); localizer == nil {
		t.Fatal("expected default localizer after Init")
	}
	if got := TWithLang("en-US", "missing.translation.key"); got != "missing.translation.key" {
		t.Fatalf("missing translation fallback = %q", got)
	}

	SetSystemLanguage("zh-CN")
	if got := GetSystemLanguage(); got != "zh-CN" {
		t.Fatalf("system language = %q, want zh-CN", got)
	}
}

func TestInitUsesDefaultLanguageWhenBlank(t *testing.T) {
	if err := Init(""); err != nil {
		t.Fatalf("Init blank language error = %v", err)
	}
	if got := GetSystemLanguage(); got != "zh-CN" {
		t.Fatalf("blank Init language = %q, want zh-CN", got)
	}
}
