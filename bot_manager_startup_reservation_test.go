package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"quantmesh/config"
	"quantmesh/event"
)

func TestBotStartupReservationRejectsConcurrentConflictingInitializers(t *testing.T) {
	bm := NewBotManager(&config.Config{}, nil, nil, nil, "")
	var wg sync.WaitGroup
	results := make(chan func(), 2)
	errors := make(chan error, 2)
	ready := make(chan struct{})
	for _, id := range []string{"one", "two"} {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			<-ready
			finish, err := bm.reserveBotStartup(config.BotConfig{ID: id, Exchange: "binance", Symbol: "BTCUSDT", MarketType: "futures"})
			results <- finish
			errors <- err
		}(id)
	}
	close(ready)
	wg.Wait()
	winners, conflicts := 0, 0
	for range 2 {
		if finish := <-results; finish != nil {
			winners++
			defer finish()
		}
		if err := <-errors; err != nil && strings.Contains(err.Error(), "symbol_conflict") {
			conflicts++
		}
	}
	if winners != 1 || conflicts != 1 {
		t.Fatalf("initialization race admitted %d winners, %d conflicts", winners, conflicts)
	}
}

func TestBotStartupReservationCopiesSpreadLegsAndReleasesOnlyOnce(t *testing.T) {
	bm := NewBotManager(&config.Config{}, nil, nil, nil, "")
	spread := &config.FundingPerpSpreadConfig{LegA: config.FundingPerpLeg{Exchange: "binance", Symbol: "BTCUSDT"}, LegB: config.FundingPerpLeg{Exchange: "okx", Symbol: "BTC-USDT-SWAP"}}
	finish, err := bm.reserveBotStartup(config.BotConfig{ID: "spread", MarketType: config.MarketTypeFundingPerpSpread, FundingPerpSpread: spread})
	if err != nil {
		t.Fatal(err)
	}
	spread.LegA.Symbol = "ETHUSDT"
	candidate := config.BotConfig{ID: "single", Exchange: "BINANCE", Symbol: "btcusdt", MarketType: "futures"}
	if release, err := bm.reserveBotStartup(candidate); err == nil {
		release()
		finish()
		t.Fatal("caller mutation changed reserved spread leg")
	}
	finish()
	newFinish, err := bm.reserveBotStartup(config.BotConfig{ID: "spread", MarketType: "futures", Exchange: "binance", Symbol: "BTCUSDT"})
	if err != nil {
		t.Fatal(err)
	}
	defer newFinish()
	finish() // A stale second cleanup must not delete the replacement reservation.
	if release, err := bm.reserveBotStartup(candidate); err == nil {
		release()
		t.Fatal("duplicate cleanup deleted replacement reservation")
	}
}

func TestBotStartupReservationAllowsUnrelatedScopeButBlocksRegisteredOwner(t *testing.T) {
	bm := NewBotManager(&config.Config{}, nil, nil, nil, "")
	owner := config.BotConfig{ID: "owner", Exchange: "binance", Symbol: "BTCUSDT", MarketType: "futures"}
	bm.AddRuntime(&BotRuntime{BotID: "owner", Config: owner})
	owner.ID = "new"
	if release, err := bm.reserveBotStartup(owner); err == nil {
		release()
		t.Fatal("registered owner ignored")
	}
	owner.Symbol = "ETHUSDT"
	release, err := bm.reserveBotStartup(owner)
	if err != nil {
		t.Fatal(err)
	}
	release()
}

func TestStartBotPendingScopeRejectedBeforeExternalFeeLookup(t *testing.T) {
	bus := event.NewEventBus(8)
	bm := NewBotManager(&config.Config{}, bus, nil, nil, "")
	bm.botStatesFileOverride = filepath.Join(t.TempDir(), "states.json")
	if err := os.WriteFile(bm.botStatesFileOverride, []byte(`{"candidate":{"enabled":true}}`), 0600); err != nil {
		t.Fatal(err)
	}
	owner := config.BotConfig{ID: "owner", Exchange: "binance", Symbol: "BTCUSDT", MarketType: "futures"}
	finish, err := bm.reserveBotStartup(owner)
	if err != nil {
		t.Fatal(err)
	}
	defer finish()
	feeCalls := 0
	bm.feeRateFetcher = func(*config.Config, string, string) (float64, float64, error) {
		feeCalls++
		return 0, 0, nil
	}
	owner.ID = "candidate"
	_, err = bm.StartBot(context.Background(), owner)
	if err == nil || !strings.Contains(err.Error(), "symbol_conflict") || feeCalls != 0 {
		t.Fatalf("pending initialization reached external path: %v, fee calls %d", err, feeCalls)
	}
	if len(bm.List()) != 0 {
		t.Fatal("rejected pending scope published a runtime")
	}
}

func TestStartBotCancellationAfterFeeLookupReleasesReservation(t *testing.T) {
	bm := NewBotManager(&config.Config{}, event.NewEventBus(8), nil, nil, "")
	bm.botStatesFileOverride = filepath.Join(t.TempDir(), "states.json")
	if err := os.WriteFile(bm.botStatesFileOverride, []byte(`{"candidate":{"enabled":true}}`), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	feeCalls := 0
	bm.feeRateFetcher = func(*config.Config, string, string) (float64, float64, error) {
		feeCalls++
		cancel()
		return 0, 0, nil
	}
	candidate := config.BotConfig{ID: "candidate", Exchange: "binance", Symbol: "BTCUSDT", MarketType: "futures"}
	_, err := bm.StartBot(ctx, candidate)
	if !errors.Is(err, context.Canceled) || feeCalls != 1 || len(bm.List()) != 0 {
		t.Fatalf("canceled fee lookup continued startup: %v, calls %d", err, feeCalls)
	}
	candidate.ID = "retry"
	finish, err := bm.reserveBotStartup(candidate)
	if err != nil {
		t.Fatalf("canceled startup leaked reservation: %v", err)
	}
	finish()
}

func TestBotStartupReservationPreservesSpotMarginPolicy(t *testing.T) {
	bm := NewBotManager(&config.Config{}, nil, nil, nil, "")
	finish, err := bm.reserveBotStartup(config.BotConfig{ID: "margin", Exchange: "binance", Symbol: "BTCUSDT", MarketType: "spot", UseSpotMargin: true})
	if err != nil {
		t.Fatal(err)
	}
	defer finish()
	// Existing policy does not make ordinary futures conflict with spot margin.
	// Do not reinterpret canonical "spot_margin" as the config's default futures.
	release, err := bm.reserveBotStartup(config.BotConfig{ID: "future", Exchange: "binance", Symbol: "BTCUSDT", MarketType: "futures"})
	if err != nil {
		t.Fatalf("spot margin reservation changed existing conflict policy: %v", err)
	}
	defer release()
}
