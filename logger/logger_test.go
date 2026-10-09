package logger

import (
	"bytes"
	"context"
	"log"
	"strings"
	"testing"
	"time"
)

func TestRedactSensitiveTextMasksCommonCredentialForms(t *testing.T) {
	message := `request https://api.example.test/order?symbol=BTCUSDT&signature=signed-value&timestamp=1234 X-MBX-APIKEY: header-key Authorization: Bearer bearer-token Authorization: Basic basic-token {"apiKey":"json-key","password":"json-password"} https://user:pass@example.test/path`
	got := redactSensitiveText(message)
	for _, secret := range []string{"signed-value", "header-key", "bearer-token", "basic-token", "json-key", "json-password", "user:pass"} {
		if strings.Contains(got, secret) {
			t.Fatalf("redaction retained %q in %q", secret, got)
		}
	}
	for _, safeContext := range []string{"BTCUSDT", "timestamp=1234", "[REDACTED]"} {
		if !strings.Contains(got, safeContext) {
			t.Fatalf("redaction removed safe context %q from %q", safeContext, got)
		}
	}
}

func TestRedactSensitiveTextMasksCompleteCookieHeadersAndProxyAuthorization(t *testing.T) {
	tests := []struct {
		name    string
		message string
		secrets []string
	}{
		{name: "cookie header", message: "request Cookie: session=cookie-session; csrf=cookie-csrf", secrets: []string{"cookie-session", "cookie-csrf"}},
		{name: "set-cookie header", message: "response Set-Cookie: refresh=cookie-refresh; HttpOnly; SameSite=Strict", secrets: []string{"cookie-refresh"}},
		{name: "structured cookie field", message: "{\"cookie\":\"session=cookie-json-session;csrf=cookie-json-csrf\",\"trace\":\"safe-context\"}", secrets: []string{"cookie-json-session", "cookie-json-csrf"}},
		{name: "proxy authorization", message: "Proxy-Authorization: Basic proxy-credential", secrets: []string{"proxy-credential"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := redactSensitiveText(test.message)
			for _, secret := range test.secrets {
				if strings.Contains(got, secret) {
					t.Fatalf("redaction retained %q in %q", secret, got)
				}
			}
		})
	}
}

func TestInstallStandardLogRedactionSanitizesStandardLogger(t *testing.T) {
	var output bytes.Buffer
	originalWriter := log.Writer()
	originalFlags := log.Flags()
	log.SetOutput(&output)
	log.SetFlags(0)
	t.Cleanup(func() {
		log.SetOutput(originalWriter)
		log.SetFlags(originalFlags)
	})

	InstallStandardLogRedaction()
	log.Printf("raw account response: %s", `{"apiSecret":"standard-log-secret","symbol":"BTCUSDT"}`)

	message := output.String()
	if strings.Contains(message, "standard-log-secret") || !strings.Contains(message, "[REDACTED]") {
		t.Fatalf("standard logger output was not sanitized: %q", message)
	}
	if !strings.Contains(message, "BTCUSDT") {
		t.Fatalf("standard logger redaction removed safe diagnostic context: %q", message)
	}
}

func TestLogRedactsBeforeConsoleStorageAndErrorHook(t *testing.T) {
	var output bytes.Buffer
	originalWriter := log.Writer()
	originalFlags := log.Flags()
	log.SetOutput(&output)
	log.SetFlags(0)
	t.Cleanup(func() {
		log.SetOutput(originalWriter)
		log.SetFlags(originalFlags)
		InitLogStorage(nil)
		SetErrorHook(nil)
	})

	stored := make(chan string, 1)
	hooked := make(chan string, 1)
	InitLogStorage(func(_, message, _ string) { stored <- message })
	SetErrorHook(func(_, message string) { hooked <- message })
	Error("exchange response: %s", `{"apiSecret":"storage-secret"}`)

	for name, channel := range map[string]<-chan string{"storage": stored, "error hook": hooked} {
		select {
		case message := <-channel:
			if strings.Contains(message, "storage-secret") || !strings.Contains(message, "[REDACTED]") {
				t.Fatalf("%s received unsanitized message: %q", name, message)
			}
		case <-time.After(time.Second):
			t.Fatalf("%s did not receive log message", name)
		}
	}
	if strings.Contains(output.String(), "storage-secret") || !strings.Contains(output.String(), "[REDACTED]") {
		t.Fatalf("console received unsanitized message: %q", output.String())
	}

	Warnln("signed query", "?signature=println-secret")
	select {
	case message := <-stored:
		if strings.Contains(message, "println-secret") || !strings.Contains(message, "[REDACTED]") {
			t.Fatalf("storage received unsanitized Println message: %q", message)
		}
	case <-time.After(time.Second):
		t.Fatal("storage did not receive Println message")
	}
	if strings.Contains(output.String(), "println-secret") {
		t.Fatalf("console received unsanitized Println message: %q", output.String())
	}
}

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
