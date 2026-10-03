package main

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"quantmesh/config"
	"quantmesh/exchange"
	"quantmesh/lock"
)

func TestFundingCarryConstructorDoesNotContinueAfterCancelledPreflight(t *testing.T) {
	for _, before := range []bool{true, false} {
		t.Run(map[bool]string{true: "before_preflight", false: "during_preflight"}[before], func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if before {
				cancel()
			}
			checks, creates := 0, 0
			deps := fundingCarryStartupDependencies{
				checkSetup: func(context.Context, *config.Config, string, string) (*exchange.FundingCarryPermissionResult, error) {
					checks++
					cancel()
					return &exchange.FundingCarryPermissionResult{OK: true}, nil
				},
				newExchange: func(*config.Config, string, string, string) (exchange.IExchange, error) {
					creates++
					return nil, errors.New("constructor continued after cancel")
				},
			}
			rt, err := startFundingCarrySymbolRuntimeWithDependencies(ctx, &config.Config{}, config.SymbolConfig{Exchange: "binance", Symbol: "BTCUSDT"}, nil, nil, nil, nil, nil, deps)
			if rt != nil || !errors.Is(err, context.Canceled) || creates != 0 || (before && checks != 0) {
				t.Fatalf("cancel not respected: runtime=%v error=%v checks=%d creates=%d", rt, err, checks, creates)
			}
		})
	}
}

type startupPriceReader struct {
	price    float64
	observed chan struct{}
}

func (r startupPriceReader) GetLastPrice() float64 {
	if r.observed != nil {
		select {
		case r.observed <- struct{}{}:
		default:
		}
	}
	return r.price
}

func TestFundingCarryInitialPriceWaitRespectsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	observed := make(chan struct{}, 1)
	done := make(chan error, 1)
	go func() {
		done <- waitFundingCarryInitialPrice(ctx, startupPriceReader{observed: observed}, 5*time.Second)
	}()
	<-observed
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancellation lost: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("startup continued sleeping after cancellation")
	}
}

func TestFundingCarryInitialPriceDoesNotOverrideCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := waitFundingCarryInitialPrice(ctx, startupPriceReader{price: 50000}, time.Millisecond); !errors.Is(err, context.Canceled) {
		t.Fatalf("available quote overrode cancellation: %v", err)
	}
}

type cancelledStartupVenue struct {
	exchange.IExchange
	stops   int
	stopErr error
}

func (v *cancelledStartupVenue) StopOrderStream() error { v.stops++; return v.stopErr }

func TestFundingCarryConstructorCleansConnectionCreatedDuringCancellation(t *testing.T) {
	for _, withStopFailure := range []bool{false, true} {
		t.Run(map[bool]string{false: "clean", true: "cleanup_failure"}[withStopFailure], func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			venue := &cancelledStartupVenue{}
			cleanupErr := errors.New("injected order stream cleanup failure")
			if withStopFailure {
				venue.stopErr = cleanupErr
			}
			cfg := &config.Config{Exchanges: map[string]config.ExchangeConfig{"binance": {APIKey: "fixture-account-identity"}}}
			creates := 0
			deps := fundingCarryStartupDependencies{
				checkSetup: func(context.Context, *config.Config, string, string) (*exchange.FundingCarryPermissionResult, error) {
					return &exchange.FundingCarryPermissionResult{OK: true}, nil
				},
				newExchange: func(*config.Config, string, string, string) (exchange.IExchange, error) {
					creates++
					cancel()
					return venue, nil
				},
			}
			rt, err := startFundingCarrySymbolRuntimeWithDependencies(ctx, cfg, config.SymbolConfig{Exchange: "binance", Symbol: "BTCUSDT"}, nil, nil, lock.NewNopLock(), nil, nil, deps)
			if rt != nil || !errors.Is(err, context.Canceled) || creates != 1 || venue.stops != 1 {
				t.Fatalf("cancel/cleanup lost: %v creates=%d stops=%d", err, creates, venue.stops)
			}
			if withStopFailure && !errors.Is(err, cleanupErr) {
				t.Fatalf("cleanup cause lost: %v", err)
			}
		})
	}
}

func TestFundingCarryInitialPriceRequiresFinitePositiveQuote(t *testing.T) {
	for _, price := range []float64{math.Inf(1), math.Inf(-1), math.NaN(), 0, -1} {
		if err := waitFundingCarryInitialPrice(context.Background(), startupPriceReader{price: price}, time.Nanosecond); err == nil {
			t.Fatalf("invalid quote accepted: %v", price)
		}
	}
	if err := waitFundingCarryInitialPrice(context.Background(), startupPriceReader{price: 50000}, time.Millisecond); err != nil {
		t.Fatal(err)
	}
}

type startupLeaseCleanupFailure struct {
	lock.DistributedLock
	failure error
	unlocks int
}

func (l *startupLeaseCleanupFailure) Unlock(context.Context, string) error {
	l.unlocks++
	return l.failure
}

func TestFundingCarryConstructorPreservesLeaseCleanupFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cleanupErr := errors.New("injected lease cleanup failure")
	coordinator := &startupLeaseCleanupFailure{DistributedLock: lock.NewNopLock(), failure: cleanupErr}
	venue := &cancelledStartupVenue{}
	cfg := &config.Config{Exchanges: map[string]config.ExchangeConfig{"binance": {APIKey: "fixture-account-identity"}}}
	deps := fundingCarryStartupDependencies{
		checkSetup: func(context.Context, *config.Config, string, string) (*exchange.FundingCarryPermissionResult, error) {
			return &exchange.FundingCarryPermissionResult{OK: true}, nil
		},
		newExchange: func(*config.Config, string, string, string) (exchange.IExchange, error) {
			cancel()
			return venue, nil
		},
	}
	rt, err := startFundingCarrySymbolRuntimeWithDependencies(ctx, cfg, config.SymbolConfig{Exchange: "binance", Symbol: "BTCUSDT"}, nil, nil, coordinator, nil, nil, deps)
	if rt != nil || !errors.Is(err, context.Canceled) || !errors.Is(err, cleanupErr) || coordinator.unlocks != 2 || venue.stops != 1 {
		t.Fatalf("constructor lost cancellation/cleanup evidence: %v unlocks=%d stops=%d", err, coordinator.unlocks, venue.stops)
	}
}
