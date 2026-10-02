package strategy

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestCapitalReleaseTargetPreflightDoesNotWaitForRowSnapshot(t *testing.T) {
	allocator := capitalReleaseFixture(t)
	row := allocator.strategies["dca"]
	row.mu.Lock()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	type result struct {
		found bool
		err   error
	}
	done := make(chan result, 1)
	go func() {
		found, err := allocator.HasCapitalReleaseTarget(ctx, "dca")
		done <- result{found, err}
	}()
	var got result
	select {
	case got = <-done:
		row.mu.Unlock()
	case <-time.After(time.Second):
		row.mu.Unlock()
		got = <-done
		t.Error("target lookup waited on unrelated monetary row snapshot")
	}
	if !got.found || got.err != nil || allocator.GetUsed("dca") != 200 {
		t.Fatalf("target-only lookup = %v, %v", got.found, got.err)
	}
}

func TestCapitalReleaseTargetPreflightCancelsParentWait(t *testing.T) {
	allocator := capitalReleaseFixture(t)
	allocator.mu.Lock()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := allocator.HasCapitalReleaseTarget(ctx, ""); done <- err }()
	var err error
	select {
	case err = <-done:
		allocator.mu.Unlock()
	case <-time.After(time.Second):
		allocator.mu.Unlock()
		err = <-done
		t.Error("preflight parent wait outlived request deadline")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("preflight error = %v", err)
	}
	for _, name := range []string{"", "dca", "missing"} {
		found, err := allocator.HasCapitalReleaseTarget(context.Background(), name)
		if err != nil || found != (name != "missing") {
			t.Fatalf("preflight retry name=%q found=%v err=%v", name, found, err)
		}
	}
}

func TestCapitalReleaseTargetPreflightRejectsMissingContext(t *testing.T) {
	allocator := capitalReleaseFixture(t)
	if found, err := allocator.HasCapitalReleaseTarget(nil, "dca"); found || err == nil {
		t.Fatalf("nil context: found=%v err=%v", found, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if found, err := allocator.HasCapitalReleaseTarget(ctx, "dca"); found || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled context: found=%v err=%v", found, err)
	}
	allocator.mu.Lock()
	allocator.strategies = nil
	allocator.mu.Unlock()
	if found, err := allocator.HasCapitalReleaseTarget(context.Background(), ""); found || err != nil {
		t.Fatalf("empty allocator: found=%v err=%v", found, err)
	}
}
