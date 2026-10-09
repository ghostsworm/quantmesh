package main

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"quantmesh/config"
	"quantmesh/event"
	"quantmesh/exchange"
	"quantmesh/storage"
)

var errConstructorCapitalQuery = errors.New("fixture final capital query unavailable")

type capitalQueryConstructorVenue struct {
	*cleanConstructorVenue
	fail                 atomic.Bool
	queries              atomic.Int32
	deadline             bool
	subprecisionResidual atomic.Bool
}

func (v *capitalQueryConstructorVenue) GetPositions(ctx context.Context, symbol string) ([]*exchange.Position, error) {
	// All stream stops occur after the financial stop. Fail only the distinct
	// independent capital-release verifier, never the financial stop itself.
	if v.stops.Load() > 0 {
		v.queries.Add(1)
		if v.subprecisionResidual.Load() && v.constructorRecoveryVenue.market == "futures" {
			return []*exchange.Position{{Symbol: "BTCUSDT", Size: -0.0001}}, nil
		}
		if v.fail.Load() {
			if v.deadline {
				<-ctx.Done()
				return []*exchange.Position{}, nil
			}
			return nil, errConstructorCapitalQuery
		}
	}
	return v.cleanConstructorVenue.GetPositions(ctx, symbol)
}

func TestFundingCarryFullConstructorRetainsCapitalForSubprecisionResidual(t *testing.T) {
	bm := newEnableStateStorage(t)
	bm.eventBus = event.NewEventBus(8)
	t.Cleanup(bm.eventBus.Close)
	cfg := bm.cfg
	cfg.Exchanges = map[string]config.ExchangeConfig{"binance": {APIKey: "fixture-account-identity"}}
	cfg.Timing.PriceSendInterval = 100
	provider := &runtimeLeaseTestLock{}
	venues := map[string]*capitalQueryConstructorVenue{}
	deps := fundingCarryStartupDependencies{
		checkSetup: func(context.Context, *config.Config, string, string) (*exchange.FundingCarryPermissionResult, error) {
			return &exchange.FundingCarryPermissionResult{OK: true, SpotOK: true, FuturesOK: true}, nil
		},
		newExchange: func(_ *config.Config, _, _, market string) (exchange.IExchange, error) {
			venue := &capitalQueryConstructorVenue{cleanConstructorVenue: &cleanConstructorVenue{constructorRecoveryVenue: &constructorRecoveryVenue{market: market}}}
			venues[market] = venue
			return venue, nil
		},
	}
	sym := config.SymbolConfig{ID: "subprecision-capital-owner", Exchange: "binance", Symbol: "BTCUSDT", MarketType: config.MarketTypeFundingCarry, TotalAllocatedCapital: 100,
		Strategies: []config.StrategyInstance{{Type: "funding_carry", Weight: 1, Config: map[string]interface{}{"reverse_enabled": true}}}}
	rt, err := startFundingCarrySymbolRuntimeWithDependencies(t.Context(), cfg, sym, bm.eventBus, bm.storageService, provider, nil, nil, deps)
	if err != nil {
		t.Fatal(err)
	}
	bm.AddRuntime(&BotRuntime{BotID: sym.ID, Config: config.SymbolConfigToBotConfig(sym, false), Inner: rt})
	venues["futures"].subprecisionResidual.Store(true)
	if err := bm.StopBot(sym.ID); err == nil {
		t.Fatal("capital release accepted a nonzero futures residual below quantity precision")
	}
	claims, err := bm.storageService.GetStorage().(storage.AccountWalletCapitalReservationReader).ListAccountWalletCapitalReservations(t.Context(), "", "", 10)
	if err != nil || len(claims) != 3 {
		t.Fatalf("subprecision residual released capital reservation: claims=%d err=%v", len(claims), err)
	}
	venues["futures"].subprecisionResidual.Store(false)
	if err := bm.StopBot(sym.ID); err != nil {
		t.Fatal("flat retry could not release retained capital", err)
	}
	claims, err = bm.storageService.GetStorage().(storage.AccountWalletCapitalReservationReader).ListAccountWalletCapitalReservations(t.Context(), "", "", 10)
	if err != nil || len(claims) != 0 {
		t.Fatalf("verified-flat retry did not release capital: claims=%d err=%v", len(claims), err)
	}
}

