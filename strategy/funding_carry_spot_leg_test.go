package strategy

import (
	"context"
	"testing"

	"quantmesh/config"
	"quantmesh/exchange"
)

func newSpotLegTestStrategy(spotBalance float64, futShort float64) (*FundingCarryStrategy, *mockFCExchange, *mockFCExchange) {
	spotEx := &mockFCExchange{
		baseAsset: "BTC", balance: spotBalance, latestPrice: 50000,
		priceDecimals: 2, quantityDecimals: 5,
		getOrderStatus: exchange.OrderStatusFilled,
	}
	futEx := &mockFCExchange{priceDecimals: 2, quantityDecimals: 3}
	if futShort > 0 {
		futEx.positions = []*exchange.Position{{Symbol: "BTCUSDT", Size: -futShort}}
	}
	s := NewFundingCarryStrategy("fc", nil, config.SymbolConfig{Symbol: "BTCUSDT"}, futEx, spotEx, nil, nil)
	s.SetRuntimeStateStore(&memoryRuntimeStateStore{})
	return s, spotEx, futEx
}

func TestSyncPositions_UsesRecordedSpotQuantity(t *testing.T) {
	cases := []struct {
		name          string
		recorded      float64
		known         bool
		balance       float64
		futShort      float64
		direction     CarryDirection
		ownedFut      float64
		wantSpot      float64
		wantDirection CarryDirection
		wantErr       bool
	}{
		{name: "user holds extra coins", recorded: 0.5, known: true, balance: 3, futShort: 0.5, direction: DirectionForward, ownedFut: 0.5, wantSpot: 0.5, wantDirection: DirectionForward},
		{name: "balance below record requires reconciliation", recorded: 0.5, known: true, balance: 0.3, futShort: 0.5, direction: DirectionForward, ownedFut: 0.5, wantErr: true},
		{name: "only user coins is not a position", recorded: 0, known: true, balance: 2, futShort: 0, wantSpot: 0, wantDirection: DirectionNone},
		{name: "restart without record and no short", recorded: 0, known: false, balance: 2, futShort: 0, wantSpot: 0, wantDirection: DirectionNone},
		{name: "restart cannot claim existing short", recorded: 0, known: false, balance: 2, futShort: 0.5, wantErr: true},
		{name: "recorded spot residual is recoverable", recorded: 0.5, known: true, balance: 0.5, direction: DirectionForward, wantSpot: 0.5, wantDirection: DirectionForward},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, _, _ := newSpotLegTestStrategy(tc.balance, tc.futShort)
			s.strategySpotQty = tc.recorded
			s.strategySpotKnown = tc.known
			s.direction = tc.direction
			s.futQty = tc.ownedFut

			err := s.syncPositions(context.Background())
			if (err != nil) != tc.wantErr {
				t.Fatalf("syncPositions error=%v wantErr=%v", err, tc.wantErr)
			}
			if tc.wantErr {
				if !s.unownedExposure {
					t.Fatal("unsafe exposure did not latch the trading block")
				}
				return
			}
			if s.spotQty != tc.wantSpot {
				t.Fatalf("spotQty=%v want %v", s.spotQty, tc.wantSpot)
			}
			if s.direction != tc.wantDirection {
				t.Fatalf("direction=%v want %v", s.direction, tc.wantDirection)
			}
		})
	}
}

func TestCloseAll_SellsOnlyStrategySpotQuantity(t *testing.T) {
	s, spotEx, _ := newSpotLegTestStrategy(3, 0.5)
	spotEx.getOrderExecQty = 0.5
	s.recordStrategySpot(0.5)
	s.direction = DirectionForward
	s.futQty = 0.5

	if err := s.closeAll(context.Background(), "test_exit"); err != nil {
		t.Fatalf("closeAll: %v", err)
	}

	spotEx.mu.Lock()
	defer spotEx.mu.Unlock()
	if len(spotEx.placedOrders) != 1 {
		t.Fatalf("spot orders=%d want 1", len(spotEx.placedOrders))
	}
	if got := spotEx.placedOrders[0]; got.Side != exchange.SideSell || got.Quantity != 0.5 {
		t.Fatalf("spot close order side=%s qty=%v, want SELL 0.5 (not whole balance 3)", got.Side, got.Quantity)
	}
	if s.strategySpotQty != 0 {
		t.Fatalf("recorded spot after close=%v want 0", s.strategySpotQty)
	}
}

func TestCloseAll_NoRecordedSpotDoesNotSellUserCoins(t *testing.T) {
	s, spotEx, _ := newSpotLegTestStrategy(3, 0)
	s.strategySpotKnown = true

	if err := s.closeAll(context.Background(), "test_exit"); err != nil {
		t.Fatalf("closeAll: %v", err)
	}
	spotEx.mu.Lock()
	defer spotEx.mu.Unlock()
	if len(spotEx.placedOrders) != 0 {
		t.Fatalf("should not sell user coins, orders=%d", len(spotEx.placedOrders))
	}
}

func TestOpenHedge_RecordsStrategySpot(t *testing.T) {
	s, spotEx, _ := newSpotLegTestStrategy(0, 0)
	spotEx.getOrderExecQty = 0.004
	s.symCfg.TotalAllocatedCapital = 500

	if err := s.openHedge(context.Background(), 50050, 50000, 0.001); err != nil {
		t.Fatalf("openHedge: %v", err)
	}
	if !s.strategySpotKnown || s.strategySpotQty != 0.004 {
		t.Fatalf("recorded spot=%v known=%v want 0.004", s.strategySpotQty, s.strategySpotKnown)
	}
}
