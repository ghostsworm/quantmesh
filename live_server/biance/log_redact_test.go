package main

import (
	"bytes"
	"log"
	"strings"
	"testing"
)

func TestStandardLogRedactionMasksCredentialPayloads(t *testing.T) {
	var output bytes.Buffer
	originalWriter := log.Writer()
	originalFlags := log.Flags()
	log.SetOutput(&output)
	log.SetFlags(0)
	t.Cleanup(func() {
		log.SetOutput(originalWriter)
		log.SetFlags(originalFlags)
	})

	installStandardLogRedaction()
	log.Printf(`raw account response: {"apiSecret":"hidden-secret","access_key":"hidden-access","client_secret":"hidden-client","private_key":"hidden-private","symbol":"BTCUSDT"} Authorization: Bearer hidden-token`)
	log.Printf(`signed request: https://user:hidden-password@example.test?listen_key=hidden-listen&signature=hidden-signature`)
	log.Printf("-----BEGIN RSA PRIVATE KEY-----hidden-pem-----END RSA PRIVATE KEY-----")

	message := output.String()
	for _, secret := range []string{"hidden-secret", "hidden-access", "hidden-client", "hidden-private", "hidden-token", "hidden-password", "hidden-listen", "hidden-signature", "hidden-pem"} {
		if strings.Contains(message, secret) {
			t.Fatalf("standard log leaked %q: %q", secret, message)
		}
	}
	if !strings.Contains(message, "BTCUSDT") || !strings.Contains(message, "[REDACTED]") {
		t.Fatalf("standard log lost safe context or redaction marker: %q", message)
	}
}
