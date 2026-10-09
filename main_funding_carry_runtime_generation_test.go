package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"testing"

	"quantmesh/config"
	"quantmesh/exchange"
	"quantmesh/lock"
	"quantmesh/storage"
)

func newFundingCarryGenerationTestStorage(t *testing.T) (*storage.StorageService, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "runtime-generation.db")
	cfg := &config.Config{}
	cfg.Storage.Enabled = true
	cfg.Storage.Type = "sqlite"
	cfg.Storage.Path = path
	service, err := storage.NewStorageService(cfg, t.Context())
	if err != nil {
		t.Fatal("open runtime-generation storage:", err)
	}
	t.Cleanup(func() { _ = service.GetStorage().Close() })
	return service, path
}

func TestFundingCarryRuntimeGenerationAdapterFencesSaveAndCAS(t *testing.T) {
	service, _ := newFundingCarryGenerationTestStorage(t)
	store := service.GetStorage().(storage.FundingCarryRuntimeGenerationStore)
	scopes := []string{fmt.Sprintf("%064x", 11), fmt.Sprintf("%064x", 12)}
	first, err := store.ClaimFundingCarryRuntimeGeneration(t.Context(), scopes)
	if err != nil {
		t.Fatal("first claim:", err)
	}
	newAdapter := func(generation storage.FundingCarryRuntimeGeneration) *fundingCarryRuntimeStateAdapter {
		return &fundingCarryRuntimeStateAdapter{
			strategyRuntimeStateAdapter: &strategyRuntimeStateAdapter{storageService: service, botID: "adapter-fencing"},
			generation:                  generation,
		}
	}
	oldAdapter := newAdapter(first)
	initial := `{"stable":"adapter"}`
	if err := oldAdapter.SaveRuntimeState("funding_carry", 4, initial); err != nil {
		t.Fatal("initial adapter Save:", err)
	}
	second, err := store.ClaimFundingCarryRuntimeGeneration(t.Context(), scopes)
	if err != nil {
		t.Fatal("replacement claim:", err)
	}
	stalePayload := `{"stale":"adapter-save"}`
	if err := oldAdapter.SaveRuntimeState("funding_carry", 4, stalePayload); !errors.Is(err, storage.ErrFundingCarryRuntimeGenerationLost) {
		t.Fatalf("old adapter Save error = %v, want generation lost", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	if saved, err := oldAdapter.CompareAndSwapRuntimeState(ctx, "funding_carry", 4, initial, 4, `{"stale":"adapter-cas"}`); !errors.Is(err, storage.ErrFundingCarryRuntimeGenerationLost) || saved {
		t.Fatalf("old adapter CAS = saved %v, err %v", saved, err)
	}
	state, err := service.GetStorage().(storage.StrategyRuntimeStateStore).GetStrategyRuntimeState("adapter-fencing", "funding_carry")
	if err != nil || state == nil || state.Payload != initial {
		t.Fatalf("old adapter altered canonical payload: %+v err=%v", state, err)
	}
	currentAdapter := newAdapter(second)
	if err := currentAdapter.SaveRuntimeState("funding_carry", 4, `{"current":"adapter-save"}`); err != nil {
		t.Fatal("current adapter Save:", err)
	}
	if saved, err := currentAdapter.CompareAndSwapRuntimeState(ctx, "funding_carry", 4, `{"current":"adapter-save"}`, 4, `{"current":"adapter-cas"}`); err != nil || !saved {
		t.Fatalf("current adapter CAS = saved %v, err %v", saved, err)
	}
}

func TestFundingCarryGenerationClaimFailureDoesNotCreateExchange(t *testing.T) {
	service, path := newFundingCarryGenerationTestStorage(t)
	cfg := &config.Config{Exchanges: map[string]config.ExchangeConfig{
		"binance": {APIKey: "test-key", SecretKey: "test-secret"},
	}}
	symbol := config.SymbolConfig{ID: "claim-failure-bot", Exchange: "binance", Symbol: "BTCUSDT", MarketType: config.MarketTypeFundingCarry}
	scopes, err := fundingCarryRuntimeOwnershipScopes(cfg, symbol.Exchange, symbol.Symbol, false)
	if err != nil {
		t.Fatal("derive complete ownership scopes:", err)
	}
	keys := make([]string, 0, len(scopes))
	for _, scope := range scopes {
		key, err := scope.Key()
		if err != nil {
			t.Fatal("derive scope key:", err)
		}
		keys = append(keys, key)
	}
	store := service.GetStorage().(storage.FundingCarryRuntimeGenerationStore)
	_, err = store.ClaimFundingCarryRuntimeGeneration(t.Context(), keys)
	if err != nil {
		t.Fatal("seed complete owner generation:", err)
	}
	db, err := sql.Open("sqlite3", path+"?_journal_mode=WAL&_synchronous=NORMAL")
	if err != nil {
		t.Fatal("open trigger connection:", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	beforeTokens := make(map[string]string, len(keys))
	for _, key := range keys {
		var token string
		if err := db.QueryRowContext(t.Context(), `SELECT owner_token FROM funding_carry_runtime_generations WHERE scope_key = ?`, key).Scan(&token); err != nil {
			t.Fatal("read seeded generation token:", err)
		}
		beforeTokens[key] = token
	}
	if _, err := db.ExecContext(t.Context(), fmt.Sprintf(`CREATE TRIGGER reject_runtime_generation_claim BEFORE UPDATE ON funding_carry_runtime_generations WHEN OLD.scope_key = '%s' BEGIN SELECT RAISE(ABORT, 'injected claim failure'); END`, keys[len(keys)-1])); err != nil {
		t.Fatal("install deterministic claim failure:", err)
	}
	newExchangeCalls := 0
	result, err := startFundingCarrySymbolRuntimeWithDependencies(t.Context(), cfg, symbol, nil, service, lock.NewNopLock(), nil, nil,
		fundingCarryStartupDependencies{
			checkSetup: func(context.Context, *config.Config, string, string) (*exchange.FundingCarryPermissionResult, error) {
				return &exchange.FundingCarryPermissionResult{OK: true}, nil
			},
			newExchange: func(*config.Config, string, string, string) (exchange.IExchange, error) {
				newExchangeCalls++
				return nil, errors.New("must not create exchange after generation claim failure")
			},
		})
	if err == nil || result != nil {
		t.Fatalf("startup result=%v err=%v, want claim failure", result, err)
	}
	if newExchangeCalls != 0 {
		t.Fatalf("exchange factory called %d times after claim failure", newExchangeCalls)
	}
	for _, key := range keys {
		var token string
		var generation int64
		if err := db.QueryRowContext(t.Context(), `SELECT owner_token, generation FROM funding_carry_runtime_generations WHERE scope_key = ?`, key).Scan(&token, &generation); err != nil {
			t.Fatal("read generation after failed startup:", err)
		}
		if token != beforeTokens[key] || generation != 1 {
			t.Fatalf("partial generation takeover for %s: token=%q generation=%d", key, token, generation)
		}
	}
	states, err := service.GetStorage().(storage.StrategyRuntimeStateLister).ListStrategyRuntimeStates("funding_carry")
	if err != nil || len(states) != 0 {
		t.Fatalf("failed claim created writable runtime state: states=%v err=%v", states, err)
	}
}
