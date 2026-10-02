package main

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"quantmesh/config"
	"quantmesh/exchange"
	"quantmesh/execution"
	"quantmesh/lock"
	"quantmesh/order"
	"quantmesh/position"
	"quantmesh/storage"
	"quantmesh/strategy"
	"quantmesh/web"
)

type capitalReleaseRuntimeStrategy struct {
	strategy.Strategy
	positions []*strategy.Position
	orders    []*strategy.Order
}

func (s *capitalReleaseRuntimeStrategy) GetPositions() []*strategy.Position { return s.positions }
func (s *capitalReleaseRuntimeStrategy) GetOrders() []*strategy.Order       { return s.orders }

// Controlled fixture proofs, not a substitute for the real strategy durable
// proof regressions in the sibling DCA/Spot/Combo/hedge test files.
func (*capitalReleaseRuntimeStrategy) VerifyCapitalReleaseState(ctx context.Context) error {
	return ctx.Err()
}

func (s *capitalReleaseRuntimeStrategy) CapitalReleaseInventorySnapshot(ctx context.Context) ([]*strategy.Position, []*strategy.Order, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	positions := make([]*strategy.Position, len(s.positions))
	for i, row := range s.positions {
		if row != nil {
			copy := *row
			positions[i] = &copy
		}
	}
	orders := make([]*strategy.Order, len(s.orders))
	for i, row := range s.orders {
		if row != nil {
			copy := *row
			orders[i] = &copy
		}
	}
	return positions, orders, ctx.Err()
}

type capitalReleaseRuntimeVenue struct {
	*runtimeJournalVenue
	onRead         func(context.Context)
	lastReadSymbol string
}

func (v *capitalReleaseRuntimeVenue) GetPositions(ctx context.Context, symbol string) ([]*exchange.Position, error) {
	v.lastReadSymbol = symbol
	if v.onRead != nil {
		v.onRead(ctx)
	}
	return v.runtimeJournalVenue.GetPositions(ctx, symbol)
}

func capitalReleaseRuntimeFixture(t *testing.T) (*SymbolRuntime, *capitalReleaseRuntimeVenue, *storage.SQLStorage) {
	t.Helper()
	venue := &capitalReleaseRuntimeVenue{runtimeJournalVenue: &runtimeJournalVenue{}}
	rt, store := capitalReleaseRuntimeFixtureWithVenue(t, venue)
	return rt, venue, store
}

func capitalReleaseRuntimeFixtureWithVenue(t *testing.T, venue exchange.IExchange) (*SymbolRuntime, *storage.SQLStorage) {
	return capitalReleaseRuntimeFixtureWithLock(t, venue, lock.NewNopLock())
}

