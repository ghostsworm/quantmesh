package main

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"quantmesh/exchange"
	"quantmesh/execution"
	"quantmesh/lock"
	"quantmesh/order"
	"quantmesh/position"
	"quantmesh/storage"
)

type runtimeJournalVenue struct {
	exchange.IExchange
	mu           sync.Mutex
	positions    []*exchange.Position
	orders       []*exchange.Order
	liveOrders   map[int64]*exchange.Order
	positionErr  error
	orderErr     error
	positionsNil bool
	ordersNil    bool
	getOrderErr  error
	sends        int
}

type spotRuntimeJournalVenue struct {
	*runtimeJournalVenue
	totalInventory float64
	inventoryErr   error
}

func (*spotRuntimeJournalVenue) GetMarketType() string { return "spot" }
func (*spotRuntimeJournalVenue) GetBaseAsset() string  { return "BTC" }
func (v *spotRuntimeJournalVenue) SpotInventoryQty(context.Context) (float64, error) {
	return v.totalInventory, v.inventoryErr
}

func (*runtimeJournalVenue) GetName() string       { return "fake" }
func (*runtimeJournalVenue) GetMarketType() string { return "futures" }
func (v *runtimeJournalVenue) GetPositions(context.Context, string) ([]*exchange.Position, error) {
	if v.positionsNil {
		return nil, v.positionErr
	}
	if v.positions == nil && v.positionErr == nil {
		return []*exchange.Position{}, nil
	}
	return v.positions, v.positionErr
}
func (v *runtimeJournalVenue) GetOpenOrders(context.Context, string) ([]*exchange.Order, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.orderErr != nil {
		return nil, v.orderErr
	}
	if v.ordersNil {
		return nil, nil
	}
	result := make([]*exchange.Order, 0, len(v.orders)+len(v.liveOrders))
	result = append(result, v.orders...)
	for _, o := range v.liveOrders {
		if o.Status != exchange.OrderStatusFilled && o.Status != exchange.OrderStatusCanceled && o.Status != exchange.OrderStatusRejected && o.Status != exchange.OrderStatusExpired {
			copy := *o
			result = append(result, &copy)
		}
	}
	return result, nil
}
func (v *runtimeJournalVenue) PlaceOrder(_ context.Context, req *exchange.OrderRequest) (*exchange.Order, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.sends++
	placed := &exchange.Order{OrderID: int64(v.sends), ClientOrderID: req.ClientOrderID, Symbol: req.Symbol, Side: req.Side,
		Quantity: req.Quantity, Price: req.Price, Status: exchange.OrderStatusNew}
	if v.liveOrders == nil {
		v.liveOrders = make(map[int64]*exchange.Order)
	}
	v.liveOrders[placed.OrderID] = placed
	return placed, nil
}

func (v *runtimeJournalVenue) GetOrder(_ context.Context, _ string, id int64) (*exchange.Order, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.getOrderErr != nil {
		return nil, v.getOrderErr
	}
	if placed := v.liveOrders[id]; placed != nil {
		copy := *placed
		return &copy, nil
	}
	return nil, errors.New("order not found")
}

func (v *runtimeJournalVenue) CancelOrder(_ context.Context, _ string, id int64) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if placed := v.liveOrders[id]; placed != nil {
		placed.Status = exchange.OrderStatusCanceled
	}
	return nil
}

func runtimeJournalScope() execution.IntentScope {
	return execution.IntentScope{Account: "account-full-identity", Exchange: "fake", Market: "futures", Symbol: "BTCUSDT", Bot: "a"}
}

func newJournalRuntime(venue exchange.IExchange, scope execution.IntentScope) (*order.ExchangeOrderExecutor, *execution.OpeningGate) {
	executor := order.NewExchangeOrderExecutor(venue, scope.Symbol, 0, 0, lock.NewNopLock(), scope.Bot)
	gate := &execution.OpeningGate{}
	executor.SetOpeningGate(gate, "LONG")
	return executor, gate
}

