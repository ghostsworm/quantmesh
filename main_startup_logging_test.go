package main

import (
	"bytes"
	"errors"
	"log"
	"strings"
	"testing"
)

func TestStartupBotFailureLogWithholdsUnderlyingDiagnostic(t *testing.T) {
	var output bytes.Buffer
	originalWriter := log.Writer()
	originalFlags := log.Flags()
	log.SetOutput(&output)
	log.SetFlags(0)
	t.Cleanup(func() {
		log.SetOutput(originalWriter)
		log.SetFlags(originalFlags)
	})

	const diagnostic = "signed request failed: signature=private-token"
	logStartupBotFailure("bot-1", errors.New(diagnostic))

	if strings.Contains(output.String(), diagnostic) || strings.Contains(output.String(), "private-token") {
		t.Fatalf("startup failure log leaked underlying diagnostic: %s", output.String())
	}
	if !strings.Contains(output.String(), "bot-1") || !strings.Contains(output.String(), "底层診斷未輸出") {
		t.Fatalf("startup failure log lacks safe context: %s", output.String())
	}
}