func TestFundingCarryFullConstructorCapitalVerificationRetriesWithoutFinancialReplay(t *testing.T) {
	for _, scenario := range []struct{ all, deadline bool }{{}, {all: true}, {all: true, deadline: true}} {
		name := map[bool]string{false: "StopBot", true: "StopAll"}[scenario.all]
		if scenario.deadline {
			name += "_deadline"
		}
		t.Run(name, func(t *testing.T) {
			bm := newEnableStateStorage(t)
			bm.eventBus = event.NewEventBus(8)
			t.Cleanup(bm.eventBus.Close)
			cfg := bm.cfg
			cfg.Exchanges = map[string]config.ExchangeConfig{"binance": {APIKey: "fixture-account-identity"}}
			cfg.Timing.PriceSendInterval = 100
			provider := &runtimeLeaseTestLock{}
			venues := map[string]*capitalQueryConstructorVenue{}
			deps := fundingCarryStartupDependencies{
				checkSetup: func(context.Context, *config.Config, string, string) (*exchange.FundingCarryPermissionResult, error) {
					return &exchange.FundingCarryPermissionResult{OK: true, SpotOK: true, FuturesOK: true}, nil
				},
				newExchange: func(_ *config.Config, _, _, market string) (exchange.IExchange, error) {
					v := &capitalQueryConstructorVenue{cleanConstructorVenue: &cleanConstructorVenue{constructorRecoveryVenue: &constructorRecoveryVenue{market: market}}, deadline: scenario.deadline}
					venues[market] = v
					return v, nil
				},
			}
			ctx, cancel := context.WithCancel(t.Context())
			t.Cleanup(cancel)
			sym := config.SymbolConfig{ID: "capital-retry-owner", Exchange: "binance", Symbol: "BTCUSDT", MarketType: config.MarketTypeFundingCarry, TotalAllocatedCapital: 100,
				Strategies: []config.StrategyInstance{{Type: "funding_carry", Weight: 1, Config: map[string]interface{}{"reverse_enabled": true}}}}
			rt, err := startFundingCarrySymbolRuntimeWithDependencies(ctx, cfg, sym, bm.eventBus, bm.storageService, provider, nil, nil, deps)
			if err != nil {
				t.Fatal(err)
			}
			br := &BotRuntime{BotID: sym.ID, Config: config.SymbolConfigToBotConfig(sym, false), Inner: rt}
			bm.AddRuntime(br)
			stop := func() error { return bm.StopBot(br.BotID) }
			if scenario.all {
				stop = bm.StopAll
			}
			venues["futures"].fail.Store(true)
			wantErr := errConstructorCapitalQuery
			if scenario.deadline {
				wantErr = context.DeadlineExceeded
			}
			if err := stop(); !errors.Is(err, wantErr) {
				t.Fatal("capital verification fault absent", err)
			}
			original := rt.shutdownCloseUnverified.Load()
			if original == nil {
				t.Fatal("capital failure not retained")
			}
			if !br.stopVerificationPending.Load() {
				t.Fatal("capital proof lost controller pending state")
			}
			stateStore := bm.storageService.GetStorage().(storage.StrategyRuntimeStateStore)
			before, err := stateStore.GetStrategyRuntimeState(br.BotID, "funding_carry")
			if err != nil || before == nil {
				t.Fatal("durable flat state missing", err)
			}
			claims, err := bm.storageService.GetStorage().(storage.AccountWalletCapitalReservationReader).ListAccountWalletCapitalReservations(t.Context(), "", "", 10)
			if err != nil || len(claims) != 3 {
				t.Fatal("unverified capital released", err)
			}
			if err := stop(); err == nil || rt.shutdownCloseUnverified.Load() != original {
				t.Fatal("still-failing verifier lost original evidence", err)
			}
			if venues["futures"].queries.Load() != 2 {
				t.Error("independent capital verifier was not retried")
			}
			after, err := stateStore.GetStrategyRuntimeState(br.BotID, "funding_carry")
			if err != nil || after == nil || after.Payload != before.Payload || after.SchemaVersion != before.SchemaVersion {
				t.Fatal("failed readonly proof rewrote durable state", err)
			}
			venues["futures"].fail.Store(false)
			if err := stop(); err != nil {
				t.Fatal("recovered independent proof remains suspended", err)
			}
			claims, err = bm.storageService.GetStorage().(storage.AccountWalletCapitalReservationReader).ListAccountWalletCapitalReservations(t.Context(), "", "", 10)
			if err != nil || len(claims) != 0 || rt.shutdownCloseUnverified.Load() != nil {
				t.Fatal("recovered proof did not release verified capital", err)
			}
			if _, ok := bm.Get(br.BotID); ok {
				t.Fatal("completed stop retained controller")
			}
			provider.mu.Lock()
			held := len(provider.held)
			provider.mu.Unlock()
			if held != 0 {
				t.Fatal("completed stop retained leases")
			}
			for _, v := range venues {
				if v.stops.Load() != 1 || v.mutations.Load() != 0 {
					t.Fatal("capital retry replayed financial or stream operations")
				}
				v.handedOver.Store(true)
			}
			if err := rt.StopWithError(); err != nil {
				t.Fatal("completed stop re-read peer scope", err)
			}
		})
	}
}
