package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"quantmesh/config"
)

const fallbackProbePathEnv = "QUANTMESH_FALLBACK_PROBE_PATH"
const fallbackProbeIndexEnv = "QUANTMESH_FALLBACK_PROBE_INDEX"
const fallbackProcessOwners = 8

func TestFallbackStateProcessHelper(t *testing.T) {
	path := os.Getenv(fallbackProbePathEnv)
	if path == "" {
		return
	}
	index, err := strconv.Atoi(os.Getenv(fallbackProbeIndexEnv))
	if err != nil || index < 0 || index > 2 {
		t.Fatal("invalid isolated child index")
	}
	bm := NewBotManager(&config.Config{}, nil, nil, nil, "")
	bm.botStatesFileOverride = path
	for i := range fallbackProcessOwners {
		if err := bm.EnableBot(fmt.Sprintf("child-%d-owner-%d", index, i)); err != nil {
			t.Fatal(err)
		}
	}
}

func TestFallbackStateConcurrentProcessesRetainAllAcknowledgedEnables(t *testing.T) {
	path := filepath.Join(t.TempDir(), "states.json")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	results := make(chan error, 3)
	for index := range 3 {
		child := exec.CommandContext(ctx, executable, "-test.run=^TestFallbackStateProcessHelper$", "-test.count=1")
		child.Env = append(os.Environ(), fallbackProbePathEnv+"="+path, fallbackProbeIndexEnv+"="+strconv.Itoa(index))
		go func() { results <- child.Run() }()
	}
	for range 3 {
		if err := <-results; err != nil {
			t.Errorf("isolated child update failed: %v", err)
		}
	}
	bm := NewBotManager(&config.Config{}, nil, nil, nil, "")
	bm.botStatesFileOverride = path
	for index := range 3 {
		for i := range fallbackProcessOwners {
			if enabled, found := bm.isBotEnabledFromFile(fmt.Sprintf("child-%d-owner-%d", index, i)); !found || !enabled {
				t.Errorf("child-%d-owner-%d acknowledgement lost", index, i)
			}
		}
	}
}
