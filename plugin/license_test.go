package plugin

import (
	"path/filepath"
	"testing"
	"time"
)

func TestGenerateParseAndValidateLicense(t *testing.T) {
	expiry := time.Now().Add(24 * time.Hour)
	licenseKey, err := GenerateLicense("premium-grid", "customer-1", expiry, 3, []string{"grid", "ai"}, "", getSecretKey())
	if err != nil {
		t.Fatalf("GenerateLicense failed: %v", err)
	}

	info, err := ParseLicense(licenseKey)
	if err != nil {
		t.Fatalf("ParseLicense failed: %v", err)
	}
	if info.PluginName != "premium-grid" {
		t.Fatalf("PluginName = %q, want premium-grid", info.PluginName)
	}
	if !verifySignature(info) {
		t.Fatal("expected generated license signature to verify")
	}

	store := &LicenseStore{
		licenses: make(map[string]*LicenseInfo),
		filePath: filepath.Join(t.TempDir(), "licenses.enc"),
	}
	if err := store.Store("premium-grid", licenseKey); err != nil {
		t.Fatalf("Store failed: %v", err)
	}
	if err := store.Validate("premium-grid"); err != nil {
		t.Fatalf("Validate failed: %v", err)
	}
}

func TestParseLicenseRejectsInvalidData(t *testing.T) {
	if _, err := ParseLicense("not base64"); err == nil {
		t.Fatal("expected invalid base64 to fail")
	}
}

func TestLicenseStoreValidateRejectsExpiredAndMissing(t *testing.T) {
	store := &LicenseStore{
		licenses: make(map[string]*LicenseInfo),
		filePath: filepath.Join(t.TempDir(), "licenses.enc"),
	}

	if err := store.Validate("missing"); err == nil {
		t.Fatal("expected missing license to fail")
	}

	expired := &LicenseInfo{
		PluginName: "old-plugin",
		CustomerID: "customer-1",
		ExpiryDate: time.Now().Add(-time.Hour),
	}
	expired.Signature = generateSignature(expired, getSecretKey())
	store.licenses["old-plugin"] = expired

	if err := store.Validate("old-plugin"); err == nil {
		t.Fatal("expected expired license to fail")
	}
}

func TestEncryptDecryptRoundTrip(t *testing.T) {
	key := getEncryptionKey()
	plaintext := []byte("license payload")

	ciphertext, err := encrypt(plaintext, key)
	if err != nil {
		t.Fatalf("encrypt failed: %v", err)
	}
	got, err := decrypt(ciphertext, key)
	if err != nil {
		t.Fatalf("decrypt failed: %v", err)
	}
	if string(got) != string(plaintext) {
		t.Fatalf("decrypt() = %q, want %q", got, plaintext)
	}
}
