package observability

import "testing"

func TestParseSentryDSNBuildsEnvelopeURL(t *testing.T) {
	got, err := parseSentryDSN("https://public@example.com/123")
	if err != nil {
		t.Fatalf("parseSentryDSN returned error: %v", err)
	}
	if got.EnvelopeURL != "https://example.com/api/123/envelope/" {
		t.Fatalf("unexpected envelope URL: %s", got.EnvelopeURL)
	}
	if got.AuthHeader != "Sentry sentry_version=7, sentry_client=quantmesh-go/1.0, sentry_key=public" {
		t.Fatalf("unexpected auth header: %s", got.AuthHeader)
	}
}

func TestParseSentryDSNPreservesPathPrefix(t *testing.T) {
	got, err := parseSentryDSN("https://public@example.com/sentry/456")
	if err != nil {
		t.Fatalf("parseSentryDSN returned error: %v", err)
	}
	if got.EnvelopeURL != "https://example.com/sentry/api/456/envelope/" {
		t.Fatalf("unexpected envelope URL: %s", got.EnvelopeURL)
	}
}

func TestNormalizeConfigDefaults(t *testing.T) {
	got := normalizeConfig(Config{})
	if got.PostHogHost != DefaultPostHogHost {
		t.Fatalf("unexpected PostHog host: %s", got.PostHogHost)
	}
	if got.Environment != DefaultEnvironment {
		t.Fatalf("unexpected environment: %s", got.Environment)
	}
	if got.DistinctID != "quantmesh-server" {
		t.Fatalf("unexpected distinct id: %s", got.DistinctID)
	}
}
