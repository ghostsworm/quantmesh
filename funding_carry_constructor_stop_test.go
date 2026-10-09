package main

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"quantmesh/config"
	"quantmesh/exchange"
	"quantmesh/storage"
)

type cleanConstructorVenue struct {
	*constructorRecoveryVenue
	handedOver atomic.Bool
	reads      atomic.Int32
}

func (v *cleanConstructorVenue) read() error {
	v.reads.Add(1)
	if v.handedOver.Load() {
		return errors.New("fixture rejects reads of scope handed to peer")
	}
	return nil
}
func (v *cleanConstructorVenue) GetAccount(ctx context.Context) (*exchange.Account, error) {
	if err := v.read(); err != nil {
		return nil, err
	}
	return v.constructorRecoveryVenue.GetAccount(ctx)
}
func (v *cleanConstructorVenue) GetPositions(context.Context, string) ([]*exchange.Position, error) {
	if err := v.read(); err != nil {
		return nil, err
	}
	return []*exchange.Position{}, nil
}
func (v *cleanConstructorVenue) GetOpenOrders(context.Context, string) ([]*exchange.Order, error) {
	if err := v.read(); err != nil {
		return nil, err
	}
	return []*exchange.Order{}, nil
}
func (v *cleanConstructorVenue) GetAccountOpenOrders(context.Context) ([]*exchange.Order, error) {
	if err := v.read(); err != nil {
		return nil, err
	}
	return []*exchange.Order{}, nil
}
func (v *cleanConstructorVenue) GetBalance(_ context.Context, asset string) (float64, error) {
	if err := v.read(); err != nil {
		return 0, err
	}
	if asset == "USDT" {
		return 1000, nil
	}
	return 0, nil
}
func (*cleanConstructorVenue) GetFundingInfo(context.Context, string) (*exchange.FundingInfo, error) {
	return nil, errors.New("fixture intentionally supplies no trading signal")
}

type constructorStopLeaseLock struct {
	*runtimeLeaseTestLock
	armed        atomic.Bool
	failedKey    atomic.Pointer[string]
	leaseUnlocks atomic.Int32
}

func (l *constructorStopLeaseLock) Unlock(ctx context.Context, key string) error {
	if strings.HasPrefix(key, "runtime-owner:") {
		l.leaseUnlocks.Add(1)
		if l.armed.CompareAndSwap(true, false) {
			l.failedKey.Store(&key)
			return errors.New("fixture release failed before mutation")
		}
	}
	return l.runtimeLeaseTestLock.Unlock(ctx, key)
}
func TestFundingCarryFullConstructorStopRetriesOnlyRemainingLease(t *testing.T) {
	bm := newEnableStateStorage(t)
	cfg := bm.cfg
	cfg.Exchanges = map[string]config.ExchangeConfig{"binance": {APIKey: "fixture-account-identity"}}
	cfg.Timing.PriceSendInterval = 100
	provider := &constructorStopLeaseLock{runtimeLeaseTestLock: &runtimeLeaseTestLock{}}
	venues := map[string]*cleanConstructorVenue{}
	deps := fundingCarryStartupDependencies{
		checkSetup: func(context.Context, *config.Config, string, string) (*exchange.FundingCarryPermissionResult, error) {
			return &exchange.FundingCarryPermissionResult{OK: true, SpotOK: true, FuturesOK: true}, nil
		},
		newExchange: func(_ *config.Config, _, _, market string) (exchange.IExchange, error) {
			v := &cleanConstructorVenue{constructorRecoveryVenue: &constructorRecoveryVenue{market: market}}
			venues[market] = v
			return v, nil
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	sym := config.SymbolConfig{ID: "owner", Exchange: "binance", Symbol: "BTCUSDT", MarketType: config.MarketTypeFundingCarry, TotalAllocatedCapital: 100,
		Strategies: []config.StrategyInstance{{Type: "funding_carry", Weight: 1, Config: map[string]interface{}{"reverse_enabled": true}}}}
	rt, err := startFundingCarrySymbolRuntimeWithDependencies(ctx, cfg, sym, nil, bm.storageService, provider, nil, nil, deps)
	if err != nil || rt == nil {
		t.Fatalf("clean complete constructor failed: %v", err)
	}
	t.Cleanup(func() { _ = rt.StopWithError() })
	provider.armed.Store(true)
	if err := rt.StopWithError(); err == nil || !isRetryableRuntimeOwnershipRelease(err) || rt.shutdownCloseUnverifiedReason() != "" {
		t.Fatalf("actual stop did not expose pure lease release retry: %v", err)
	}
	claims, err := bm.storageService.GetStorage().(storage.AccountWalletCapitalReservationReader).ListAccountWalletCapitalReservations(t.Context(), "", "", 10)
	if err != nil || len(claims) != 0 {
		t.Fatalf("actual flat verifier did not release capital: %v", err)
	}
	stateStore := bm.storageService.GetStorage().(storage.StrategyRuntimeStateStore)
	saved, err := stateStore.GetStrategyRuntimeState("owner", "funding_carry")
	if err != nil || saved == nil || saved.SchemaVersion != 8 {
		t.Fatalf("actual stop lost durable state: %v", err)
	}
	var state struct {
		Direction    int     `json:"direction"`
		OwnedSpot    float64 `json:"owned_spot"`
		OwnedFutures float64 `json:"owned_futures"`
		Debt         float64 `json:"margin_debt"`
		Unknown      bool    `json:"exposure_unknown"`
		Pending      bool    `json:"intent_in_flight"`
		Known        bool    `json:"ownership_ready"`
	}
	if err := json.Unmarshal([]byte(saved.Payload), &state); err != nil {
		t.Fatal(err)
	}
	if !state.Known || state.Direction != 0 || state.OwnedSpot != 0 || state.OwnedFutures != 0 || state.Debt != 0 || state.Unknown || state.Pending {
		t.Fatal("actual stop released capital without durable flat evidence")
	}
	account := equityAccountScopeID("binance", cfg.Exchanges["binance"])
	failed := provider.failedKey.Load()
	if failed == nil {
		t.Fatal("fixture did not inject a runtime lease failure")
	}
	for _, market := range []string{"futures", "spot", "spot_margin"} {
		scope := runtimeOwnershipScope(account, "binance", market, "BTCUSDT")
		key, err := scope.Key()
		if err != nil {
			t.Fatal(err)
		}
		peer, err := acquireRuntimeOwnershipLease(t.Context(), provider, scope, time.Second, nil)
		if "runtime-owner:"+key == *failed {
			if err == nil {
				t.Cleanup(func() { _ = peer.Release() })
				t.Fatal("failed leg surrendered ownership")
			}
			continue
		}
		if err != nil {
			t.Fatal("released leg unavailable to peer", err)
		}
		t.Cleanup(func() { _ = peer.Release() })
	}
	var before int32
	for _, v := range venues {
		before += v.reads.Load()
		v.handedOver.Store(true)
	}
	if err := rt.StopWithError(); err != nil {
		t.Fatal(err)
	}
	var after int32
	for _, v := range venues {
		after += v.reads.Load()
		if v.mutations.Load() != 0 || v.stops.Load() != 1 {
			t.Fatal("lease retry repeated financial or stream cleanup")
		}
	}
	if before != after || provider.leaseUnlocks.Load() != 4 {
		t.Fatal("actual retry reread peer scopes or repeated released leases")
	}
	retained, err := stateStore.GetStrategyRuntimeState("owner", "funding_carry")
	if err != nil || retained == nil || retained.Payload != saved.Payload {
		t.Fatal("lease retry rewrote verified financial state")
	}
}
