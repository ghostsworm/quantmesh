//go:build embedded_frontend

package web

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"os"
	"path"
	"testing"
)

// TestEmbeddedFrontendProvenance reads compiled go:embed bytes, not the output
// directory. Run after the Ruby gate has built and synchronized the frontend.
func TestEmbeddedFrontendProvenance(t *testing.T) {
	want, err := os.ReadFile("../webui/dist/build-meta.json")
	if err != nil {
		t.Fatal(err)
	}
	got, err := staticFiles.ReadFile("dist/build-meta.json")
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("compiled metadata differs from verified frontend: %v", err)
	}
	var metadata struct {
		Schema       int               `json:"schema"`
		Version      string            `json:"version"`
		SourceDigest string            `json:"source_digest"`
		Files        map[string]string `json:"files"`
	}
	if err := json.Unmarshal(got, &metadata); err != nil {
		t.Fatal(err)
	}
	if metadata.Schema != 1 || metadata.Version == "" || len(metadata.SourceDigest) != 64 || len(metadata.Files) == 0 {
		t.Fatal("invalid embedded metadata")
	}
	seen := 0
	err = fs.WalkDir(staticFiles, "dist", func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || name == "dist/build-meta.json" {
			return nil
		}
		data, err := staticFiles.ReadFile(name)
		if err != nil {
			return err
		}
		hash := sha256.Sum256(data)
		relative := name[len("dist/"):]
		if metadata.Files[relative] != hex.EncodeToString(hash[:]) {
			t.Errorf("compiled asset absent from manifest or digest mismatch: %s", relative)
		}
		// Exercise the same filesystem used by the HTTP static handler.
		file, err := GetStaticFS().Open(path.Clean(relative))
		if err != nil {
			return err
		}
		if err := file.Close(); err != nil {
			return err
		}
		seen++
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if seen != len(metadata.Files) {
		t.Fatalf("compiled assets=%d, manifest assets=%d", seen, len(metadata.Files))
	}
}
