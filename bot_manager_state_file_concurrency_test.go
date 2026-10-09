package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"quantmesh/config"
)

func TestConcurrentFallbackEnablesPreserveAllOwners(t *testing.T) {
	path := filepath.Join(t.TempDir(), "states.json")
	if err := os.WriteFile(path, []byte(`{"stopped-owner":{"enabled":false,"future":{"generation":7}}}`), 0600); err != nil {
		t.Fatal(err)
	}
	const owners = 24
	start := make(chan struct{})
	errors := make(chan error, owners)
	var wait sync.WaitGroup
	for i := range owners {
		bm := NewBotManager(&config.Config{}, nil, nil, nil, "")
		bm.botStatesFileOverride = path
		wait.Add(1)
		go func(i int) {
			defer wait.Done()
			<-start
			errors <- bm.EnableBot(fmt.Sprintf("owner-%d", i))
		}(i)
	}
	close(start)
	wait.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Errorf("concurrent actual EnableBot failed: %v", err)
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var states map[string]json.RawMessage
	if err := json.Unmarshal(data, &states); err != nil {
		t.Fatal(err)
	}
	if len(states) != owners+1 {
		t.Errorf("lost acknowledged owner states: got %d want %d", len(states), owners+1)
	}
	for i := range owners {
		var state struct {
			Enabled bool `json:"enabled"`
		}
		if err := json.Unmarshal(states[fmt.Sprintf("owner-%d", i)], &state); err != nil || !state.Enabled {
			t.Errorf("acknowledged owner-%d enable not retained", i)
		}
	}
	var stopped struct {
		Enabled bool `json:"enabled"`
		Future  struct {
			Generation int `json:"generation"`
		} `json:"future"`
	}
	if err := json.Unmarshal(states["stopped-owner"], &stopped); err != nil || stopped.Enabled || stopped.Future.Generation != 7 {
		t.Error("untouched stopped owner or unknown recovery metadata lost")
	}
	bm := NewBotManager(&config.Config{}, nil, nil, nil, "")
	bm.botStatesFileOverride = path
	if err := bm.EnableBot("stopped-owner"); err != nil {
		t.Fatal(err)
	}
	data, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &states); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(states["stopped-owner"], &stopped); err != nil || !stopped.Enabled || stopped.Future.Generation != 7 {
		t.Error("updating same owner discarded unknown recovery metadata")
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("fallback state file lacks private permissions")
	}
}
