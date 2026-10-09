package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

const stopGuardProbeEnv = "QUANTMESH_STOP_GUARD_PROBE_DIR"

func TestStopGuardProcessHelper(t *testing.T) {
	dir := os.Getenv(stopGuardProbeEnv)
	if dir == "" {
		return
	}
	bm := NewBotManager(nil, nil, nil, nil, "")
	bm.botStatesFileOverride = filepath.Join(dir, "states.json")
	release, err := bm.lockStopJournal(context.Background(), "owner")
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if err := os.WriteFile(filepath.Join(dir, "locked"), []byte("ready"), 0600); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(filepath.Join(dir, "exit")); err == nil {
			// Bypass deferred close: the OS must release the held guard.
			os.Exit(stopJournalProbeExitCode)
		}
		time.Sleep(stopGuardPollInterval)
	}
	t.Fatal("parent did not release isolated child")
}

func TestStopGuardProcessExclusionAndAbruptRelease(t *testing.T) {
	dir := t.TempDir()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	child := exec.CommandContext(ctx, executable, "-test.run=^TestStopGuardProcessHelper$", "-test.count=1")
	child.Env = append(os.Environ(), stopGuardProbeEnv+"="+dir)
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	finished := false
	t.Cleanup(func() {
		if !finished {
			_ = child.Process.Kill()
			_ = child.Wait()
		}
	})
	deadline := time.Now().Add(5 * time.Second)
	ready := false
	for time.Now().Before(deadline) {
		if _, err := os.Stat(filepath.Join(dir, "locked")); err == nil {
			ready = true
			break
		}
		time.Sleep(stopGuardPollInterval)
	}
	if !ready {
		t.Fatal("child never acquired guard")
	}
	bm := NewBotManager(nil, nil, nil, nil, "")
	bm.botStatesFileOverride = filepath.Join(dir, "states.json")
	waitCtx, waitCancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer waitCancel()
	unlock, err := bm.lockStopJournal(waitCtx, "owner")
	if unlock != nil {
		unlock()
		t.Fatal("parent acquired child-held guard")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline, got %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "exit"), []byte("exit"), 0600); err != nil {
		t.Fatal(err)
	}
	err = child.Wait()
	finished = true
	var exited *exec.ExitError
	if !errors.As(err, &exited) || exited.ExitCode() != stopJournalProbeExitCode {
		t.Fatal("child did not abruptly exit with held lock")
	}
	release, err := bm.lockStopJournal(ctx, "owner")
	if err != nil {
		t.Fatal(err)
	}
	release()
}
