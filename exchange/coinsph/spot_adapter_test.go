package coinsph

import (
	"context"
	"strings"
	"testing"
)

func TestNewCoinsphSpotAdapterRequiresCredentials(t *testing.T) {
	if _, err := NewCoinsphSpotAdapter(map[string]string{}, "BTCPHP"); err == nil {
		t.Fatal("expected missing credentials error")
	}
}

func TestCoinsphSpotAdapterPureAccessorsAndRounding(t *testing.T) {
	adapter := &CoinsphSpotAdapter{
		symbol:           "BTCPHP",
		priceDecimals:    2,
		quantityDecimals: 3,
		tickSize:         0.5,
		stepSize:         0.001,
		baseAsset:        "BTC",
		quoteAsset:       "PHP",
	}

	if adapter.GetName() != "Coins.ph Spot" {
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
	if adapter.GetBaseAsset() != "BTC" || adapter.GetQuoteAsset() != "PHP" {
		t.Fatalf("unexpected assets")
	}
	if got := adapter.EstimateFinalOrderAmount("BTCPHP", 200, 0.5, false); got != 100 {
		t.Fatalf("EstimateFinalOrderAmount() = %v", got)
	}
}

func TestCoinsphSpotAdapterConvertOrderInfoToOrder(t *testing.T) {
	adapter := &CoinsphSpotAdapter{}

	tests := []struct {
		name       string
		status     string
		wantStatus OrderStatus
	}{
		{name: "new", status: "NEW", wantStatus: OrderStatusNew},
		{name: "partial", status: "PARTIALLY_FILLED", wantStatus: OrderStatusPartiallyFilled},
		{name: "filled", status: "FILLED", wantStatus: OrderStatusFilled},
		{name: "canceled", status: "CANCELED", wantStatus: OrderStatusCanceled},
		{name: "expired", status: "EXPIRED", wantStatus: OrderStatusExpired},
		{name: "unknown", status: "BROKEN", wantStatus: OrderStatusRejected},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			order := adapter.convertOrderInfoToOrder(&OrderInfo{
				ClientOrderID:       "client-1",
				CummulativeQuoteQty: "330",
				ExecutedQty:         "3",
				OrderID:             99,
				OrigQty:             "5",
				Price:               "100",
				Side:                "BUY",
				Status:              tt.status,
				Symbol:              "BTCPHP",
				Time:                1710000000123,
				Type:                "LIMIT",
				UpdateTime:          1710000000456,
			}, "BTCPHP")

			if order.Status != tt.wantStatus {
				t.Fatalf("status = %s, want %s", order.Status, tt.wantStatus)
			}
			if order.OrderID != 99 || order.Side != SideBuy || order.Type != OrderTypeLimit {
				t.Fatalf("unexpected order identity: %+v", order)
			}
			if order.Price != 100 || order.Quantity != 5 || order.ExecutedQty != 3 || order.AvgPrice != 110 {
				t.Fatalf("unexpected order amounts: %+v", order)
			}
		})
	}
}

func TestCoinsphSpotUnsupportedSpotMethods(t *testing.T) {
	adapter := &CoinsphSpotAdapter{}
	ctx := context.Background()

	positions, err := adapter.GetPositions(ctx, "BTCPHP")
	if err != nil || len(positions) != 0 {
		t.Fatalf("GetPositions() = %v, %v", positions, err)
	}
	if err := adapter.StartOrderStream(ctx, func(interface{}) {}); err == nil || !strings.Contains(err.Error(), "待实现") {
		t.Fatalf("expected unsupported order stream error, got %v", err)
	}
	if err := adapter.StopOrderStream(); err != nil {
		t.Fatalf("StopOrderStream() error = %v", err)
	}
	if _, err := adapter.GetFundingRate(ctx, "BTCPHP"); err == nil {
		t.Fatal("expected spot funding rate error")
	}
	if _, err := adapter.GetHistoricalKlines(ctx, "BTCPHP", "1m", 10); err == nil {
		t.Fatal("expected historical kline unsupported error")
	}
	if fills, err := adapter.GetIncomeHistory(ctx, "BTCPHP", "REALIZED_PNL", 0, 1); err != nil || fills != nil {
		t.Fatalf("GetIncomeHistory() = %v, %v", fills, err)
	}
	if _, err := adapter.InternalTransfer(ctx, "spot", "funding", "BTC", 1); err == nil {
		t.Fatal("expected internal transfer unsupported error")
	}
}
