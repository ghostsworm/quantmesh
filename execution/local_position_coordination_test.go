package execution

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestLocalPositionCoordinationSerializesSameKeyAndAllowsDifferentKeys(t *testing.T) {
	firstRelease, err := AcquireLocalPositionCoordination(context.Background(), "venue:BTCUSDT")
	if err != nil {
		t.Fatal(err)
	}
	differentRelease, err := AcquireLocalPositionCoordination(context.Background(), "venue:ETHUSDT")
	if err != nil {
		firstRelease()
		t.Fatalf("different key was blocked: %v", err)
	}
	differentRelease()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := AcquireLocalPositionCoordination(ctx, "venue:BTCUSDT"); !errors.Is(err, context.DeadlineExceeded) {
		firstRelease()
		t.Fatalf("same-key acquire error = %v, want context deadline", err)
	}
	firstRelease()

	release, err := AcquireLocalPositionCoordination(context.Background(), "venue:BTCUSDT")
	if err != nil {
		t.Fatalf("same key remained locked after release: %v", err)
	}
	release()
}

func TestLocalPositionCoordinationReleaseIsIdempotent(t *testing.T) {
	release, err := AcquireLocalPositionCoordination(context.Background(), "venue:BTCUSDT")
	if err != nil {
		t.Fatal(err)
	}
	release()
	release()
	unlock, err := AcquireLocalPositionCoordination(context.Background(), "venue:BTCUSDT")
	if err != nil {
		t.Fatalf("idempotent release corrupted the lock: %v", err)
	}
	unlock()
}
