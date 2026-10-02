package strategy

import (
	"context"
	"errors"
	"math"
	"testing"

	"quantmesh/config"
)

func capitalReleaseFixture(t *testing.T) *CapitalAllocator {
	t.Helper()
	allocator := NewCapitalAllocator(&config.Config{}, 1000)
	allocator.RegisterStrategy("dca", 1, 0)
	allocator.Allocate()
	if !allocator.Reserve("dca", 200) {
		t.Fatal("fixture reservation failed")
	}
	return allocator
}

func TestCapitalReleaseRequiresSuccessfulCurrentProof(t *testing.T) {
	queryErr := errors.New("venue position evidence unavailable")
	for _, reason := range []string{"missing verifier", "query failure", "cancelled after query", "reserve release churn", "reallocation"} {
		t.Run(reason, func(t *testing.T) {
			allocator := capitalReleaseFixture(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			verify := func(context.Context) error { return nil }
			switch reason {
			case "missing verifier":
				verify = nil
			case "query failure":
				verify = func(context.Context) error { return queryErr }
			case "cancelled after query":
				verify = func(context.Context) error { cancel(); return nil }
			case "reserve release churn":
				verify = func(context.Context) error {
					if !allocator.Reserve("dca", 10) {
						t.Fatal("churn reservation failed")
					}
					allocator.Release("dca", 10)
					return nil
				}
			case "reallocation":
				verify = func(context.Context) error { allocator.Allocate(); return nil }
			}
			released, err := allocator.ReleaseVerified(ctx, "dca", verify)
			if err == nil || released != nil || allocator.GetUsed("dca") != 200 || allocator.GetAvailable("dca") != 800 {
				t.Fatalf("unverified/expired proof released capital: released=%v err=%v used=%v", released, err, allocator.GetUsed("dca"))
			}
			if reason == "query failure" && !errors.Is(err, queryErr) {
				t.Fatalf("verification cause lost: %v", err)
			}
		})
	}
}

func TestCapitalReleaseAllValidatesBeforeAnyMutation(t *testing.T) {
	allocator := capitalReleaseFixture(t)
	allocator.RegisterStrategy("momentum", 1, 0)
	allocator.Allocate()
	allocator.mu.Lock()
	allocator.strategies["momentum"].Used = math.NaN()
	allocator.mu.Unlock()
	released, err := allocator.ReleaseVerified(context.Background(), "", func(context.Context) error { return nil })
	if err == nil || released != nil || allocator.GetUsed("dca") != 200 {
		t.Fatalf("invalid all-strategy accounting partially cleared valid capital: released=%v err=%v", released, err)
	}
}

func TestCapitalReleaseVerifiedStaleReservationCanRecover(t *testing.T) {
	for _, name := range []string{"dca", ""} {
		allocator := capitalReleaseFixture(t)
		released, err := allocator.ReleaseVerified(context.Background(), name, func(context.Context) error { return nil })
		if err != nil || released["dca"] != 200 || allocator.GetUsed("dca") != 0 || allocator.GetAvailable("dca") != 1000 {
			t.Fatalf("verified stale reservation failed to release: released=%v err=%v", released, err)
		}
	}
}
