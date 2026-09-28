package main

import (
	"context"
	"errors"
	"math"
	"path/filepath"
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
