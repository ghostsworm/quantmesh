package observability

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	SettingKeyPostHogProjectKey = "posthog_project_api_key"
	SettingKeyPostHogHost       = "posthog_host"
	SettingKeyPostHogEnabled    = "posthog_enabled"
	SettingKeySentryDSN         = "sentry_dsn"
	SettingKeySentryEnabled     = "sentry_enabled"
	SettingKeyEnvironment       = "observability_environment"

	DefaultPostHogHost = "https://us.i.posthog.com"
	DefaultEnvironment = "production"
)

type Config struct {
	PostHogProjectKey string
	PostHogHost       string
	PostHogEnabled    bool
	SentryDSN         string
	SentryEnabled     bool
	Environment       string
	Release           string
	DistinctID        string
}

type Event struct {
	Level   string
	Message string
	Topic   string
	Extra   string
}

var (
	mu      sync.RWMutex
	current Config
	client  = &http.Client{Timeout: 5 * time.Second}
)

func Reload(cfg Config) {
	cfg = normalizeConfig(cfg)
	mu.Lock()
	current = cfg
	mu.Unlock()
}

func Disable() {
	mu.Lock()
	current = Config{}
	mu.Unlock()
}

func IsEnabled() bool {
	mu.RLock()
	defer mu.RUnlock()
	return current.PostHogEnabled || current.SentryEnabled
}

func CurrentConfig() Config {
	mu.RLock()
	defer mu.RUnlock()
	c := current
	c.PostHogProjectKey = ""
	c.SentryDSN = ""
	return c
}

func ReportError(err error, topic string, extra string) {
	if err == nil {
		return
	}
	ReportEvent(Event{
		Level:   "ERROR",
		Message: err.Error(),
		Topic:   topic,
		Extra:   extra,
	})
}

func ReportMessage(level, message, topic string) {
	if strings.TrimSpace(message) == "" {
		return
	}
	ReportEvent(Event{
		Level:   level,
		Message: message,
		Topic:   topic,
	})
}

func ReportEvent(event Event) {
	cfg := snapshot()
	if !cfg.PostHogEnabled && !cfg.SentryEnabled {
		return
	}
	go func() {
		_ = sendEvent(cfg, event)
	}()
}

func TestPostHog(cfg Config) error {
	cfg = normalizeConfig(cfg)
	if !cfg.PostHogEnabled {
		cfg.PostHogEnabled = true
	}
	if cfg.PostHogProjectKey == "" {
		return errors.New("PostHog Project API Key 为空")
	}
	return sendPostHog(cfg, Event{
		Level:   "INFO",
		Message: "quantmesh posthog connectivity test",
		Topic:   "test",
	})
}

func TestSentry(cfg Config) error {
	cfg = normalizeConfig(cfg)
	if !cfg.SentryEnabled {
		cfg.SentryEnabled = true
	}
	if cfg.SentryDSN == "" {
		return errors.New("Sentry DSN 为空")
	}
	return sendSentry(cfg, Event{
		Level:   "INFO",
		Message: "quantmesh sentry connectivity test",
		Topic:   "test",
	})
}

