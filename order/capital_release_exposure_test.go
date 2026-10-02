package order

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestCapitalReleaseExposureRequiresContextQuoteCapability(t *testing.T) {
	oe := NewExchangeOrderExecutor(&fakeOrderExchange{}, "BTCUSDT", 0, 0, nil, "")
	bindTestExposureBook(t, oe)
	oe.SetExposureMarkProvider(func() (float64, time.Time) { panic("synchronous provider called by proof") })
	if snapshot, err := oe.CapitalReleaseExposureSnapshot(t.Context()); err == nil || snapshot != nil {
		t.Fatal("legacy provider used without context capability")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
	defer cancel()
	oe.SetCapitalExposureQuoteProvider(func(got context.Context) (float64, time.Time, error) {
		if got != ctx {
			t.Error("request context not forwarded")
		}
		<-got.Done()
		return 0, time.Time{}, got.Err()
	})
	if snapshot, err := oe.CapitalReleaseExposureSnapshot(ctx); !errors.Is(err, context.DeadlineExceeded) || snapshot != nil {
		t.Fatalf("provider deadline lost: %+v %v", snapshot, err)
	}
	oe.SetCapitalExposureQuoteProvider(func(context.Context) (float64, time.Time, error) { return 101, time.Now(), nil })
	if snapshot, err := oe.CapitalReleaseExposureSnapshot(t.Context()); err != nil || !snapshot.Ready || snapshot.Mark != 101 {
		t.Fatalf("quote recovery failed: %+v %v", snapshot, err)
	}
	for _, invalid := range []context.Context{nil, ctx} {
		if snapshot, err := oe.CapitalReleaseExposureSnapshot(invalid); err == nil || snapshot != nil {
			t.Fatal("invalid context returned evidence")
		}
	}
	base, stop := context.WithCancel(t.Context())
	defer stop()
	oe.SetCapitalExposureQuoteProvider(func(context.Context) (float64, time.Time, error) {
		stop()
		return 100, time.Now(), nil
	})
	if snapshot, err := oe.CapitalReleaseExposureSnapshot(base); !errors.Is(err, context.Canceled) || snapshot != nil {
		t.Fatalf("provider ignored cancellation and returned usable proof: %+v %v", snapshot, err)
	}
}
