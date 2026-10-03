package main

import (
	"context"
	"testing"

	"quantmesh/exchange"
	"quantmesh/lock"
	"quantmesh/order"
)

type fundingCarryCIDVenue struct {
	exchange.IExchange
	request *exchange.OrderRequest
}

func (v *fundingCarryCIDVenue) GetName() string          { return "fake" }
func (v *fundingCarryCIDVenue) GetMarketType() string    { return "spot_margin" }
func (v *fundingCarryCIDVenue) GetPriceDecimals() int    { return 2 }
func (v *fundingCarryCIDVenue) GetQuantityDecimals() int { return 3 }
func (v *fundingCarryCIDVenue) GetLatestPrice(context.Context, string) (float64, error) {
	return 50000, nil
}
func (v *fundingCarryCIDVenue) PlaceOrder(_ context.Context, req *exchange.OrderRequest) (*exchange.Order, error) {
	v.request = req
	return &exchange.Order{OrderID: 7, ClientOrderID: req.ClientOrderID, Symbol: req.Symbol, Side: req.Side, Quantity: req.Quantity, ExecutedQty: req.Quantity, Status: exchange.OrderStatusFilled}, nil
}

func TestFundingCarryExecutorPreservesCallerCID(t *testing.T) {
	venue := &fundingCarryCIDVenue{}
	adapter := &fundingCarryOrderExecutor{executor: order.NewExchangeOrderExecutor(venue, "BTCUSDT", 1, 1, lock.NewNopLock(), "test"), market: "spot_margin"}
	_, err := adapter.PlaceOrderContext(context.Background(), &exchange.OrderRequest{Symbol: "BTCUSDT", Side: exchange.SideBuy, Type: exchange.OrderTypeLimit, Quantity: 0.4, Price: 50000, ClientOrderID: "durable-cover-cid"})
	if err != nil {
		t.Fatal(err)
	}
	if venue.request == nil || venue.request.ClientOrderID != "durable-cover-cid" {
		t.Fatal("caller-owned recovery CID was replaced")
	}
}
