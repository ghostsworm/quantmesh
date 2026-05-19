package web

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestNormalizeWebAuthnResponseConvertsAssertionBinaryFields(t *testing.T) {
	normalized := normalizeWebAuthnResponse(map[string]interface{}{
		"id":    "credential",
		"rawId": []interface{}{float64(1), float64(2), float64(3)},
		"response": map[string]interface{}{
			"authenticatorData": []interface{}{float64(4), float64(5)},
			"clientDataJSON":    []interface{}{float64(6), float64(7)},
			"signature":         []interface{}{float64(8), float64(9)},
			"userHandle":        []interface{}{float64(10)},
		},
		"type": "public-key",
	})
	if normalized == nil {
		t.Fatal("normalizeWebAuthnResponse() returned nil")
	}

	if got := normalized["rawId"]; got != base64.RawURLEncoding.EncodeToString([]byte{1, 2, 3}) {
		t.Fatalf("rawId = %v", got)
	}

	response := normalized["response"].(map[string]interface{})
	for field, want := range map[string][]byte{
		"authenticatorData": {4, 5},
		"clientDataJSON":    {6, 7},
		"signature":         {8, 9},
		"userHandle":        {10},
	} {
		if got := response[field]; got != base64.RawURLEncoding.EncodeToString(want) {
			t.Fatalf("%s = %v", field, got)
		}
	}
}

func TestNormalizeWebAuthnResponseRejectsInvalidBinaryArray(t *testing.T) {
	normalized := normalizeWebAuthnResponse(map[string]interface{}{
		"rawId": []interface{}{float64(256)},
	})
	if normalized != nil {
		t.Fatalf("normalizeWebAuthnResponse() = %#v, want nil", normalized)
	}
}

func TestNewWebAuthnSessionKeyIsRandom(t *testing.T) {
	first, err := newWebAuthnSessionKey("webauthn_login", "admin")
	if err != nil {
		t.Fatalf("newWebAuthnSessionKey() error = %v", err)
	}
	second, err := newWebAuthnSessionKey("webauthn_login", "admin")
	if err != nil {
		t.Fatalf("newWebAuthnSessionKey() second error = %v", err)
	}

	if first == second {
		t.Fatal("session keys should be unique")
	}
	if !strings.HasPrefix(first, "webauthn_login_admin_") {
		t.Fatalf("session key prefix = %q", first)
	}
}
