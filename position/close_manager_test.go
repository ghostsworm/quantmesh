package position

import (
	"context"
	"testing"

	"quantmesh/config"
)

type fakeCloseExchange struct {
	price    float64
	decimals int
	lastReq  *ExchangeOrderRequest
}

func (f *fakeCloseExchange) GetName() string { return "fake" }
func (f *fakeCloseExchange) PlaceOrder(ctx context.Context, req *ExchangeOrderRequest) (*ExchangeOrder, error) {
	f.lastReq = req
	return &ExchangeOrder{OrderID: 1, Status: "NEW", Symbol: req.Symbol, Side: req.Side, ClientOrderID: req.ClientOrderID, Quantity: req.Quantity}, nil
}
func (f *fakeCloseExchange) GetOrder(ctx context.Context, symbol string, orderID int64) (*ExchangeOrder, error) {
	return &ExchangeOrder{OrderID: orderID, Status: "NEW", Symbol: symbol, Side: f.lastReq.Side, ClientOrderID: f.lastReq.ClientOrderID, Quantity: f.lastReq.Quantity}, nil
}
func (f *fakeCloseExchange) CancelOrder(ctx context.Context, symbol string, orderID int64) error {
	return nil
}
func (f *fakeCloseExchange) GetLatestPrice(ctx context.Context, symbol string) (float64, error) {
	return f.price, nil
}
func (f *fakeCloseExchange) GetPriceDecimals() int { return f.decimals }

func TestClosePositions_LimitUsesRealPrecisionAndNoPostOnly(t *testing.T) {
	tests := []struct {
		name         string
		side         string
		price        float64
		decimals     int
		offset       float64
		wantPrice    float64
		wantDecimals int
	}{
		{"平多 4 位精度", "SELL", 0.123456, 4, -0.1, 0.1233, 4},
		{"平空 1 位精度", "BUY", 50000.1, 1, -0.1, 50050.1, 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ex := &fakeCloseExchange{price: tc.price, decimals: tc.decimals}
			cpm := NewClosePositionManager(ex, "bot", "XUSDT")
			defer cpm.Stop()
			_, err := cpm.ClosePositions(context.Background(), tc.side, 1, config.ClosePositionConfig{Method: "limit", PriceOffset: tc.offset})
			if err != nil {
				t.Fatalf("close: %v", err)
			}
			req := ex.lastReq
			if req == nil || req.PostOnly || !req.ReduceOnly || req.Type != "LIMIT" {
				t.Fatalf("unexpected request: %+v", req)
			}
			if req.PriceDecimals != tc.wantDecimals || req.Price != tc.wantPrice {
				t.Fatalf("price=%v decimals=%d want %v/%d", req.Price, req.PriceDecimals, tc.wantPrice, tc.wantDecimals)
			}
		})
	}
}
