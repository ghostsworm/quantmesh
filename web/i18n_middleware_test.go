package web

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	qmi18n "quantmesh/i18n"
)

func TestParseAcceptLanguage(t *testing.T) {
	tests := []struct {
		name   string
		header string
		want   string
	}{
		{name: "empty defaults zh cn", header: "", want: "zh-CN"},
		{name: "keeps highest priority", header: "en-US,en;q=0.8,zh-CN;q=0.6", want: "en-US"},
		{name: "strips weight", header: "zh-TW;q=0.9,en;q=0.8", want: "zh-TW"},
		{name: "normalizes underscore", header: "zh_cn,zh;q=0.9", want: "zh-CN"},
		{name: "unknown defaults zh cn", header: "xx-YY", want: "zh-CN"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseAcceptLanguage(tt.header); got != tt.want {
				t.Fatalf("parseAcceptLanguage(%q) = %q, want %q", tt.header, got, tt.want)
			}
		})
	}
}

func TestNormalizeLanguageSupportedLocales(t *testing.T) {
	tests := map[string]string{
		"fr-ca":   "fr-FR",
		"es-MX":   "es-ES",
		"ru":      "ru-RU",
		"hi-IN":   "hi-IN",
		"pt-PT":   "pt-BR",
		"de":      "de-DE",
		"ko-KR":   "ko-KR",
		"ar-EG":   "ar-SA",
		"tr-TR":   "tr-TR",
		"vi":      "vi-VN",
		"it-IT":   "it-IT",
		"id-ID":   "id-ID",
		"nl-NL":   "nl-NL",
		"zh-Hant": "zh-TW",
	}

	for input, want := range tests {
		t.Run(input, func(t *testing.T) {
			if got := normalizeLanguage(input); got != want {
				t.Fatalf("normalizeLanguage(%q) = %q, want %q", input, got, want)
			}
		})
	}
}

func TestI18nMiddlewareSetsLanguageAndLocalizer(t *testing.T) {
	gin.SetMode(gin.TestMode)
	if err := qmi18n.Init("zh-CN"); err != nil {
		t.Fatalf("failed to init i18n: %v", err)
	}

	router := gin.New()
	router.Use(I18nMiddleware())
	router.GET("/probe", func(c *gin.Context) {
		if got := GetLanguage(c); got != "en-US" {
			t.Fatalf("GetLanguage() = %q, want en-US", got)
		}
		if localizer := GetLocalizer(c); localizer == nil {
			t.Fatal("expected localizer to be set")
		}
		c.Status(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodGet, "/probe", nil)
	req.Header.Set("Accept-Language", "en-US,en;q=0.8")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusNoContent)
	}
}

func TestGetLanguageAndLocalizerFallbacks(t *testing.T) {
	gin.SetMode(gin.TestMode)
	if err := qmi18n.Init("zh-CN"); err != nil {
		t.Fatalf("failed to init i18n: %v", err)
	}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())

	if got := GetLanguage(c); got != "zh-CN" {
		t.Fatalf("GetLanguage fallback = %q, want zh-CN", got)
	}
	if got := GetLocalizer(c); got == nil {
		t.Fatal("expected fallback localizer")
	}

	c.Set("language", 123)
	c.Set("localizer", 123)
	if got := GetLanguage(c); got != "zh-CN" {
		t.Fatalf("GetLanguage invalid type fallback = %q, want zh-CN", got)
	}
	if got := GetLocalizer(c); got == nil {
		t.Fatal("expected invalid localizer type to fall back")
	}
}
