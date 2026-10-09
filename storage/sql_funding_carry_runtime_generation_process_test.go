package storage

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

const (
	fundingCarryGenerationProcessDBEnv      = "QUANTMESH_FC_GENERATION_PROCESS_DB"
	fundingCarryGenerationProcessReadyEnv   = "QUANTMESH_FC_GENERATION_PROCESS_READY"
	fundingCarryGenerationProcessReleaseEnv = "QUANTMESH_FC_GENERATION_PROCESS_RELEASE"
	fundingCarryGenerationProcessResultEnv  = "QUANTMESH_FC_GENERATION_PROCESS_RESULT"
)

// Invoked in a child copy of the test binary. It deliberately keeps an old
// generation across the parent's replacement claim, then attempts one write.
func TestFundingCarryRuntimeGenerationOldOwnerProcessHelper(t *testing.T) {
	dbPath := os.Getenv(fundingCarryGenerationProcessDBEnv)
	if dbPath == "" {
		return
	}
	readyPath := os.Getenv(fundingCarryGenerationProcessReadyEnv)
	releasePath := os.Getenv(fundingCarryGenerationProcessReleaseEnv)
	resultPath := os.Getenv(fundingCarryGenerationProcessResultEnv)
	store, err := NewSQLStorage(dbPath)
	if err != nil {
		t.Fatal("open child storage:", err)
	}
	defer store.Close()
	owner, err := store.ClaimFundingCarryRuntimeGeneration(t.Context(), fundingCarryTestScopeKeys())
	if err != nil {
		t.Fatal("child old-owner claim:", err)
	}
	if err := os.WriteFile(readyPath, []byte("old-owner-claimed"), 0600); err != nil {
		t.Fatal("signal old owner ready:", err)
	}
	deadline := time.Now().Add(20 * time.Second)
	for {
		if _, err := os.Stat(releasePath); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for replacement-claim barrier")
		}
		time.Sleep(10 * time.Millisecond)
	}
	state := &StrategyRuntimeState{BotID: "process-fencing", StrategyName: "funding_carry", SchemaVersion: 1, Payload: `{"stale":true}`}
	if err := store.SetFundingCarryRuntimeState(t.Context(), owner, state); !errors.Is(err, ErrFundingCarryRuntimeGenerationLost) {
		t.Fatalf("old process Save error = %v, want generation lost", err)
	}
	if err := os.WriteFile(resultPath, []byte("STALE_WRITE_REJECTED"), 0600); err != nil {
		t.Fatal("write stale-write result marker:", err)
	}
}

func TestFundingCarryRuntimeGenerationSQLiteOldProcessCannotWriteAfterTakeover(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "funding-carry-process.db")
	store, err := NewSQLStorage(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	initial := &StrategyRuntimeState{BotID: "process-fencing", StrategyName: "funding_carry", SchemaVersion: 1, Payload: `{"stable":"parent"}`}
	first, err := store.ClaimFundingCarryRuntimeGeneration(t.Context(), fundingCarryTestScopeKeys())
	if err != nil {
		t.Fatal("seed initial generation:", err)
	}
	if err := store.SetFundingCarryRuntimeState(t.Context(), first, initial); err != nil {
		t.Fatal("seed initial runtime state:", err)
	}
	readyPath := filepath.Join(t.TempDir(), "old-owner-ready")
	releasePath := filepath.Join(t.TempDir(), "release-old-owner")
	resultPath := filepath.Join(t.TempDir(), "old-owner-result")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal("locate test executable:", err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	child := exec.CommandContext(ctx, executable, "-test.run=^TestFundingCarryRuntimeGenerationOldOwnerProcessHelper$", "-test.count=1")
	child.Env = append(os.Environ(),
		fundingCarryGenerationProcessDBEnv+"="+dbPath,
		fundingCarryGenerationProcessReadyEnv+"="+readyPath,
		fundingCarryGenerationProcessReleaseEnv+"="+releasePath,
		fundingCarryGenerationProcessResultEnv+"="+resultPath,
	)
	childErr := make(chan error, 1)
	go func() { childErr <- child.Run() }()
	if err := waitForFundingCarryGenerationFile(ctx, readyPath); err != nil {
		_ = child.Process.Kill()
		t.Fatalf("old process did not acquire its generation: %v", err)
	}
	second, err := store.ClaimFundingCarryRuntimeGeneration(t.Context(), fundingCarryTestScopeKeys())
	if err != nil {
		_ = child.Process.Kill()
		t.Fatal("parent replacement claim:", err)
	}
	if err := os.WriteFile(releasePath, []byte("replacement-claim-committed"), 0600); err != nil {
		_ = child.Process.Kill()
		t.Fatal("release old-owner barrier:", err)
	}
	select {
	case err := <-childErr:
		if err != nil {
			t.Fatalf("old-owner helper failed: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("old-owner helper exceeded timeout")
	}
	result, err := os.ReadFile(resultPath)
	if err != nil || string(result) != "STALE_WRITE_REJECTED" {
		t.Fatalf("child did not confirm stale write rejection: result=%q err=%v", result, err)
	}
	loaded, err := store.GetStrategyRuntimeState(initial.BotID, initial.StrategyName)
	if err != nil || loaded == nil || loaded.Payload != initial.Payload {
		t.Fatalf("old process modified canonical state: %+v err=%v", loaded, err)
	}
	current := *initial
	current.Payload = `{"current":"parent"}`
	if err := store.SetFundingCarryRuntimeState(t.Context(), second, &current); err != nil {
		t.Fatal("replacement owner could not write:", err)
	}
}

func waitForFundingCarryGenerationFile(ctx context.Context, path string) error {
	for {
		if _, err := os.Stat(path); err == nil {
			return nil
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("stat barrier file: %w", err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
}
