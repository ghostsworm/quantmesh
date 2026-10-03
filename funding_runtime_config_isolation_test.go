package main

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"

	"quantmesh/config"
	"quantmesh/exchange"
	"quantmesh/lock"
)

func TestFundingRuntimeMergeDoesNotOverwriteBaseStrategyConfig(t *testing.T) {
	for _, kind := range []string{"funding_carry", "funding_perp_spread"} {
		t.Run(kind, func(t *testing.T) {
			original := config.StrategyConfig{Enabled: false, Type: kind, Weight: 0.2, Config: map[string]interface{}{"min_funding_rate": 0.01}}
			base := &config.Config{}
			base.Strategies.Configs = map[string]config.StrategyConfig{kind: original, "grid": {Enabled: true, Type: "grid", Weight: 0.8}}
			local := *base
			sym := config.SymbolConfig{Strategies: []config.StrategyInstance{{Type: kind, Weight: 0.7, Config: map[string]interface{}{"min_funding_rate": 0.02}}}}
			mergeFundingRuntimeForTest(kind, &local, sym)
			if !reflect.DeepEqual(base.Strategies.Configs[kind], original) || base.Strategies.Enabled {
				t.Fatal("Bot-local strategy merge mutated base configuration")
			}
			if got := local.Strategies.Configs[kind]; !got.Enabled || got.Weight != 0.7 || got.Config["min_funding_rate"] != 0.02 {
				t.Fatalf("Bot-specific configuration not retained: %+v", got)
			}
			delete(local.Strategies.Configs, "grid")
			if _, ok := base.Strategies.Configs["grid"]; !ok {
				t.Fatal("local map deletion removed another base strategy")
			}
		})
	}
}

func mergeFundingRuntimeForTest(kind string, cfg *config.Config, sym config.SymbolConfig) {
	if kind == "funding_carry" {
		mergeFundingCarryStrategyConfig(cfg, sym)
	} else {
		mergeFundingPerpSpreadStrategyConfig(cfg, sym)
	}
}

func TestFundingCarryFailedConstructorDoesNotMutateBaseConfiguration(t *testing.T) {
	cfg := &config.Config{Exchanges: map[string]config.ExchangeConfig{"binance": {APIKey: "fixture-account-identity"}}}
	original := config.StrategyConfig{Type: "funding_carry", Weight: 0.25, Config: map[string]interface{}{"min_funding_rate": 0.03}}
	cfg.Strategies.Configs = map[string]config.StrategyConfig{"funding_carry": original}
	failure := errors.New("injected factory failure")
	deps := fundingCarryStartupDependencies{
		checkSetup: func(context.Context, *config.Config, string, string) (*exchange.FundingCarryPermissionResult, error) {
			return &exchange.FundingCarryPermissionResult{OK: true}, nil
		},
		newExchange: func(*config.Config, string, string, string) (exchange.IExchange, error) { return nil, failure },
	}
	sym := config.SymbolConfig{Exchange: "binance", Symbol: "BTCUSDT", Strategies: []config.StrategyInstance{{Type: "funding_carry", Weight: 1}}}
	rt, err := startFundingCarrySymbolRuntimeWithDependencies(context.Background(), cfg, sym, nil, nil, lock.NewNopLock(), nil, nil, deps)
	if rt != nil || !errors.Is(err, failure) {
		t.Fatalf("unexpected constructor result: %v", err)
	}
	if !reflect.DeepEqual(cfg.Strategies.Configs["funding_carry"], original) {
		t.Fatal("failed constructor rewrote shared strategy settings")
	}
}

func TestFundingRuntimeConcurrentLocalMergesRemainIndependent(t *testing.T) {
	base := &config.Config{}
	base.Strategies.Configs = map[string]config.StrategyConfig{"grid": {Enabled: true, Type: "grid", Weight: 1}}
	const count = 24
	locals := make([]config.Config, count)
	var group sync.WaitGroup
	for i := range locals {
		locals[i] = *base
		group.Add(1)
		go func(i int) {
			defer group.Done()
			kind := "funding_carry"
			if i%2 != 0 {
				kind = "funding_perp_spread"
			}
			mergeFundingRuntimeForTest(kind, &locals[i], config.SymbolConfig{Strategies: []config.StrategyInstance{{Type: kind, Weight: float64(i + 1)}}})
		}(i)
	}
	group.Wait()
	if len(base.Strategies.Configs) != 1 {
		t.Fatal("concurrent Bot merges added entries to shared map")
	}
	for i := range locals {
		kind := "funding_carry"
		if i%2 != 0 {
			kind = "funding_perp_spread"
		}
		if len(locals[i].Strategies.Configs) != 2 || locals[i].Strategies.Configs[kind].Weight != float64(i+1) {
			t.Fatalf("Bot %d inherited another Bot configuration", i)
		}
	}
}
