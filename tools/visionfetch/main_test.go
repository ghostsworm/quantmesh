package main

import (
	"strings"
	"testing"
)

func TestRequestRejectsABadDateBeforeAnyClientWork(t *testing.T) {
	_, err := request("BTCUSDT", "2026-02-31", "2026-03-01", "", true)
	if err == nil || !strings.Contains(err.Error(), "-from") {
		t.Fatalf("bad date accepted: %v", err)
	}
	_, err = request("BTCUSDT", "2026-03-02", "2026-03-01", "", true)
	if err == nil {
		t.Fatal("reversed range accepted")
	}
}
