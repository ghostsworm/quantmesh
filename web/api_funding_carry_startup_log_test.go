package web

import (
	"bytes"
	"errors"
	"log"
	"strings"
	"testing"
)

func TestFundingCarryBotStartFailureLogWithholdsUnderlyingDiagnostic(t *testing.T) {
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
	logFundingCarryBotStartFailure("funding-bot", errors.New(diagnostic))

	if strings.Contains(output.String(), diagnostic) || strings.Contains(output.String(), "private-token") {
		t.Fatalf("Funding Carry startup log leaked underlying diagnostic: %s", output.String())
	}
	if !strings.Contains(output.String(), "funding-bot") || !strings.Contains(output.String(), "底層診斷未輸出") {
		t.Fatalf("Funding Carry startup log lacks safe context: %s", output.String())
	}
}