func sendEvent(cfg Config, event Event) error {
	var errs []string
	if cfg.PostHogEnabled && cfg.PostHogProjectKey != "" {
		if err := sendPostHog(cfg, event); err != nil {
			errs = append(errs, "posthog: "+err.Error())
		}
	}
	if cfg.SentryEnabled && cfg.SentryDSN != "" {
		if err := sendSentry(cfg, event); err != nil {
			errs = append(errs, "sentry: "+err.Error())
		}
	}
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

func sendPostHog(cfg Config, event Event) error {
	endpoint := strings.TrimRight(cfg.PostHogHost, "/") + "/capture/"
	payload := map[string]interface{}{
		"api_key":     cfg.PostHogProjectKey,
		"event":       postHogEventName(event),
		"distinct_id": cfg.DistinctID,
		"timestamp":   time.Now().UTC().Format(time.RFC3339Nano),
		"properties": map[string]interface{}{
			"$process_person_profile": false,
			"source":                  "quantmesh",
			"level":                   event.Level,
			"topic":                   event.Topic,
			"message":                 truncate(event.Message, 8000),
			"extra":                   truncate(event.Extra, 8000),
			"environment":             cfg.Environment,
			"release":                 cfg.Release,
		},
	}
	return postJSON(endpoint, payload)
}

func sendSentry(cfg Config, event Event) error {
	parsed, err := parseSentryDSN(cfg.SentryDSN)
	if err != nil {
		return err
	}
	eventID := randomHex(16)
	payload := map[string]interface{}{
		"event_id":    eventID,
		"timestamp":   time.Now().UTC().Format(time.RFC3339Nano),
		"platform":    "go",
		"logger":      "quantmesh",
		"level":       sentryLevel(event.Level),
		"message":     truncate(event.Message, 8000),
		"environment": cfg.Environment,
		"release":     cfg.Release,
		"tags": map[string]string{
			"topic":  event.Topic,
			"source": "quantmesh",
		},
		"extra": map[string]string{
			"detail": truncate(event.Extra, 12000),
		},
	}
	var buf bytes.Buffer
	writeJSONLine(&buf, map[string]string{"event_id": eventID, "dsn": cfg.SentryDSN})
	writeJSONLine(&buf, map[string]string{"type": "event"})
	writeJSONLine(&buf, payload)

	req, err := http.NewRequest(http.MethodPost, parsed.EnvelopeURL, &buf)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-sentry-envelope")
	req.Header.Set("X-Sentry-Auth", parsed.AuthHeader)
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return nil
}

type sentryEndpoint struct {
	EnvelopeURL string
	AuthHeader  string
}

func parseSentryDSN(raw string) (sentryEndpoint, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return sentryEndpoint{}, err
	}
	publicKey := u.User.Username()
	if u.Scheme == "" || u.Host == "" || publicKey == "" {
		return sentryEndpoint{}, errors.New("无效的 Sentry DSN")
	}
	pathParts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(pathParts) == 0 || pathParts[len(pathParts)-1] == "" {
		return sentryEndpoint{}, errors.New("Sentry DSN 缺少项目 ID")
	}
	projectID := pathParts[len(pathParts)-1]
	prefixParts := pathParts[:len(pathParts)-1]
	basePath := ""
	if len(prefixParts) > 0 {
		basePath = "/" + strings.Join(prefixParts, "/")
	}
	envelope := fmt.Sprintf("%s://%s%s/api/%s/envelope/", u.Scheme, u.Host, basePath, projectID)
	auth := fmt.Sprintf("Sentry sentry_version=7, sentry_client=quantmesh-go/1.0, sentry_key=%s", publicKey)
	return sentryEndpoint{EnvelopeURL: envelope, AuthHeader: auth}, nil
}

func postJSON(endpoint string, payload interface{}) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return nil
}

func writeJSONLine(buf *bytes.Buffer, v interface{}) {
	data, _ := json.Marshal(v)
	buf.Write(data)
	buf.WriteByte('\n')
}

func snapshot() Config {
	mu.RLock()
	defer mu.RUnlock()
	return current
}

func normalizeConfig(cfg Config) Config {
	cfg.PostHogHost = strings.TrimRight(strings.TrimSpace(cfg.PostHogHost), "/")
	if cfg.PostHogHost == "" {
		cfg.PostHogHost = DefaultPostHogHost
	}
	cfg.PostHogProjectKey = strings.TrimSpace(cfg.PostHogProjectKey)
	cfg.SentryDSN = strings.TrimSpace(cfg.SentryDSN)
	cfg.Environment = strings.TrimSpace(cfg.Environment)
	if cfg.Environment == "" {
		cfg.Environment = DefaultEnvironment
	}
	cfg.DistinctID = strings.TrimSpace(cfg.DistinctID)
	if cfg.DistinctID == "" {
		cfg.DistinctID = "quantmesh-server"
	}
	return cfg
}

func postHogEventName(event Event) string {
	if strings.TrimSpace(event.Topic) == "" {
		return "quantmesh_log"
	}
	return "quantmesh_" + strings.TrimSpace(event.Topic)
}

func sentryLevel(level string) string {
	switch strings.ToUpper(strings.TrimSpace(level)) {
	case "FATAL", "ERROR":
		return "error"
	case "WARN", "WARNING":
		return "warning"
	case "INFO":
		return "info"
	default:
		return "error"
	}
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "... [truncated]"
}

func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return strings.ReplaceAll(time.Now().UTC().Format("20060102150405.000000000"), ".", "")
	}
	return hex.EncodeToString(b)
}
