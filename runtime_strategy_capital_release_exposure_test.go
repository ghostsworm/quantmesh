package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"quantmesh/execution"
)

func TestRuntimeStrategyCapitalReleaseExposureEvidence(t *testing.T) {
	for _, mode := range []string{"legacy_only", "read_error", "deadline", "invalid", "stale", "future"} {
		t.Run(mode, func(t *testing.T) {
			rt, venue, _ := capitalReleaseRuntimeFixture(t)
			if mode == "stale" {
				book, err := execution.NewExposureBook(execution.ExposureLimits{}, time.Minute)
				if err != nil {
					t.Fatal(err)
				}
				if err := book.Seed(nil); err != nil {
					t.Fatal(err)
				}
				rt.ExchangeExecutor.SetExposureBook(book)
			}
			rt.ExchangeExecutor.SetExposureMarkProvider(func() (float64, time.Time) { panic("capital proof called legacy quote") })
			if mode != "legacy_only" {
				rt.ExchangeExecutor.SetCapitalExposureQuoteProvider(func(ctx context.Context) (float64, time.Time, error) {
					switch mode {
					case "read_error":
						return 0, time.Time{}, errors.New("quote unavailable")
					case "deadline":
						<-ctx.Done()
						return 0, time.Time{}, ctx.Err()
					case "invalid":
						return -1, time.Now(), nil
					case "stale":
						return 100, time.Now().Add(-2 * time.Minute), nil
					default:
						// Future evidence invalidates even a previous good quote.
						return 100, time.Now().Add(time.Minute), nil
					}
				})
			}
			ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
			defer cancel()
			amounts, err := releaseRuntimeStrategyCapital(ctx, []*SymbolRuntime{rt}, "dca")
			if err == nil || amounts["dca"] != 0 || rt.StrategyManager.GetCapitalAllocator().GetUsed("dca") != 200 || venue.lastReadSymbol != "" {
				t.Fatalf("bad quote released capital: %v %v", amounts, err)
			}
			rt.ExchangeExecutor.SetCapitalExposureQuoteProvider(func(context.Context) (float64, time.Time, error) { return 100, time.Now(), nil })
			amounts, err = releaseRuntimeStrategyCapital(t.Context(), []*SymbolRuntime{rt}, "dca")
			if err != nil || amounts["dca"] != 200 {
				t.Fatalf("verified quote did not recover: %v %v", amounts, err)
			}
		})
	}
}
