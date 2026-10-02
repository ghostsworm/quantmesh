package main

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"testing"

	"quantmesh/exchange"
	"quantmesh/execution"
	"quantmesh/order"
	"quantmesh/storage"
	"quantmesh/web"
)

type capitalReleaseSpotVenue struct {
	*capitalReleaseRuntimeVenue
	inventory      float64
	inventoryErr   error
	inventoryCalls int
	onInventory    func()
}

func (*capitalReleaseSpotVenue) GetMarketType() string { return "spot" }
func (*capitalReleaseSpotVenue) GetBaseAsset() string  { return "BTC" }
func (v *capitalReleaseSpotVenue) SpotInventoryQty(context.Context) (float64, error) {
	v.inventoryCalls++
	if v.onInventory != nil {
		v.onInventory()
	}
	return v.inventory, v.inventoryErr
}

type capitalReleaseMarginVenue struct {
	*capitalReleaseSpotVenue
	debtErr   error
	debtCalls int
	onDebt    func()
}

func (*capitalReleaseMarginVenue) GetMarketType() string { return "spot_margin" }
func (v *capitalReleaseMarginVenue) VerifySpotMarginAccountFlat(context.Context) error {
	v.debtCalls++
	if v.onDebt != nil {
		v.onDebt()
	}
	return v.debtErr
}

type capitalReleaseMissingSpotReader struct{ *capitalReleaseRuntimeVenue }

func (*capitalReleaseMissingSpotReader) GetMarketType() string { return "spot" }

type capitalReleaseMissingMarginVerifier struct{ *capitalReleaseSpotVenue }

func (*capitalReleaseMissingMarginVerifier) GetMarketType() string { return "spot_margin" }

func TestRuntimeStrategyCapitalReleaseSpotUsesTotalInventory(t *testing.T) {
	for _, all := range []bool{false, true} {
		for _, scenario := range []string{"flat", "inventory remains", "invalid inventory", "query failure", "open order", "nil order item", "nil orders", "cancelled inventory read"} {
			t.Run(map[bool]string{false: "single/", true: "all/"}[all]+scenario, func(t *testing.T) {
				base := &capitalReleaseRuntimeVenue{runtimeJournalVenue: &runtimeJournalVenue{}}
				venue := &capitalReleaseSpotVenue{capitalReleaseRuntimeVenue: base}
				rt, _ := capitalReleaseRuntimeFixtureWithVenue(t, venue)
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				switch scenario {
				case "inventory remains":
					venue.inventory = 0.001
				case "invalid inventory":
					venue.inventory = math.NaN()
				case "query failure":
					venue.inventoryErr = errors.New("inventory unavailable")
				case "open order":
					venue.orders = []*exchange.Order{{Symbol: "BTCUSDT", OrderID: 17, Status: exchange.OrderStatusNew}}
				case "nil order item":
					venue.orders = append(venue.orders, nil)
				case "nil orders":
					venue.ordersNil = true
				case "cancelled inventory read":
					venue.onInventory = cancel
				}
				provider := newRuntimeStrategyCapitalProvider(nil, func() []*SymbolRuntime { return []*SymbolRuntime{rt} }).(web.VerifiedStrategyCapitalProvider)
				var released float64
				var err error
				if all {
					var amounts map[string]float64
					amounts, err = provider.ReleaseAllVerifiedCapital(ctx)
					released = amounts["dca"]
				} else {
					released, err = provider.ReleaseVerifiedCapital(ctx, "dca")
				}
				wantReleased := 0.0
				if scenario == "flat" {
					wantReleased = 200
				}
				if (err == nil) != (scenario == "flat") || released != wantReleased || rt.StrategyManager.GetCapitalAllocator().GetUsed("dca") != 200-wantReleased {
					t.Fatalf("spot release wrong: released=%v err=%v", released, err)
				}
				if venue.inventoryCalls != 1 || venue.lastReadSymbol != "" {
					t.Fatal("spot release did not use total inventory, or queried generic futures positions")
				}
				if scenario == "cancelled inventory read" && !errors.Is(err, context.Canceled) {
					t.Fatalf("cancellation lost: %v", err)
				}
			})
		}
	}
}

