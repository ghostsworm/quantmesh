package bingx

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newBingXOpenOrdersTestClient(t *testing.T, response string) (*BingXClient, func()) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/openApi/swap/v2/trade/openOrders" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.String())
		}
		if r.URL.Query().Has("symbol") {
			t.Errorf("account-wide request must omit symbol: %s", r.URL.String())
		}
		if r.URL.Query().Get("timestamp") == "" || r.URL.Query().Get("signature") == "" {
			t.Errorf("signed request parameters are missing: %s", r.URL.RawQuery)
		}
		_, _ = w.Write([]byte(response))
	}))
	client := NewBingXClient("test-key", "test-secret", false)
	client.baseURL = server.URL
	client.httpClient = server.Client()
	return client, server.Close
}

func TestGetAccountOpenOrdersReturnsEverySymbol(t *testing.T) {
	client, closeServer := newBingXOpenOrdersTestClient(t, `{"code":0,"data":[{"orderId":101,"symbol":"BTC-USDT","price":"50000","quantity":"0.1","executedQty":"0","side":"BUY","type":"LIMIT","status":"NEW","updateTime":10},{"orderId":202,"symbol":"ETH-USDT","price":"3000","quantity":"1","executedQty":"0.2","side":"SELL","type":"LIMIT","status":"PARTIALLY_FILLED","updateTime":11}]}`)
	defer closeServer()

	orders, err := client.GetAccountOpenOrders(context.Background())
	if err != nil {
		t.Fatalf("GetAccountOpenOrders() error = %v", err)
	}
	if len(orders) != 2 || orders[0].Symbol != "BTC-USDT" || orders[1].Symbol != "ETH-USDT" {
		t.Fatalf("account-wide orders = %#v", orders)
	}
}

func TestGetAccountOpenOrdersRejectsUnknownOrIncompleteSnapshot(t *testing.T) {
	tests := []struct {
		name     string
		response string
	}{
		{name: "missing data", response: `{"code":0}`},
		{name: "null data", response: `{"code":0,"data":null}`},
		{name: "missing order symbol", response: `{"code":0,"data":[{"orderId":1,"side":"BUY","status":"NEW"}]}`},
		{name: "duplicate order", response: `{"code":0,"data":[{"orderId":1,"symbol":"BTC-USDT","side":"BUY","status":"NEW"},{"orderId":1,"symbol":"ETH-USDT","side":"SELL","status":"NEW"}]}`},
		{name: "unknown status", response: `{"code":0,"data":[{"orderId":1,"symbol":"BTC-USDT","side":"BUY","status":"FILLED"}]}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, closeServer := newBingXOpenOrdersTestClient(t, tt.response)
			defer closeServer()
			if _, err := client.GetAccountOpenOrders(context.Background()); err == nil || !strings.Contains(err.Error(), "BingX") {
				t.Fatalf("GetAccountOpenOrders() error = %v, want contextual fail-closed error", err)
			}
		})
	}
}
