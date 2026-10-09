package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"quantmesh/config"
	"quantmesh/storage"
)

func TestEnableBotReturnsFallbackPersistenceFailure(t *testing.T) {
	bm := NewBotManager(&config.Config{}, nil, nil, nil, "")
	// An existing directory cannot be replaced by a JSON state document. Only
	// isolated test paths are touched; no default data/bot_states.json write.
	bm.botStatesFileOverride = t.TempDir()
	if err := bm.EnableBot("owner"); err == nil {
		t.Fatal("EnableBot falsely reported persisted enable after file write failure")
	}
}

func TestEnableBotFallbackSuccessIsReadableByStart(t *testing.T) {
	bm := NewBotManager(&config.Config{}, nil, nil, nil, "")
	bm.botStatesFileOverride = filepath.Join(t.TempDir(), "bot_states.json")
	if err := bm.EnableBot("owner"); err != nil {
		t.Fatal(err)
	}
	if enabled, reason := bm.IsBotEnabledInDB("owner"); !enabled || reason != "from_file" {
		t.Fatal("successful fallback enable not readable by startup")
	}
}

func newEnableStateStorage(t *testing.T) *BotManager {
	t.Helper()
	cfg := &config.Config{}
	cfg.Storage.Enabled = true
	cfg.Storage.Type = "sqlite"
	cfg.Storage.Path = filepath.Join(t.TempDir(), "state.db")
	cfg.Storage.BufferSize = 1
	cfg.Storage.BatchSize = 1
	ss, err := storage.NewStorageService(cfg, context.Background())
	if err != nil {
		t.Fatal(err)
	}
	bm := NewBotManager(cfg, nil, ss, nil, "")
	bm.botStatesFileOverride = filepath.Join(t.TempDir(), "fallback.json")
	t.Cleanup(ss.Stop)
	return bm
}

func TestEnableBotPrimarySuccessHasDurableReadback(t *testing.T) {
	bm := newEnableStateStorage(t)
	if err := bm.EnableBot("owner"); err != nil {
		t.Fatal(err)
	}
	state, err := bm.storageService.GetStorage().GetBotState("owner")
	if err != nil || state == nil || !state.Enabled {
		t.Fatalf("primary enable not persisted: %v", err)
	}
	if _, err := os.Stat(bm.botStatesFileOverride); !os.IsNotExist(err) {
		t.Fatal("successful primary enable unexpectedly wrote fallback")
	}
}

func TestEnableBotPrimaryFailureDoesNotClaimFallbackSuccess(t *testing.T) {
	bm := newEnableStateStorage(t)
	if err := bm.storageService.GetStorage().Close(); err != nil {
		t.Fatal(err)
	}
	if err := bm.EnableBot("owner"); err == nil {
		t.Fatal("primary failure falsely enabled Bot")
	}
	if _, err := os.Stat(bm.botStatesFileOverride); !os.IsNotExist(err) {
		t.Fatal("primary failure wrote misleading enabled fallback")
	}
}
