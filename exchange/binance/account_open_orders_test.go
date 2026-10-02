package binance

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	binancesdk "github.com/adshao/go-binance/v2"
	"github.com/adshao/go-binance/v2/futures"
)

func TestAccountOpenOrdersQueriesAllSymbolsForEachMarket(t *testing.T) {
	tests := []struct {
		name string
		path string
		new  func(string) ([]*Order, error)
	}{
		{
			name: "futures",
			path: "/fapi/v1/openOrders",
			new: func(endpoint string) ([]*Order, error) {
				client := futures.NewClient("test-key", "test-secret").SetApiEndpoint(endpoint)
				return (&BinanceAdapter{client: client}).GetAccountOpenOrders(context.Background())
			},
		},
		{
			name: "spot",
			path: "/api/v3/openOrders",
			new: func(endpoint string) ([]*Order, error) {
				client := binancesdk.NewClient("test-key", "test-secret")
				client.BaseURL = endpoint
				return (&BinanceSpotAdapter{client: client}).GetAccountOpenOrders(context.Background())
			},
		},
		{
			name: "cross margin",
			path: "/sapi/v1/margin/openOrders",
			new: func(endpoint string) ([]*Order, error) {
				client := binancesdk.NewClient("test-key", "test-secret")
				client.BaseURL = endpoint
				spot := &BinanceSpotAdapter{client: client}
				return (&BinanceSpotMarginAdapter{BinanceSpotAdapter: spot, marginClient: NewMarginClient(client)}).GetAccountOpenOrders(context.Background())
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != tt.path {
					t.Errorf("endpoint path = %q, want %q", r.URL.Path, tt.path)
				}
				if _, present := r.URL.Query()["symbol"]; present {
					t.Errorf("account-wide request unexpectedly contains symbol=%q", r.URL.Query().Get("symbol"))
				}
				_, _ = fmt.Fprint(w, `[{"symbol":"ETHUSDT","orderId":17,"clientOrderId":"external-17","price":"10.5","origQty":"2","executedQty":"0","avgPrice":"0","cummulativeQuoteQty":"0","side":"BUY","type":"LIMIT","status":"NEW"}]`)
			}))
			defer server.Close()

			orders, err := tt.new(server.URL)
			if err != nil {
				t.Fatalf("GetAccountOpenOrders() error = %v", err)
			}
			if len(orders) != 1 || orders[0].Symbol != "ETHUSDT" || orders[0].OrderID != 17 || orders[0].ClientOrderID != "external-17" {
				t.Fatalf("account-wide orders = %+v", orders)
			}
		})
	}
}
