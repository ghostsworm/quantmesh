package main

import (
	"context"
	"errors"
	"math"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"quantmesh/config"
	"quantmesh/exchange"
	"quantmesh/risk"
	"quantmesh/storage"
)

func TestProductionCircuitBreakerRequiresCashFlowReconciliation(t *testing.T) {
	options := circuitBreakerMetricsOptions(nil, nil)
	if !options.RequireCashFlowReconciliation || !options.RequirePersistence {
		t.Fatalf("production feeder must fail closed without reconciled, persisted equity: %+v", options)
	}
}

func TestPersistedEquityStateRevisionAndCorruption(t *testing.T) {
	db, err := storage.NewSQLStorage(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.MigrateRiskCheckpoints(t.Context()); err != nil {
		t.Fatal(err)
	}
	store := &persistedEquityState{backend: db}
	if state, err := store.LoadEquityState(t.Context()); err != nil || state != nil {
		t.Fatalf("missing state: %+v %v", state, err)
	}
	state := risk.EquityCheckpoint{Version: 1, Revision: 1, Scope: "account", Currency: "USDT", BaseAt: time.Now(), HighWater: 1200}
	if err := store.SaveEquityState(t.Context(), 0, state); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.LoadEquityState(t.Context())
	if err != nil || loaded.HighWater != 1200 {
		t.Fatalf("round trip: %+v %v", loaded, err)
	}
	if err := store.SaveEquityState(t.Context(), 0, state); !errors.Is(err, storage.ErrRiskCheckpointConflict) {
		t.Fatalf("overwrite allowed: %v", err)
	}
	if err := db.SaveRiskCheckpoint(t.Context(), circuitEquityCheckpointKey, 1, []byte(`{"revision":1}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.LoadEquityState(t.Context()); err == nil {
		t.Fatal("revision mismatch ignored")
	}
}

func TestEquityAccountScopeIDIsOpaqueAndEnvironmentScoped(t *testing.T) {
	apiKey := "secret-key-prefix-and-suffix"
	live := equityAccountScopeID("binance", config.ExchangeConfig{APIKey: apiKey})
	repeated := equityAccountScopeID("binance", config.ExchangeConfig{APIKey: apiKey})
	testnet := equityAccountScopeID("binance", config.ExchangeConfig{APIKey: apiKey, Testnet: true})
	if len(live) != 64 || live == apiKey || strings.Contains(live, apiKey[:8]) || repeated != live {
		t.Fatalf("account identity is not stable and opaque: %q", live)
	}
	if live == testnet {
		t.Fatal("live and testnet account identities collided")
	}
}

type runtimePnLReaderFixture struct {
	legacyCalls int
	scopedCalls int
	scope       string
	asset       string
}

func (r *runtimePnLReaderFixture) GetPnLBySymbol(string, string, time.Time, time.Time) (*storage.PnLSummary, error) {
	r.legacyCalls++
	return &storage.PnLSummary{TotalPnL: 999}, nil
}

func (r *runtimePnLReaderFixture) GetPnLBySymbolAccountScopeAndAsset(_, scope, _, _, asset string, _, _ time.Time) (*storage.PnLSummary, error) {
	r.scopedCalls++
	r.scope = scope
	r.asset = asset
	return &storage.PnLSummary{TotalPnL: 25}, nil
}

func TestRuntimePnLSummaryUsesExactImmutableAccountScope(t *testing.T) {
	runtime := &SymbolRuntime{
		AccountID:    "opaque-account-id",
		AccountScope: "credential-scope-digest",
		Exchange:     &pnlAssetExchange{asset: "USDT"},
		Config:       config.SymbolConfig{Symbol: "BTCUSDT", Exchange: "binance", MarketType: "futures"},
	}
	reader := &runtimePnLReaderFixture{}
	summary, err := getRuntimePnLSummary(reader, runtime, time.Unix(0, 0), time.Now())
	if err != nil || summary.TotalPnL != 25 || reader.scopedCalls != 1 || reader.legacyCalls != 0 || reader.scope != runtime.AccountScope || reader.asset != "USDT" {
		t.Fatalf("runtime PnL did not use exact account scope: summary=%+v reader=%+v err=%v", summary, reader, err)
	}
}

func TestRuntimePnLSummaryRejectsMissingScopeOrAssetWithoutLegacyQuery(t *testing.T) {
	for _, test := range []struct {
		name  string
		scope string
		asset string
	}{
		{name: "missing immutable scope", asset: "USDT"},
		{name: "missing denomination", scope: "credential-scope-digest"},
	} {
		t.Run(test.name, func(t *testing.T) {
			runtime := &SymbolRuntime{
				AccountID:    "legacy-account-label",
				AccountScope: test.scope,
				Exchange:     &pnlAssetExchange{asset: test.asset},
				Config:       config.SymbolConfig{Symbol: "BTCUSDT", Exchange: "binance", MarketType: "futures"},
			}
			reader := &runtimePnLReaderFixture{}
			if summary, err := getRuntimePnLSummary(reader, runtime, time.Unix(0, 0), time.Now()); err == nil || summary != nil {
				t.Fatalf("runtime without complete ownership/asset proof must fail closed: summary=%+v err=%v", summary, err)
			}
			if reader.scopedCalls != 0 || reader.legacyCalls != 0 {
				t.Fatalf("unsafe ledger query was attempted: %+v", reader)
			}
		})
	}
}

type pnlAssetExchange struct {
	exchange.IExchange
	asset string
}

func (e *pnlAssetExchange) GetQuoteAsset() string { return e.asset }

type equityAccountExchange struct {
	exchange.IExchange
	balance float64
	asset   string
	err     error
	calls   int
}

func (e *equityAccountExchange) GetAccount(context.Context) (*exchange.Account, error) {
	e.calls++
	return &exchange.Account{TotalMarginBalance: e.balance, BalanceAsset: e.asset}, e.err
}

func TestRuntimeEquityScopeIsolationAndUnavailableAccounts(t *testing.T) {
	first := &equityAccountExchange{balance: 1000, asset: "USDT"}
	second := &equityAccountExchange{balance: 200, asset: "USDT"}
	a := &SymbolRuntime{AccountID: "a", Exchange: first, Config: config.SymbolConfig{Exchange: "binance", MarketType: "futures"}}
	b := &SymbolRuntime{AccountID: "b", Exchange: second, Config: config.SymbolConfig{Exchange: "binance", MarketType: "futures"}}
	a.AccountMarketType, b.AccountMarketType = "futures", "futures"
	a.AccountScope, b.AccountScope = "fixture-account-a", "fixture-account-b"
	o, err := observeRuntimeEquity(t.Context(), []*SymbolRuntime{a, a, b})
	if err != nil || o.Equity != 1200 || first.calls != 1 || second.calls != 1 || o.CashFlowComplete {
		t.Fatalf("scope/dedup/coverage: %+v %v", o, err)
	}
	reordered, err := observeRuntimeEquity(t.Context(), []*SymbolRuntime{b, a})
	if err != nil || reordered.Scope != o.Scope {
		t.Fatalf("order changed scope: %+v %v", reordered, err)
	}
	b.AccountScope = equityAccountScopeID("binance", config.ExchangeConfig{APIKey: "test-fixture", Testnet: true})
	changed, err := observeRuntimeEquity(t.Context(), []*SymbolRuntime{a, b})
	if err != nil || changed.Scope == o.Scope {
		t.Fatal("testnet membership did not change scope")
	}
	if b.AccountScope == equityAccountScopeID("binance", config.ExchangeConfig{APIKey: "test-fixture", Testnet: false}) {
		t.Fatal("testnet/live credentials share equity scope")
	}
	for _, bad := range []float64{math.NaN(), math.Inf(1)} {
		second.balance = bad
		if _, err := observeRuntimeEquity(t.Context(), []*SymbolRuntime{a, b}); err == nil {
			t.Fatal("invalid account silently included")
		}
	}
	second.balance, second.err = 200, errors.New("account unavailable")
	if _, err := observeRuntimeEquity(t.Context(), []*SymbolRuntime{a, b}); err == nil {
		t.Fatal("partial account total accepted")
	}
	second.err = nil
	for _, asset := range []string{"", "BTC", "USD"} {
		second.asset = asset
		if _, err := observeRuntimeEquity(t.Context(), []*SymbolRuntime{a, b}); err == nil {
			t.Fatalf("unverified account unit %q accepted", asset)
		}
	}
	second.asset = "USDT"
	b.AccountMarketType = "spot"
	if _, err := observeRuntimeEquity(t.Context(), []*SymbolRuntime{a, b}); err == nil {
		t.Fatal("spot silently omitted from global scope")
	}
	if _, err := observeRuntimeEquity(t.Context(), nil); !errors.Is(err, risk.ErrEquityUnavailable) {
		t.Fatalf("missing accounts: %v", err)
	}
}
