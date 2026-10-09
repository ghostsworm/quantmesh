//go:build darwin || linux

package main

import (
	"context"
	"os"
	"path/filepath"
	"quantmesh/event"
	"quantmesh/storage"
	"syscall"
	"testing"
	"time"
)

func TestStaleStopRecoveryMustNotOverwriteNewEnable(t *testing.T) {
	old := newEnableStateStorage(t)
	old.eventBus = event.NewEventBus(8)
	if err := old.EnableBot("owner"); err != nil {
		t.Fatal(err)
	}
	journalTestOwner(old, func() error { return nil })
	if err := old.storageService.GetStorage().Close(); err != nil {
		t.Fatal(err)
	}
	if err := old.StopBot("owner"); err == nil {
		t.Fatal("fixture primary failure absent")
	}
	ss, err := storage.NewStorageService(old.cfg, context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(ss.Stop)
	first := NewBotManager(old.cfg, event.NewEventBus(8), ss, nil, "")
	second := NewBotManager(old.cfg, event.NewEventBus(8), ss, nil, "")
	first.botStatesFileOverride, second.botStatesFileOverride = old.botStatesFileOverride, old.botStatesFileOverride
	path := first.stopJournalPath("owner")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	backup, alias := filepath.Join(t.TempDir(), "old.json"), filepath.Join(t.TempDir(), "reader.fifo")
	if err := os.Rename(path, backup); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(path, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(path, alias); err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() { result <- first.StopBot("owner") }()
	// Open a nonblocking writer only once the actual ReadFile has opened its
	// reader. Withhold bytes: this is an inode read barrier, not a mock manager.
	var writer *os.File
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		fd, err := syscall.Open(alias, syscall.O_WRONLY|syscall.O_NONBLOCK, 0)
		if err == nil {
			writer = os.NewFile(uintptr(fd), alias)
			break
		}
		if err != syscall.ENXIO {
			t.Fatal(err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if writer == nil {
		t.Fatal("actual stale reader did not reach barrier")
	}
	t.Cleanup(func() { writer.Close() })
	if err := os.Rename(backup, path); err != nil {
		t.Fatal(err)
	}
	peer := make(chan error, 1)
	go func() {
		if err := second.StopBot("owner"); err != nil {
			peer <- err
			return
		}
		peer <- second.EnableBot("owner")
	}()
	// Old code lets the peer finish before the stale read; corrected code
	// serializes it behind the reader. Both paths must preserve newer enable.
	peerDone := false
	select {
	case err := <-peer:
		peerDone = true
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(100 * time.Millisecond):
	}
	if n, err := writer.Write(data); err != nil || n != len(data) {
		t.Fatal("could not release old journal read")
	}
	writer.Close()
	select {
	case err := <-result:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("stale recovery did not terminate")
	}
	if !peerDone {
		select {
		case err := <-peer:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(10 * time.Second):
			t.Fatal("peer transition did not terminate")
		}
	}
	state, err := ss.GetStorage().GetBotState("owner")
	if err != nil || state == nil || !state.Enabled {
		t.Error("stale actual StopBot recovery overwrote newer explicit enable")
	}
}