func capitalReleaseRuntimeFixtureWithLock(t *testing.T, venue exchange.IExchange, coordinator lock.DistributedLock) (*SymbolRuntime, *storage.SQLStorage) {
	t.Helper()
	scope := runtimeJournalScope()
	scope.Market = venue.GetMarketType()
	executor := order.NewExchangeOrderExecutor(venue, scope.Symbol, 0, 0, coordinator, scope.Bot)
	executor.SetOpeningGate(&execution.OpeningGate{}, "LONG")
	store, err := storage.NewSQLStorage(filepath.Join(t.TempDir(), "capital-release.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.MigrateExecutionIntents(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := executor.ConfigureIntentJournal(t.Context(), store, scope); err != nil {
		t.Fatal(err)
	}
	book, err := execution.NewExposureBook(execution.ExposureLimits{}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := book.Seed(nil); err != nil {
		t.Fatal(err)
	}
	if err := book.ObserveMark(100, time.Now(), time.Now()); err != nil {
		t.Fatal(err)
	}
	executor.SetExposureBook(book)
	manager := strategy.NewStrategyManager(&config.Config{}, 1000)
	manager.RegisterStrategy("dca", &capitalReleaseRuntimeStrategy{}, 1, 0)
	allocator := manager.GetCapitalAllocator()
	allocator.Allocate()
	if !allocator.Reserve("dca", 200) {
		t.Fatal("fixture reserve failed")
	}
	grid := &position.SuperPositionManager{}
	grid.MarkGridRuntimeVenueFlatVerified()
	return &SymbolRuntime{
		Config: config.SymbolConfig{Symbol: scope.Symbol}, AccountMarketType: scope.Market, AccountScope: scope.Account,
		capitalReleaseScope:      scope,
		capitalReleaseWalletLock: lock.NewNopLock(),
		Exchange:                 venue, ExchangeExecutor: executor, StrategyManager: manager, SuperPositionManager: grid,
		CapitalExecutor: strategy.NewMultiStrategyExecutor(executor, allocator),
	}, store
}

func TestRuntimeStrategyCapitalReleaseRequiresMatchingBinding(t *testing.T) {
	for _, mismatch := range []string{"allocator", "executor", "account", "market", "bot", "missing scope", "exchange client", "typed nil exchange"} {
		t.Run(mismatch, func(t *testing.T) {
			rt, venue, _ := capitalReleaseRuntimeFixture(t)
			switch mismatch {
			case "allocator":
				rt.CapitalExecutor = strategy.NewMultiStrategyExecutor(rt.ExchangeExecutor, strategy.NewCapitalAllocator(&config.Config{}, 1000))
			case "executor":
				rt.CapitalExecutor = strategy.NewMultiStrategyExecutor(nil, rt.StrategyManager.GetCapitalAllocator())
			case "account":
				rt.AccountScope = "another-account"
			case "market":
				rt.AccountMarketType = "spot"
			case "bot":
				rt.capitalReleaseScope.Bot = "another-bot"
			case "missing scope":
				rt.capitalReleaseScope = execution.IntentScope{}
			case "exchange client":
				rt.Exchange = &capitalReleaseRuntimeVenue{runtimeJournalVenue: &runtimeJournalVenue{}}
			case "typed nil exchange":
				var missing *capitalReleaseRuntimeVenue
				rt.Exchange = missing
			}
			released, err := releaseRuntimeStrategyCapital(t.Context(), []*SymbolRuntime{rt}, "dca")
			if err == nil || released["dca"] != 0 || rt.StrategyManager.GetCapitalAllocator().GetUsed("dca") != 200 {
				t.Fatalf("mismatched %s proof authorized release: released=%v used=%v err=%v", mismatch, released, rt.StrategyManager.GetCapitalAllocator().GetUsed("dca"), err)
			}
			if venue.lastReadSymbol != "" {
				t.Fatal("mismatched binding reached live venue verification")
			}
		})
	}
}

func TestRuntimeStrategyCapitalReleaseUsesImmutableStartupSymbol(t *testing.T) {
	rt, venue, _ := capitalReleaseRuntimeFixture(t)
	rt.Config.Symbol = "ETHUSDT"
	released, err := releaseRuntimeStrategyCapital(t.Context(), []*SymbolRuntime{rt}, "dca")
	if err != nil || released["dca"] != 200 || venue.lastReadSymbol != "BTCUSDT" {
		t.Fatalf("mutable config changed authoritative owner symbol: released=%v symbol=%s err=%v", released, venue.lastReadSymbol, err)
	}
}

func TestRuntimeStrategyCapitalProviderUsesRealProof(t *testing.T) {
	for _, scenario := range []string{"flat stale capital", "live position", "live order", "nil positions", "unverified grid", "local strategy inventory", "durable journal outage", "missing logical barrier", "proof invalidated by reservation", "cancelled venue read"} {
		t.Run(scenario, func(t *testing.T) {
			rt, venue, store := capitalReleaseRuntimeFixture(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			switch scenario {
			case "live position":
				venue.positions = []*exchange.Position{{Symbol: "BTCUSDT", Size: 1}}
			case "live order":
				venue.orders = []*exchange.Order{{Symbol: "BTCUSDT", OrderID: 12, Status: exchange.OrderStatusNew}}
			case "nil positions":
				venue.positionsNil = true
			case "unverified grid":
				rt.SuperPositionManager = &position.SuperPositionManager{}
			case "local strategy inventory":
				rt.StrategyManager.GetStrategy("dca").(*capitalReleaseRuntimeStrategy).positions = []*strategy.Position{{Symbol: "BTCUSDT", Size: 1}}
			case "durable journal outage":
				if err := store.Close(); err != nil {
					t.Fatal(err)
				}
			case "missing logical barrier":
				rt.CapitalExecutor = nil
			case "proof invalidated by reservation":
				venue.onRead = func(context.Context) { rt.StrategyManager.GetCapitalAllocator().Reserve("dca", 10) }
			case "cancelled venue read":
				venue.onRead = func(context.Context) { cancel() }
			}
			provider := newRuntimeStrategyCapitalProvider(nil, func() []*SymbolRuntime { return []*SymbolRuntime{rt} })
			verified, ok := provider.(web.VerifiedStrategyCapitalProvider)
			if !ok {
				t.Fatal("production factory returned unsafe legacy provider")
			}
			released, err := verified.ReleaseVerifiedCapital(ctx, "dca")
			wantSuccess := scenario == "flat stale capital"
			if (err == nil) != wantSuccess {
				t.Fatalf("release error=%v, wantSuccess=%v", err, wantSuccess)
			}
			wantUsed, wantReleased := 200.0, 0.0
			if wantSuccess {
				wantUsed, wantReleased = 0, 200
			}
			if scenario == "proof invalidated by reservation" {
				wantUsed = 210
			}
			if released != wantReleased || rt.StrategyManager.GetCapitalAllocator().GetUsed("dca") != wantUsed {
				t.Fatalf("unverified release or missing recovery: released=%v used=%v", released, rt.StrategyManager.GetCapitalAllocator().GetUsed("dca"))
			}
			if scenario == "cancelled venue read" && !errors.Is(err, context.Canceled) {
				t.Fatalf("cancellation lost: %v", err)
			}
		})
	}
}

func TestRuntimeStrategyCapitalReleaseReportsPartialAcrossSharedSymbol(t *testing.T) {
	first, _, _ := capitalReleaseRuntimeFixture(t)
	second, secondVenue, _ := capitalReleaseRuntimeFixture(t)
	secondVenue.positions = []*exchange.Position{{Symbol: "BTCUSDT", Size: 1}}
	provider := newRuntimeStrategyCapitalProvider(nil, func() []*SymbolRuntime { return []*SymbolRuntime{first, second} }).(web.VerifiedStrategyCapitalProvider)
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	released, err := provider.ReleaseAllVerifiedCapital(ctx)
	if err == nil || released["dca"] != 200 || first.StrategyManager.GetCapitalAllocator().GetUsed("dca") != 0 || second.StrategyManager.GetCapitalAllocator().GetUsed("dca") != 200 {
		t.Fatalf("partial release misreported: released=%v err=%v", released, err)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("shared-symbol runtime release nested the same lease")
	}
}
