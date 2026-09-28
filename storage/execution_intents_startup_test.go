package storage

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"quantmesh/config"
	"quantmesh/execution"
)

func TestStorageServiceMigratesExecutionIntentJournalAtStartup(t *testing.T) {
	cfg := &config.Config{}
	cfg.Storage.Enabled = true
	cfg.Storage.Type = "sqlite"
	cfg.Storage.Path = filepath.Join(t.TempDir(), "runtime.db")
	cfg.Storage.BufferSize = 16
	cfg.Storage.BatchSize = 4

	service, err := NewStorageService(cfg, context.Background())
	if err != nil {
		t.Fatalf("NewStorageService: %v", err)
	}
	t.Cleanup(service.Stop)

	backend, ok := service.GetStorage().(interface {
		LoadExecutionIntents(context.Context, string, int64, int) ([]execution.IntentJournalRecord, error)
	})
	if !ok {
		t.Fatal("storage does not expose the execution intent journal")
	}
	if _, err := backend.LoadExecutionIntents(context.Background(), strings.Repeat("a", 64), 0, 10); err != nil {
		t.Fatalf("execution intent journal was not migrated at startup: %v", err)
	}
}
