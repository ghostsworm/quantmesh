package exchange

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOrderIDStringUsesDecimalFormat(t *testing.T) {
	got := orderIDString(123456789)
	if got != "123456789" {
		t.Fatalf("orderIDString() = %q, want decimal order ID", got)
	}
	if got == string(rune(123456789)) {
		t.Fatalf("orderIDString() used rune conversion instead of decimal formatting")
	}
}

func TestJoinOrderOpErrorsKeepsFailuresVisible(t *testing.T) {
	err := joinOrderOpErrors("batch cancel test orders", []error{
		nil,
		errors.New("first failure"),
		errors.New("second failure"),
	})
	if err == nil {
		t.Fatal("joinOrderOpErrors() returned nil for failed order operations")
	}
	text := err.Error()
	for _, want := range []string{"batch cancel test orders failed", "first failure", "second failure"} {
		if !strings.Contains(text, want) {
			t.Fatalf("joined error %q does not contain %q", text, want)
		}
	}
}

func TestWrappersDoNotUseRuneOrderIDConversion(t *testing.T) {
	files, err := filepath.Glob("wrapper_*.go")
	if err != nil {
		t.Fatalf("glob wrappers: %v", err)
	}
	for _, file := range files {
		content, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}
		if strings.Contains(string(content), "string(rune(orderID))") {
			t.Fatalf("%s still converts numeric order IDs through rune", file)
		}
	}
}