func TestRuntimeIntentJournalSQLiteRestartDoesNotReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.db")
	store, err := storage.NewSQLStorage(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.MigrateExecutionIntents(t.Context()); err != nil {
		t.Fatal(err)
	}
	scope := runtimeJournalScope()
	venue := &runtimeJournalVenue{}
	executor, gate := newJournalRuntime(venue, scope)
	if err := configureRuntimeIntentJournal(t.Context(), executor, gate, venue, store, scope); err != nil {
		t.Fatal(err)
	}
	req := &order.OrderRequest{Symbol: "BTCUSDT", Side: "BUY", Price: 100, Quantity: 1, ClientOrderID: "pending", StrategyName: "dca"}
	if _, err := executor.PlaceOrder(req); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = storage.NewSQLStorage(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	restarted, restoredGate := newJournalRuntime(venue, scope)
	if err := configureRuntimeIntentJournal(t.Context(), restarted, restoredGate, venue, store, scope); !errors.Is(err, execution.ErrOrderUnknown) {
		t.Fatalf("restart lost unsettled order: %v", err)
	}
	restoredGate.Unblock("opening_manager")
	req.ClientOrderID = "second"
	if _, err := restarted.PlaceOrder(req); !errors.Is(err, execution.ErrOpeningPaused) || venue.sends != 1 {
		t.Fatalf("reopened before recovery: %v sends=%d", err, venue.sends)
	}
	routes := restarted.RecoveredOrderRoutes()
	if len(routes) != 1 || routes[0].StrategyName != "dca" || routes[0].ClientOrderID != "pending" {
		t.Fatalf("scoped route lost: %v", routes)
	}
	// Same symbol and account, different Bot must not inherit this route.
	otherScope := scope
	otherScope.Bot = "b"
	venue.orders = []*exchange.Order{{OrderID: 1, Symbol: "BTCUSDT", ClientOrderID: "pending"}}
	other, otherGate := newJournalRuntime(venue, otherScope)
	if err := configureRuntimeIntentJournal(t.Context(), other, otherGate, venue, store, otherScope); err == nil || !otherGate.Blocked() {
		t.Fatal("foreign account orders bypassed fresh-state verification")
	}
	if len(other.RecoveredOrderRoutes()) != 0 {
		t.Fatal("cross-Bot routing contamination")
	}
	update := &position.OrderUpdate{OrderID: 1, ClientOrderID: "pending", Symbol: "BTCUSDT", Side: "BUY", Status: "PARTIALLY_FILLED", ExecutedQty: 0.5, AvgPrice: 100}
	if observeOwnedRuntimeOrder(other, update) {
		t.Fatal("live same-symbol update leaked to another Bot")
	}
	if !observeOwnedRuntimeOrder(restarted, update) {
		t.Fatal("restored owned update was not accepted")
	}
}

func TestRuntimeIntentJournalFreshOwnerRequiresVerifiedEmptyState(t *testing.T) {
	for _, scenario := range []string{"missing_schema", "missing_storage", "legacy", "position", "order", "out_of_scope_order", "malformed_order", "position_error", "order_error", "orders_nil", "flat"} {
		t.Run(scenario, func(t *testing.T) {
			store, err := storage.NewSQLStorage(filepath.Join(t.TempDir(), "runtime.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			if scenario != "missing_schema" {
				if err := store.MigrateExecutionIntents(t.Context()); err != nil {
					t.Fatal(err)
				}
			}
			var backend runtimeIntentBackend = store
			venue := &runtimeJournalVenue{}
			scope := runtimeJournalScope()
			switch scenario {
			case "missing_storage":
				backend = nil
			case "legacy":
				if err := store.SaveOrder(&storage.Order{OrderID: 1, BotID: "a", Account: "a", Exchange: "fake", Symbol: "BTCUSDT", ClientOrderID: "old", Side: "BUY", Status: "FILLED", CreatedAt: time.Now(), UpdatedAt: time.Now()}); err != nil {
					t.Fatal(err)
				}
			case "position":
				venue.positions = []*exchange.Position{{Symbol: "BTCUSDT", Size: 1}}
			case "order":
				venue.orders = []*exchange.Order{{Symbol: "BTCUSDT", OrderID: 10}}
			case "out_of_scope_order":
				venue.orders = []*exchange.Order{{Symbol: "ETHUSDT", OrderID: 10}}
			case "malformed_order":
				venue.orders = []*exchange.Order{{OrderID: 10}}
			case "position_error":
				venue.positionErr = errors.New("offline")
			case "order_error":
				venue.orderErr = errors.New("offline")
			case "orders_nil":
				venue.ordersNil = true
			}
			executor, gate := newJournalRuntime(venue, scope)
			err = configureRuntimeIntentJournal(t.Context(), executor, gate, venue, backend, scope)
			if scenario == "flat" {
				if err != nil || gate.Blocked() {
					t.Fatalf("fresh verified owner blocked: %v", err)
				}
			} else if err == nil || !gate.Blocked() {
				t.Fatalf("unverified startup allowed: %v", err)
			}
			if venue.sends != 0 {
				t.Fatal("bootstrap itself submitted an order")
			}
		})
	}
}

func TestRuntimeIntentJournalSpotBootstrapRequiresVerifiedInventoryAndOrders(t *testing.T) {
	for _, scenario := range []string{"flat", "inventory", "inventory_error", "orders_nil"} {
		t.Run(scenario, func(t *testing.T) {
			store, err := storage.NewSQLStorage(filepath.Join(t.TempDir(), "spot-runtime.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			if err := store.MigrateExecutionIntents(t.Context()); err != nil {
				t.Fatal(err)
			}

			venue := &spotRuntimeJournalVenue{runtimeJournalVenue: &runtimeJournalVenue{}}
			switch scenario {
			case "inventory":
				venue.totalInventory = 0.01
			case "inventory_error":
				venue.inventoryErr = errors.New("inventory unavailable")
			case "orders_nil":
				venue.ordersNil = true
			}
			scope := runtimeJournalScope()
			scope.Market = "spot"
			executor, gate := newJournalRuntime(venue, scope)
			err = configureRuntimeIntentJournal(t.Context(), executor, gate, venue, store, scope)
			if scenario == "flat" {
				if err != nil || gate.Blocked() {
					t.Fatalf("verified empty spot owner blocked: %v", err)
				}
			} else if err == nil || !gate.Blocked() {
				t.Fatalf("unverified spot startup was allowed: %v", err)
			}
			if venue.sends != 0 {
				t.Fatal("spot bootstrap itself submitted an order")
			}
		})
	}
}

func TestSettleVerifiedStrategyIntentOnlyAfterExactOwnerAccounting(t *testing.T) {
	for _, scenario := range []struct {
		name       string
		strategy   string
		queryError bool
	}{
		{name: "matching_owner", strategy: "dca"},
		{name: "mismatched_owner", strategy: "trend_following"},
		{name: "exact_query_failure", strategy: "dca", queryError: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "runtime.db")
			store, err := storage.NewSQLStorage(path)
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			if err := store.MigrateExecutionIntents(t.Context()); err != nil {
				t.Fatal(err)
			}
			scope := runtimeJournalScope()
			venue := &runtimeJournalVenue{}
			if scenario.queryError {
				venue.getOrderErr = errors.New("exact order query unavailable")
			}
			executor, gate := newJournalRuntime(venue, scope)
			if err := configureRuntimeIntentJournal(t.Context(), executor, gate, venue, store, scope); err != nil {
				t.Fatal(err)
			}
			const clientOrderID = "dca-filled"
			if _, err := executor.PlaceOrder(&order.OrderRequest{Symbol: scope.Symbol, Side: "BUY", Price: 100, Quantity: 1,
				ClientOrderID: clientOrderID, StrategyName: "dca"}); err != nil {
				t.Fatal(err)
			}
			venue.mu.Lock()
			venue.liveOrders[1].Status = exchange.OrderStatusFilled
			venue.liveOrders[1].ExecutedQty = 1
			venue.liveOrders[1].AvgPrice = 100
			venue.mu.Unlock()
			update := &position.OrderUpdate{OrderID: 1, ClientOrderID: clientOrderID, Symbol: scope.Symbol, Side: "BUY",
				Status: "FILLED", ExecutedQty: 1, AvgPrice: 100}
			if !observeOwnedRuntimeOrder(executor, update) {
				t.Fatal("terminal owned order update was not durably observed")
			}

			err = settleVerifiedStrategyIntent(t.Context(), executor, gate, scenario.strategy, update)
			if scenario.name == "matching_owner" {
				if err != nil {
					t.Fatalf("exact owning strategy could not settle its accounted fill: %v", err)
				}
				if gate.HasBlock(strategyIntentSettlementBlock) {
					t.Fatal("verified settlement left its failure gate blocked")
				}
				restarted, restartedGate := newJournalRuntime(venue, scope)
				if err := configureRuntimeIntentJournal(t.Context(), restarted, restartedGate, venue, store, scope); err != nil || restartedGate.Blocked() {
					t.Fatalf("settled fill incorrectly blocked restart: err=%v blocked=%t", err, restartedGate.Blocked())
				}
				return
			}
			if err == nil {
				t.Fatal("unverified strategy fill was settled")
			}
			if !gate.HasBlock(strategyIntentSettlementBlock) {
				t.Fatal("unverified terminal fill did not immediately block new openings")
			}
			restarted, restartedGate := newJournalRuntime(venue, scope)
			if err := configureRuntimeIntentJournal(t.Context(), restarted, restartedGate, venue, store, scope); !errors.Is(err, execution.ErrOrderUnknown) {
				t.Fatalf("unsettled mismatched-owner fill did not remain fail-closed on restart: %v", err)
			}
		})
	}
}
