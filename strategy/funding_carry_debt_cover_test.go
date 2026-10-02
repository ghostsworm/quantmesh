package strategy

import (
	"context"
	"math"
	"testing"

	"quantmesh/config"
	"quantmesh/exchange"
	"quantmesh/execution"
)

func TestFundingCarryDebtCoverRequiresPrincipalAndInterest(t *testing.T) {
	for _, fill := range []float64{0.4, math.NaN(), math.Inf(1), 0.5} {
		t.Run(fmtDebtCoverFill(fill), func(t *testing.T) {
			spot := &mockFCExchange{baseAsset: "BTC", latestPrice: 50000, quantityDecimals: 3, priceDecimals: 2}
			futures := &mockFCExchange{quantityDecimals: 3}
			margin := &mockFCExchange{quantityDecimals: 3, positions: []*exchange.Position{{Symbol: "BTCUSDT", Size: -0.45, MarginBorrowed: 0.4, MarginInterest: 0.05, MarginDebtKnown: true}}, getOrderStatus: exchange.OrderStatusFilled, getOrderExecQty: fill, clearDebtOnRepay: true, repayPrincipal: 0.4}
			s := NewFundingCarryStrategy("fc", nil, config.SymbolConfig{Symbol: "BTCUSDT"}, futures, spot, margin, nil)
			s.SetRuntimeStateStore(&memoryRuntimeStateStore{})
			s.direction, s.marginDebt, s.marginBorrowTransferID = DirectionReverse, 0.4, 42
			if err := s.closeReverse(context.Background(), "incomplete_debt_cover"); err == nil {
				t.Fatal("invalid or incomplete cover declared successful")
			}
			if margin.repayCalls != 0 || !s.unownedExposure || s.marginDebt != 0.4 || s.marginBorrowTransferID != 42 {
				t.Fatal("invalid cover borrowed shared funds for repayment or lost debt")
			}
		})
	}
}

type fundingCarryInterruptedCoverExchange struct {
	*mockFCExchange
	afterFill func()
}

func (e *fundingCarryInterruptedCoverExchange) GetOrder(ctx context.Context, symbol string, id int64) (*exchange.Order, error) {
	order, err := e.mockFCExchange.GetOrder(ctx, symbol, id)
	if e.afterFill != nil {
		e.afterFill()
	}
	return order, err
}

func TestFundingCarryDebtCoverInterruptionCannotRepay(t *testing.T) {
	spot := &mockFCExchange{baseAsset: "BTC", latestPrice: 50000, quantityDecimals: 3, priceDecimals: 2}
	futures := &mockFCExchange{quantityDecimals: 3}
	margin := &fundingCarryInterruptedCoverExchange{mockFCExchange: &mockFCExchange{quantityDecimals: 3, positions: []*exchange.Position{{Symbol: "BTCUSDT", Size: -0.45, MarginBorrowed: 0.4, MarginInterest: 0.05, MarginDebtKnown: true}}, getOrderStatus: exchange.OrderStatusFilled, getOrderExecQty: 0.45, clearDebtOnRepay: true, repayPrincipal: 0.4}}
	s := NewFundingCarryStrategy("fc", nil, config.SymbolConfig{Symbol: "BTCUSDT"}, futures, spot, margin, nil)
	store := &memoryRuntimeStateStore{}
	s.SetRuntimeStateStore(store)
	s.direction, s.marginDebt = DirectionReverse, 0.4
	gate := &execution.OpeningGate{}
	s.SetOpeningGate(gate)
	var before string
	margin.afterFill = func() { before = store.payload; gate.Block(strategyWalletRuntimeOwnershipBlock) }
	if err := s.closeReverse(context.Background(), "interrupted_cover"); err == nil {
		t.Fatal("lost owner completed repayment")
	}
	if margin.repayCalls != 0 || store.payload != before || !s.unownedExposure {
		t.Fatal("lost owner mutated wallet after fill")
	}
}

func fmtDebtCoverFill(fill float64) string {
	if math.IsNaN(fill) {
		return "nan"
	}
	if math.IsInf(fill, 0) {
		return "infinite"
	}
	if fill > 0.45 {
		return "overfill"
	}
	return "interest_not_covered"
}
