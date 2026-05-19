package web

import (
	"encoding/base64"
	"encoding/json"
	"testing"

	"github.com/go-webauthn/webauthn/webauthn"
)

func TestWebAuthnCredentialRoundTrip(t *testing.T) {
	manager, err := NewWebAuthnManager(nil, t.TempDir(), "localhost", "http://localhost")
	if err != nil {
		t.Fatalf("NewWebAuthnManager() error = %v", err)
	}
	defer manager.Close()

	credential := &webauthn.Credential{
		ID:              []byte("credential-id"),
		PublicKey:       []byte("credential-public-key"),
		AttestationType: "none",
		Authenticator: webauthn.Authenticator{
			SignCount: 7,
		},
	}

	if err := manager.SaveCredential("admin", "admin", credential, "Touch ID"); err != nil {
		t.Fatalf("SaveCredential() error = %v", err)
	}

	user, err := manager.GetUser("admin")
	if err != nil {
		t.Fatalf("GetUser() error = %v", err)
	}
	credentials := user.WebAuthnCredentials()
	if len(credentials) != 1 {
		t.Fatalf("WebAuthnCredentials() len = %d, want 1", len(credentials))
	}
	if got := string(credentials[0].PublicKey); got != "credential-public-key" {
		t.Fatalf("PublicKey = %q, want %q", got, "credential-public-key")
	}
	if got := credentials[0].Authenticator.SignCount; got != 7 {
		t.Fatalf("SignCount = %d, want 7", got)
	}
}

func TestWebAuthnLegacyPublicKeyStorage(t *testing.T) {
	manager, err := NewWebAuthnManager(nil, t.TempDir(), "localhost", "http://localhost")
	if err != nil {
		t.Fatalf("NewWebAuthnManager() error = %v", err)
	}
	defer manager.Close()

	credentialID := base64.RawURLEncoding.EncodeToString([]byte("legacy-credential"))
	legacyPublicKey, err := json.Marshal([]byte("legacy-public-key"))
	if err != nil {
		t.Fatalf("Marshal legacy public key error = %v", err)
	}

	_, err = manager.db.Exec(`
		INSERT INTO webauthn_credentials (id, user_id, username, credential_id, public_key, counter, device_name)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, credentialID, "admin", "admin", credentialID, string(legacyPublicKey), 3, "Legacy")
	if err != nil {
		t.Fatalf("insert legacy credential error = %v", err)
	}

	user, err := manager.GetUser("admin")
	if err != nil {
		t.Fatalf("GetUser() error = %v", err)
	}
	credentials := user.WebAuthnCredentials()
	if len(credentials) != 1 {
		t.Fatalf("WebAuthnCredentials() len = %d, want 1", len(credentials))
	}
	if got := string(credentials[0].PublicKey); got != "legacy-public-key" {
		t.Fatalf("PublicKey = %q, want %q", got, "legacy-public-key")
	}
	if got := credentials[0].Authenticator.SignCount; got != 3 {
		t.Fatalf("SignCount = %d, want 3", got)
	}
}
