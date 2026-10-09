package main

import (
	"os"
	"path/filepath"
	"testing"

	"quantmesh/config"
)

func TestEnableBotPreservesUnreadableRecoveryStateDocument(t *testing.T) {
	for _, payload := range []string{`{"stopped-owner":{"enabled":false},`, `null`, `[]`} {
		t.Run(payload, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "states.json")
			if err := os.WriteFile(path, []byte(payload), 0600); err != nil {
				t.Fatal(err)
			}
			bm := NewBotManager(&config.Config{}, nil, nil, nil, "")
			bm.botStatesFileOverride = path
			err := bm.EnableBot("new-owner")
			after, readErr := os.ReadFile(path)
			if err == nil || readErr != nil || string(after) != payload {
				t.Fatal("invalid recovery state was accepted or overwritten")
			}
		})
	}
}

func TestEnableBotFallbackRetainsOtherStoppedOwner(t *testing.T) {
	path := filepath.Join(t.TempDir(), "states.json")
	if err := os.WriteFile(path, []byte(`{"stopped-owner":{"enabled":false,"reason":"fixture stop"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	bm := NewBotManager(&config.Config{}, nil, nil, nil, "")
	bm.botStatesFileOverride = path
	if err := bm.EnableBot("new-owner"); err != nil {
		t.Fatal(err)
	}
	if enabled, found := bm.isBotEnabledFromFile("stopped-owner"); !found || enabled {
		t.Fatal("other stopped owner was lost")
	}
	if enabled, found := bm.isBotEnabledFromFile("new-owner"); !found || !enabled {
		t.Fatal("normal enable not persisted")
	}
}
