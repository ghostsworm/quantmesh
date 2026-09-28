package main

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestPriceFeedWatchdogReportsOnlyHealthTransitions(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var stale atomic.Bool
	transitions := make(chan bool, 4)
	done := make(chan struct{})
	go func() {
		defer close(done)
		watchPriceFeedHealth(ctx, time.Millisecond, stale.Load, func(value bool) { transitions <- value })
	}()
	stale.Store(true)
	select {
	case got := <-transitions:
		if !got {
			t.Fatal("first transition should report stale")
		}
	case <-time.After(time.Second):
		t.Fatal("watchdog did not report stale feed")
	}
	select {
	case got := <-transitions:
		t.Fatalf("unchanged state must not repeat callback: %v", got)
	case <-time.After(10 * time.Millisecond):
	}
	stale.Store(false)
	select {
	case got := <-transitions:
		if got {
			t.Fatal("recovery transition should report healthy")
		}
	case <-time.After(time.Second):
		t.Fatal("watchdog did not report recovery")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("watchdog did not stop")
	}
}
