package execution

import (
	"context"
	"errors"
	"sync"
	"testing"
)

func TestOpeningGateSourcesAndDrain(t *testing.T) {
	var gate OpeningGate
	release, err := gate.Begin()
	if err != nil {
		t.Fatal(err)
	}
	gate.Block("manual")
	gate.Block("market")
	gate.Unblock("market")
	if !gate.Blocked() {
		t.Fatal("market recovery cleared manual pause")
	}
	if _, err := gate.Begin(); !errors.Is(err, ErrOpeningPaused) {
		t.Fatalf("blocked admission: %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := gate.Drain(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("in-flight order must prevent drain: %v", err)
	}
	release()
	release() // idempotent even with duplicate caller cleanup
	if err := gate.Drain(t.Context()); err != nil {
		t.Fatal(err)
	}
	gate.Unblock("manual")
	release, err = gate.Begin()
	if err != nil {
		t.Fatal(err)
	}
	release()
}

func TestOpeningGateConcurrentAdmissionsAndPause(t *testing.T) {
	var gate OpeningGate
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if release, err := gate.Begin(); err == nil {
				release()
			} else if !errors.Is(err, ErrOpeningPaused) {
				t.Error(err)
			}
		}()
	}
	gate.Block("risk")
	wg.Wait()
	if err := gate.Drain(t.Context()); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 100; i++ {
		if _, err := gate.Begin(); !errors.Is(err, ErrOpeningPaused) {
			t.Fatalf("post-pause admission: %v", err)
		}
	}
}