func TestRuntimeStrategyCapitalReleaseMarginUsesAccountWideDebtProof(t *testing.T) {
	for _, scenario := range []string{"flat", "debt remains", "debt query failure", "cancelled debt read"} {
		t.Run(scenario, func(t *testing.T) {
			base := &capitalReleaseRuntimeVenue{runtimeJournalVenue: &runtimeJournalVenue{}}
			venue := &capitalReleaseMarginVenue{capitalReleaseSpotVenue: &capitalReleaseSpotVenue{capitalReleaseRuntimeVenue: base}}
			rt, _ := capitalReleaseRuntimeFixtureWithVenue(t, venue)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			switch scenario {
			case "debt remains":
				venue.debtErr = errors.New("account still has borrowed assets")
			case "debt query failure":
				venue.debtErr = errors.New("account debt query unavailable")
			case "cancelled debt read":
				venue.onDebt = cancel
			}
			amounts, err := releaseRuntimeStrategyCapital(ctx, []*SymbolRuntime{rt}, "")
			wantReleased := 0.0
			if scenario == "flat" {
				wantReleased = 200
			}
			if (err == nil) != (scenario == "flat") || amounts["dca"] != wantReleased || rt.StrategyManager.GetCapitalAllocator().GetUsed("dca") != 200-wantReleased {
				t.Fatalf("margin release wrong: released=%v err=%v", amounts, err)
			}
			if venue.debtCalls != 1 || venue.inventoryCalls != 0 || venue.lastReadSymbol != "" {
				t.Fatal("margin release bypassed account-wide debt proof or substituted futures/spot-only evidence")
			}
			if scenario == "cancelled debt read" && !errors.Is(err, context.Canceled) {
				t.Fatalf("cancellation lost: %v", err)
			}
		})
	}
}

func TestRuntimeStrategyCapitalReleaseRejectsMissingMarketEvidence(t *testing.T) {
	base := &capitalReleaseRuntimeVenue{runtimeJournalVenue: &runtimeJournalVenue{}}
	spot := &capitalReleaseMissingSpotReader{capitalReleaseRuntimeVenue: base}
	margin := &capitalReleaseMissingMarginVerifier{capitalReleaseSpotVenue: &capitalReleaseSpotVenue{capitalReleaseRuntimeVenue: base}}
	for _, venue := range []exchange.IExchange{spot, margin} {
		t.Run(venue.GetMarketType(), func(t *testing.T) {
			// These concrete wrappers implement IExchange via the embedded venue,
			// but deliberately omit the required market-specific proof capability.
			rt, _ := capitalReleaseRuntimeFixtureWithVenue(t, venue)
			amounts, err := releaseRuntimeStrategyCapital(t.Context(), []*SymbolRuntime{rt}, "dca")
			if err == nil || amounts["dca"] != 0 || rt.StrategyManager.GetCapitalAllocator().GetUsed("dca") != 200 {
				t.Fatalf("missing market evidence released capital: amounts=%v err=%v", amounts, err)
			}
		})
	}
}

func TestRuntimeStrategyCapitalReleaseRechecksDurableIntentAfterVenueRead(t *testing.T) {
	rt, venue, store := capitalReleaseRuntimeFixture(t)
	venue.onRead = func(context.Context) {
		writeCapitalReleasePreparedIntent(t, store, rt.capitalReleaseScope)
	}
	amounts, err := releaseRuntimeStrategyCapital(t.Context(), []*SymbolRuntime{rt}, "dca")
	if !errors.Is(err, execution.ErrOrderUnknown) || amounts["dca"] != 0 || rt.StrategyManager.GetCapitalAllocator().GetUsed("dca") != 200 {
		t.Fatalf("late durable hold ignored: released=%v err=%v", amounts, err)
	}
}

func writeCapitalReleasePreparedIntent(t *testing.T, store *storage.SQLStorage, scope execution.IntentScope) {
	t.Helper()
	const cid = "late-prepared"
	payload, err := json.Marshal(map[string]interface{}{
		"Version": 1, "Scope": scope, "Opening": true,
		"Request": order.OrderRequest{ClientOrderID: cid, Symbol: scope.Symbol, Side: "BUY", Price: 100, Quantity: 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	key, err := scope.Key()
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveExecutionIntent(t.Context(), key, cid, 0, payload); err != nil {
		t.Fatal(err)
	}
}
