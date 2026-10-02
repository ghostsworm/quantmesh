package strategy

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestCapitalReleaseLockWaitCancelsWithoutMutation(t *testing.T) {
	for _, phase := range []string{"snapshot", "commit", "row"} {
		t.Run(phase, func(t *testing.T) {
			allocator := capitalReleaseFixture(t)
			allocator.RegisterStrategy("z", 1, 0)
			allocator.Allocate()
			if !allocator.Reserve("z", 100) {
				t.Fatal("second reservation failed")
			}
			var unlock func()
			verify := func(context.Context) error { return nil }
			switch phase {
			case "snapshot":
				allocator.mu.Lock()
				unlock = allocator.mu.Unlock
			case "commit":
				var held atomic.Bool
				verify = func(context.Context) error {
					allocator.mu.Lock()
					held.Store(true)
					return nil
				}
				unlock = func() {
					if held.Load() {
						allocator.mu.Unlock()
					} else {
						t.Error("commit contention fixture never acquired parent lock")
					}
				}
			case "row":
				row := allocator.strategies["z"]
				row.mu.RLock()
				unlock = row.mu.RUnlock
			}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
			defer cancel()
			type result struct {
				released map[string]float64
				err      error
			}
			done := make(chan result, 1)
			go func() {
				released, err := allocator.ReleaseVerified(ctx, "", verify)
				done <- result{released, err}
			}()
			var got result
			select {
			case got = <-done:
				unlock()
			case <-time.After(time.Second):
				unlock()
				got = <-done
				t.Error("release remained blocked after request deadline")
			}
			if !errors.Is(got.err, context.DeadlineExceeded) || got.released != nil {
				t.Errorf("cancelled release = %v, %v", got.released, got.err)
			}
			if allocator.GetUsed("dca") != 200 || allocator.GetUsed("z") != 100 {
				t.Fatal("cancelled all-strategy release mutated accounting")
			}
			// A timed-out waiter must not retain parent or previously acquired row locks.
			released, err := allocator.ReleaseVerified(context.Background(), "", func(context.Context) error { return nil })
			if err != nil || released["dca"] != 200 || released["z"] != 100 {
				t.Fatalf("valid retry failed: %v, %v", released, err)
			}
		})
	}
}

func TestCapitalReleaseRejectsInvalidRowIdentityWithoutMutation(t *testing.T) {
	for _, alias := range []bool{false, true} {
		allocator := capitalReleaseFixture(t)
		allocator.mu.Lock()
		if alias {
			allocator.strategies["z"] = allocator.strategies["dca"]
		} else {
			allocator.strategies["z"] = nil
		}
		allocator.mu.Unlock()
		released, err := allocator.ReleaseVerified(context.Background(), "", func(context.Context) error { return nil })
		if err == nil || released != nil || allocator.GetUsed("dca") != 200 {
			t.Fatalf("invalid identity released capital: alias=%v released=%v err=%v", alias, released, err)
		}
		// Unselected damaged rows must not block a valid single-row release.
		released, err = allocator.ReleaseVerified(context.Background(), "dca", func(context.Context) error { return nil })
		if err != nil || released["dca"] != 200 {
			t.Fatalf("unselected row prevented valid release: %v, %v", released, err)
		}
	}
}
