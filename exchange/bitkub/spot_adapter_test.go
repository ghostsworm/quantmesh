package bitkub

import (
	"context"
	"strings"
	"testing"
)

func TestNewBitkubSpotAdapterRequiresCredentials(t *testing.T) {
	if _, err := NewBitkubSpotAdapter(map[string]string{}, "BTCUSDT"); err == nil {
		t.Fatal("expected missing credentials error")
	}
}

func TestConvertSymbolToBitkubAndFormatting(t *testing.T) {
	tests := []struct {
		name   string
		symbol string
		want   string
	}{
		{name: "usdt pair", symbol: "BTCUSDT", want: "BTC_THB"},
		{name: "non usdt pair", symbol: "ETH", want: "ETH_THB"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := convertSymbolToBitkub(tt.symbol); got != tt.want {
				t.Fatalf("convertSymbolToBitkub() = %q, want %q", got, tt.want)
			}
		})
	}

	if got := formatAmount(1000.5000); got != "1000.5" {
		t.Fatalf("formatAmount() = %q", got)
	}
	if got := formatAmount(1000.0); got != "1000" {
		t.Fatalf("formatAmount() = %q", got)
	}
}

func TestBitkubSpotAdapterPureAccessorsAndRounding(t *testing.T) {
	adapter := &BitkubSpotAdapter{
		symbol:           "BTCUSDT",
		bitkubSymbol:     "BTC_THB",
		priceDecimals:    2,
		quantityDecimals: 3,
		tickSize:         0.5,
		stepSize:         0.001,
		baseAsset:        "BTC",
		quoteAsset:       "THB",
	}

	if adapter.GetName() != "Bitkub Spot" {
		t.Fatalf("unexpected name: %s", adapter.GetName())
	}
	if adapter.GetMarketType() != "spot" {
		t.Fatalf("unexpected market type: %s", adapter.GetMarketType())
	}
	if adapter.roundToTickSize(101.26, SideBuy) != 101.0 {
		t.Fatalf("buy price should round down")
	}
	if adapter.roundToTickSize(101.26, SideSell) != 101.5 {
		t.Fatalf("sell price should round up")
	}
	if adapter.roundToStepSize(0.1239) != 0.123 {
		t.Fatalf("quantity should round down to step")
	}
	if adapter.GetPriceDecimals() != 2 || adapter.GetQuantityDecimals() != 3 {
		t.Fatalf("unexpected decimals")
	}
	if adapter.GetBaseAsset() != "BTC" || adapter.GetQuoteAsset() != "THB" {
		t.Fatalf("unexpected assets")
	}
	if got := adapter.EstimateFinalOrderAmount("BTCUSDT", 100, 0.25, false); got != 25 {
		t.Fatalf("EstimateFinalOrderAmount() = %v", got)
	}
}

func TestBitkubSpotAdapterConversions(t *testing.T) {
	adapter := &BitkubSpotAdapter{}

	openBuy := adapter.convertOpenOrderToOrder(&OpenOrder{
		ID:       "123",
		Side:     "buy",
		Type:     "limit",
		Rate:     "100.5",
		Amount:   "1005",
		Receive:  "10",
		ClientID: "cid-1",
		TS:       1710000000123,
	}, "BTCUSDT")
	if openBuy.OrderID != 123 || openBuy.Side != SideBuy || openBuy.Type != OrderTypeLimit {
		t.Fatalf("unexpected open buy conversion: %+v", openBuy)
	}
	if openBuy.ExecutedQty != 10 || openBuy.Quantity != 1005 || openBuy.Price != 100.5 {
		t.Fatalf("unexpected open buy quantities: %+v", openBuy)
	}

	openSell := adapter.convertOpenOrderToOrder(&OpenOrder{
		ID:      "124",
		Side:    "sell",
		Type:    "market",
		Rate:    "99",
		Amount:  "2",
		Receive: "198",
		TS:      1710000000456,
	}, "BTCUSDT")
	if openSell.Side != SideSell || openSell.Type != OrderTypeMarket || openSell.ExecutedQty != 2 {
		t.Fatalf("unexpected open sell conversion: %+v", openSell)
	}

	partiallyFilled := adapter.convertOrderInfoToOrder(&OrderInfo{
		ID:            "456",
		ClientID:      "cid-2",
		Amount:        "4",
		Rate:          110,
		Filled:        "2",
		Status:        "unfilled",
		PartialFilled: true,
		History: []OrderFill{
			{Amount: "1", Rate: 100},
			{Amount: "1", Rate: 120},
		},
	}, "BTCUSDT")
	if partiallyFilled.Status != OrderStatusPartiallyFilled {
		t.Fatalf("status = %s", partiallyFilled.Status)
	}
	if partiallyFilled.AvgPrice != 110 {
		t.Fatalf("avg price = %v", partiallyFilled.AvgPrice)
	}

	canceled := adapter.convertOrderInfoToOrder(&OrderInfo{ID: "789", Status: "cancelled"}, "BTCUSDT")
	if canceled.Status != OrderStatusCanceled {
		t.Fatalf("status = %s", canceled.Status)
	}
}

func TestBitkubSpotUnsupportedSpotMethods(t *testing.T) {
	adapter := &BitkubSpotAdapter{}
	ctx := context.Background()

	positions, err := adapter.GetPositions(ctx, "BTCUSDT")
	if err != nil || len(positions) != 0 {
		t.Fatalf("GetPositions() = %v, %v", positions, err)
	}
	if err := adapter.StartOrderStream(ctx, func(interface{}) {}); err == nil || !strings.Contains(err.Error(), "不支持") {
		t.Fatalf("expected unsupported order stream error, got %v", err)
	}
	if err := adapter.StopOrderStream(); err != nil {
		t.Fatalf("StopOrderStream() error = %v", err)
	}
	if _, err := adapter.GetFundingRate(ctx, "BTCUSDT"); err == nil {
		t.Fatal("expected spot funding rate error")
	}
	if _, err := adapter.GetHistoricalKlines(ctx, "BTCUSDT", "1m", 10); err == nil {
		t.Fatal("expected historical kline unsupported error")
	}
	if fills, err := adapter.GetIncomeHistory(ctx, "BTCUSDT", "REALIZED_PNL", 0, 1); err != nil || fills != nil {
		t.Fatalf("GetIncomeHistory() = %v, %v", fills, err)
	}
	if _, err := adapter.InternalTransfer(ctx, "spot", "funding", "BTC", 1); err == nil {
		t.Fatal("expected internal transfer unsupported error")
	}
}
