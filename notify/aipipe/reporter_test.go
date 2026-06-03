package aipipe

import (
	"errors"
	"testing"
)

func TestReloadDisableAndCurrentConfig(t *testing.T) {
	Disable()
	t.Cleanup(Disable)

	if IsEnabled() {
		t.Fatalf("IsEnabled() = true before reload")
	}
	if err := Reload(Config{Enabled: true}); err != nil {
		t.Fatalf("Reload(enabled without key) error = %v", err)
	}
	if IsEnabled() {
		t.Fatalf("IsEnabled() = true without API key")
	}

	currentCf = Config{
		APIKey:    "secret-key",
		Endpoint:  "https://example.test/api",
		Enabled:   true,
		AgentName: "unit-agent",
	}
	cfg := CurrentConfig()
	if cfg.APIKey != "" {
		t.Fatalf("CurrentConfig().APIKey = %q, want redacted", cfg.APIKey)
	}
	if cfg.Endpoint != "https://example.test/api" || !cfg.Enabled || cfg.AgentName != "unit-agent" {
		t.Fatalf("CurrentConfig() = %+v, want non-secret fields preserved", cfg)
	}

	Disable()
	if got := CurrentConfig(); got != (Config{}) {
		t.Fatalf("CurrentConfig() after Disable = %+v, want zero config", got)
	}
	if err := Close(); err != nil {
		t.Fatalf("Close() without client error = %v", err)
	}
}

func TestReportNoopPathsAndSuppress(t *testing.T) {
	Disable()
	t.Cleanup(Disable)

	ReportError(nil, "topic", "extra")
	ReportError(errors.New("disabled"), "topic", "extra")
	ReportMessage("ERROR", "", "log")
	ReportMessage("ERROR", "aipipe: internal", "log")
	ReportMessage("ERROR", "[aipipe] internal", "log")

	called := false
	WithSuppress(func() {
		called = true
		if !suppress.Load() {
			t.Fatalf("suppress flag = false inside WithSuppress")
		}
		ReportError(errors.New("suppressed"), "topic", "")
	})
	if !called {
		t.Fatalf("WithSuppress did not call callback")
	}
	if suppress.Load() {
		t.Fatalf("suppress flag = true after WithSuppress")
	}
}

func TestPanicGuardNoRethrowAndNormalizeEndpoint(t *testing.T) {
	Disable()
	t.Cleanup(Disable)

	func() {
		defer PanicGuardNoRethrow("background-worker")
		panic("boom")
	}()

	if got := normalizeEndpoint(""); got != DefaultEndpoint {
		t.Fatalf("normalizeEndpoint(empty) = %q, want %q", got, DefaultEndpoint)
	}
	if got := normalizeEndpoint(" https://example.test/api/// "); got != "https://example.test/api" {
		t.Fatalf("normalizeEndpoint(trim) = %q", got)
	}

	currentCf = Config{APIKey: "k", Endpoint: "https://example.test", Enabled: true, AgentName: "a"}
	if !sameConfigLocked(Config{APIKey: "k", Endpoint: "https://example.test", Enabled: true, AgentName: "a"}) {
		t.Fatalf("sameConfigLocked() = false for same config")
	}
	if sameConfigLocked(Config{APIKey: "other", Endpoint: "https://example.test", Enabled: true, AgentName: "a"}) {
		t.Fatalf("sameConfigLocked() = true for different API key")
	}
}
