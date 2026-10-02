package strategy

import (
	"context"
	"testing"

	"quantmesh/config"
	"quantmesh/exchange"
)

type fundingCarryNetCoverExchange struct {
	*mockFCExchange
	fills []*exchange.OrderFill
}

func (e *fundingCarryNetCoverExchange) GetOrderFills(context.Context, string, int64) ([]*exchange.OrderFill, error) {
	return e.fills, nil
}

func TestFundingCarryNetDebtCoverCannotUseGrossFill(t *testing.T) {
	for _, mode := range []string{"base_fee_shortfall", "missing_fills", "wrong_order", "duplicate_trade", "incomplete_quantity", "base_fee_sufficient", "quote_fee", "zero_fee", "underreported_base_fee", "zero_fee_with_base_charge", "invalid_quote_rate", "converted_base_fee"} {
		t.Run(mode, func(t *testing.T) {
			spot := &mockFCExchange{baseAsset: "BTC", latestPrice: 50000, quantityDecimals: 3, priceDecimals: 2}
			futures := &mockFCExchange{quantityDecimals: 3}
			fill := &exchange.OrderFill{OrderID: 1, TradeID: "trade-1", Symbol: "BTCUSDT", Side: exchange.SideBuy, Price: 50000, Quantity: 0.401, CommissionAsset: "BTC", Commission: 0.001, BaseFeeQty: 0.001, TradeTime: 1}
			venue := &fundingCarryNetCoverExchange{mockFCExchange: &mockFCExchange{quantityDecimals: 3, positions: []*exchange.Position{{Symbol: "BTCUSDT", Size: -0.4005, MarginBorrowed: 0.4, MarginInterest: 0.0005, MarginDebtKnown: true}}, getOrderStatus: exchange.OrderStatusFilled, getOrderExecQty: 0.401, clearDebtOnRepay: true, repayPrincipal: 0.4}, fills: []*exchange.OrderFill{fill}}
			switch mode {
			case "missing_fills":
				venue.fills = nil
			case "wrong_order":
				fill.OrderID = 2
			case "duplicate_trade":
				venue.fills = append(venue.fills, fill)
			case "incomplete_quantity":
				fill.Quantity = 0.2
			case "base_fee_sufficient":
				fill.Commission, fill.BaseFeeQty = 0.0004, 0.0004
			case "quote_fee":
				fill.CommissionAsset, fill.BaseFeeQty = "USDT", 0
			case "zero_fee":
				fill.Commission, fill.BaseFeeQty = 0, 0
			case "underreported_base_fee":
				fill.BaseFeeQty = 0.0001
			case "zero_fee_with_base_charge":
				fill.Commission, fill.BaseFeeQty = 0, 0.0001
			case "invalid_quote_rate":
				fill.Commission, fill.BaseFeeQty = 20, 0.0004
				fill.CommissionQuoteKnown, fill.CommissionQuote, fill.CommissionQuoteRate = true, 20, 0
			case "converted_base_fee":
				fill.Commission, fill.BaseFeeQty = 20, 0.0004
				fill.CommissionQuoteKnown, fill.CommissionQuote, fill.CommissionQuoteRate = true, 20, 50000
			}
			s := NewFundingCarryStrategy("fc", nil, config.SymbolConfig{Symbol: "BTCUSDT"}, futures, spot, venue, nil)
			s.SetRuntimeStateStore(&memoryRuntimeStateStore{})
			s.direction, s.marginDebt = DirectionReverse, 0.4
			err := s.closeReverse(context.Background(), mode)
			if mode == "base_fee_sufficient" || mode == "quote_fee" || mode == "zero_fee" || mode == "converted_base_fee" {
				if err != nil || venue.repayCalls != 1 || s.marginDebt != 0 || s.direction != DirectionNone || s.unownedExposure {
					t.Fatalf("verified net cover did not close: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("unverified net cover declared complete")
			}
			if venue.repayCalls != 0 || s.marginDebt != 0.4 || !s.unownedExposure {
				t.Fatal("unverified net cover used shared wallet for repayment")
			}
		})
	}
}
