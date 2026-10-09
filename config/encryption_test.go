package config

import (
	"strings"
	"testing"
)

func TestEncryptAPIKeyUsesLosslessVersionedFormat(t *testing.T) {
	key := []byte("01234567890123456789012345678901")
	plaintext := "exchange-secret-with-lowercase-xy_-/and=padding"
	ciphertext, err := EncryptAPIKey(plaintext, key)
	if err != nil {
		t.Fatalf("EncryptAPIKey: %v", err)
	}
	if !strings.HasPrefix(ciphertext, VersionedEncryptionPrefix) || !IsEncrypted(ciphertext) {
		t.Fatalf("ciphertext lacks version marker: %q", ciphertext)
	}
	got, err := DecryptAPIKey(ciphertext, key)
	if err != nil || got != plaintext {
		t.Fatalf("DecryptAPIKey = %q, %v; want exact round trip", got, err)
	}
	if _, err := DecryptAPIKey(ciphertext, []byte("different-key-012345678901234567")); err == nil {
		t.Fatal("DecryptAPIKey accepted a different master key")
	}
}

func TestRealProviderKeyPrefixIsNotMisclassifiedAsCiphertext(t *testing.T) {
	const providerKey = "AKIA1234567890123456"
	if IsEncrypted(providerKey) {
		t.Fatal("normal provider key ID was classified as encrypted data")
	}
	got, err := DecryptAPIKey(providerKey, nil)
	if err != nil || got != providerKey {
		t.Fatalf("DecryptAPIKey(%q) = %q, %v; want unchanged provider key", providerKey, got, err)
	}
}
