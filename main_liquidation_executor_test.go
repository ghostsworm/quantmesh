package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"quantmesh/config"
	"quantmesh/exchange"
	"quantmesh/execution"
	"quantmesh/lock"
	"quantmesh/order"
	"quantmesh/position"
	"quantmesh/strategy"
)

type liquidationIntentExchange struct {
	sharedGateExchange
	market  string
	sent    []*exchange.OrderRequest
	cancel  context.CancelFunc
	queries int
}

func (v *liquidationIntentExchange) GetMarketType() string { return v.market }
func (*liquidationIntentExchange) GetPriceDecimals() int   { return 2 }
func (v *liquidationIntentExchange) PlaceOrder(ctx context.Context, req *exchange.OrderRequest) (*exchange.Order, error) {
	v.sent = append(v.sent, req)
	if v.cancel != nil {
		v.cancel()
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return &exchange.Order{OrderID: int64(len(v.sent)), ClientOrderID: req.ClientOrderID,
		Symbol: req.Symbol, Side: req.Side, Quantity: req.Quantity, Status: exchange.OrderStatusNew}, nil
}

func (v *liquidationIntentExchange) GetOpenOrders(context.Context, string) ([]*exchange.Order, error) {
	v.queries++
	return nil, nil
}

func newLiquidationIntentManager(t *testing.T, ex *liquidationIntentExchange, multi bool) (*position.SuperPositionManager, position.ContextOrderExecutor, position.ContextBatchOrderExecutor) {
	t.Helper()
	cfg := &config.Config{}
	cfg.Trading.Symbol, cfg.Trading.MarketType, cfg.Trading.Direction = "BTCUSDT", ex.market, "LONG"
	physical := order.NewExchangeOrderExecutor(ex, cfg.Trading.Symbol, 0, 0, lock.NewNopLock(), "test")
	var executor position.OrderExecutorInterface = &exchangeExecutorAdapter{executor: physical, exchange: "fake"}
	if multi {
		allocator := strategy.NewCapitalAllocator(cfg, 1000)
		allocator.RegisterStrategy("grid", 1, 0)
		allocator.Allocate()
		executor = strategy.NewMultiStrategyExecutorAdapter(strategy.NewMultiStrategyExecutor(physical, allocator), "grid")
	}
	spm := position.NewSuperPositionManager(cfg, executor, &positionExchangeAdapter{exchange: ex}, 2, 4)
	physical.SetOpeningGate(spm.OpeningGate(), cfg.Trading.Direction)
	return spm, executor.(position.ContextOrderExecutor), executor.(position.ContextBatchOrderExecutor)
}

func TestLiquidationMarketUsesOwnedExecutor(t *testing.T) {
	for _, market := range []string{"futures", "spot"} {
		for _, multi := range []bool{false, true} {
			for _, side := range []string{"SELL", "BUY"} {
				ex := &liquidationIntentExchange{market: market}
				spm, _, _ := newLiquidationIntentManager(t, ex, multi)
				spm.OpeningGate().Block("test")
				id, err := spm.NewLiquidationVenue(ex).PlaceMarketOrder(t.Context(), "BTCUSDT", side, 0.25, market != "spot")
				if err != nil || id != 1 || len(ex.sent) != 1 {
					t.Fatalf("market=%s multi=%v side=%s: close blocked: id=%d err=%v", market, multi, side, id, err)
				}
				req := ex.sent[0]
				if req.Type != exchange.OrderTypeMarket || req.TimeInForce != "" || req.ClientOrderID == "" || req.Quantity != 0.25 || req.Side != exchange.Side(side) || req.ReduceOnly != (market != "spot") {
					t.Fatalf("lost options/identity: %+v", req)
				}
			}
		}
	}
}

func TestLiquidationMarketCancellationRetainsUnknownAndPreventsRetry(t *testing.T) {
	for _, multi := range []bool{false, true} {
		ctx, cancel := context.WithCancel(t.Context())
		ex := &liquidationIntentExchange{market: "futures", cancel: cancel}
		spm, _, _ := newLiquidationIntentManager(t, ex, multi)
		venue := spm.NewLiquidationVenue(ex)
		_, err := venue.PlaceMarketOrder(ctx, "BTCUSDT", "SELL", 1, true)
		cancel()
		if !errors.Is(err, execution.ErrOrderUnknown) || !spm.IsOpeningPaused() || len(ex.sent) != 1 || ex.queries != 0 {
			t.Fatalf("lost uncertain close: multi=%v err=%v", multi, err)
		}
		_, err = venue.PlaceMarketOrder(t.Context(), "BTCUSDT", "SELL", 1, true)
		if !errors.Is(err, execution.ErrIntentPending) || len(ex.sent) != 1 {
			t.Fatalf("unknown market close duplicated: %v", err)
		}
	}
}

func TestLiquidationQueryOnlyVenueCannotSubmitUntrackedMarket(t *testing.T) {
	ex := &liquidationIntentExchange{market: "futures"}
	_, err := position.NewExchangeLiquidationVenue(ex).PlaceMarketOrder(t.Context(), "BTCUSDT", "SELL", 1, true)
	if err == nil || len(ex.sent) != 0 {
		t.Fatal("query-only venue bypassed the owned executor")
	}
}

func TestContextAdaptersForwardLimitOptionsAndCancellation(t *testing.T) {
	for _, multi := range []bool{false, true} {
		ex := &liquidationIntentExchange{market: "futures"}
		_, single, batch := newLiquidationIntentManager(t, ex, multi)
		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		defer cancel()
		req := &position.OrderRequest{Symbol: "BTCUSDT", Side: "SELL", Type: "LIMIT", TimeInForce: "IOC", Price: 100, Quantity: 1, ReduceOnly: true}
		if _, err := single.PlaceOrderContext(ctx, req); err != nil {
			t.Fatal(err)
		}
		second := *req
		second.ClientOrderID = ""
		res := batch.BatchPlaceOrdersWithDetailsContext(ctx, []*position.OrderRequest{nil, &second})
		if len(res.PlacedOrders) != 1 || len(ex.sent) != 2 || req.ClientOrderID == "" || second.ClientOrderID == "" || req.ClientOrderID == second.ClientOrderID {
			t.Fatalf("lost batch intent identity: %+v", res)
		}
		for _, sent := range ex.sent {
			if sent.Type != exchange.OrderTypeLimit || sent.TimeInForce != exchange.TimeInForceIOC {
				t.Fatalf("adapter dropped execution options: %+v", sent)
			}
		}
		cancel()
		if _, err := single.PlaceOrderContext(ctx, req); !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
		batch.BatchPlaceOrdersWithDetailsContext(ctx, []*position.OrderRequest{req})
		if len(ex.sent) != 2 {
			t.Fatal("cancelled adapter call reached venue")
		}
	}
}
