package visionfeed

import (
	"os"
	"path/filepath"
	"strings"
	testing "testing"
)

func TestPublishVerifiedFileKeepsExistingBytes(t *testing.T) {
	dir := t.TempDir()
	data := filepath.Join(dir, "stage", "TESTUSDT-1m-2026-01-01.zip")
	if err := os.MkdirAll(filepath.Dir(data), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(data, []byte("zip-bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeChecksum(t, data)

	existing := filepath.Join(dir, "final", "TESTUSDT-1m-2026-01-01.zip")
	if err := os.MkdirAll(filepath.Dir(existing), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(existing, []byte("already-verified"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := PublishVerifiedFile(filepath.Join(dir, "final"), data, data+".CHECKSUM"); err == nil {
		t.Fatal("an existing final name was replaced")
	}
	got, err := os.ReadFile(existing)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "already-verified" {
		t.Fatalf("existing bytes changed to %q", got)
	}
	if _, err := os.Stat(existing + ".CHECKSUM"); !os.IsNotExist(err) {
		t.Fatal("a checksum was published without its data file")
	}
}

func TestPublishVerifiedFileRejectsABadSidecar(t *testing.T) {
	dir := t.TempDir()
	data := filepath.Join(dir, "TESTUSDT-1m-2026-01-01.zip")
	if err := os.WriteFile(data, []byte("zip-bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(data+".CHECKSUM", []byte(strings.Repeat("ab", 32)+"  other.zip\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(dir, "final")
	if err := PublishVerifiedFile(dest, data, data+".CHECKSUM"); err == nil || !strings.Contains(err.Error(), "visionfeed: checksum") {
		t.Fatalf("bad sidecar published: %v", err)
	}
	entries, _ := os.ReadDir(dest)
	if len(entries) != 0 {
		t.Fatalf("destination not empty: %v", entries)
	}
}
