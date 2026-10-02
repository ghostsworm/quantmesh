package bybit

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetAllOpenOrdersByParamsPaginatesWithoutSymbolFilter(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		query := r.URL.Query()
		if r.URL.Path != "/v5/order/realtime" || query.Get("category") != "linear" || query.Get("settleCoin") != "USDT" || query.Has("symbol") {
			t.Errorf("unexpected account-wide order query: %s", r.URL.RequestURI())
		}
		if query.Get("openOnly") != "0" || query.Get("limit") != "50" {
			t.Errorf("query does not explicitly request active orders with bounded pagination: %s", r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/json")
		if query.Get("cursor") == "" {
			_, _ = w.Write([]byte(`{"retCode":0,"retMsg":"OK","result":{"list":[{"orderId":"1","orderLinkId":"a","symbol":"ETHUSDT","side":"Buy","orderType":"Limit","qty":"1","cumExecQty":"0","orderStatus":"New"}],"nextPageCursor":"next-page"}}`))
			return
		}
		if query.Get("cursor") != "next-page" {
			t.Errorf("cursor=%q, want next-page", query.Get("cursor"))
		}
		_, _ = w.Write([]byte(`{"retCode":0,"retMsg":"OK","result":{"list":[{"orderId":"2","orderLinkId":"b","symbol":"SOLUSDT","side":"Sell","orderType":"Limit","qty":"2","cumExecQty":"0","orderStatus":"New"}],"nextPageCursor":""}}`))
	}))
	defer server.Close()
	client := NewBybitClient("", "", false)
	client.baseURL = server.URL
	orders, err := client.GetAllOpenOrdersByParams(context.Background(), map[string]interface{}{
		"category": "linear", "settleCoin": "USDT",
	})
	if err != nil {
		t.Fatal(err)
	}
	if requests != 2 || len(orders) != 2 || orders[0].Symbol != "ETHUSDT" || orders[1].Symbol != "SOLUSDT" {
		t.Fatalf("requests=%d orders=%+v", requests, orders)
	}
}

func TestGetAllOpenOrdersByParamsRejectsMissingList(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"retCode":0,"retMsg":"OK","result":{"nextPageCursor":""}}`))
	}))
	defer server.Close()
	client := NewBybitClient("", "", false)
	client.baseURL = server.URL
	if orders, err := client.GetAllOpenOrdersByParams(context.Background(), map[string]interface{}{"category": "spot"}); err == nil || orders != nil {
		t.Fatalf("missing list returned orders=%v err=%v, want fail-closed error", orders, err)
	}
}

func TestGetAllOpenOrdersByParamsRejectsRepeatedCursor(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"retCode":0,"retMsg":"OK","result":{"list":[],"nextPageCursor":"same-cursor"}}`))
	}))
	defer server.Close()
	client := NewBybitClient("", "", false)
	client.baseURL = server.URL
	if orders, err := client.GetAllOpenOrdersByParams(context.Background(), map[string]interface{}{"category": "spot"}); err == nil || orders != nil {
		t.Fatalf("repeated cursor returned orders=%v err=%v, want fail-closed error", orders, err)
	}
}

func TestGetOrderHistoryByClientIDUsesHistoryEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v5/order/history" {
			t.Errorf("path=%q, want /v5/order/history", r.URL.Path)
		}
		query := r.URL.Query()
		if query.Get("category") != "linear" || query.Get("symbol") != "BTCUSDT" || query.Get("orderLinkId") != "qm-close-1" {
			t.Errorf("unexpected history query: %s", r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"retCode":0,"retMsg":"OK","result":{"list":[{"orderId":"123","orderLinkId":"qm-close-1","symbol":"BTCUSDT","side":"Sell","orderType":"Market","qty":"2","cumExecQty":"2","avgPrice":"100","orderStatus":"Filled","updatedTime":"1790208000000"}]}}`))
	}))
	defer server.Close()
	client := NewBybitClient("", "", false)
	client.baseURL = server.URL
	order, err := client.GetOrderHistoryByClientID(context.Background(), "linear", "BTCUSDT", "qm-close-1")
	if err != nil {
		t.Fatal(err)
	}
	if order.OrderId != "123" || order.OrderStatus != "Filled" || order.CumExecQty != "2" {
		t.Fatalf("unexpected historical order: %+v", order)
	}
}

func TestGetOrderHistoryByClientIDRejectsNonMatchingResult(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"retCode":0,"retMsg":"OK","result":{"list":[{"orderId":"123","orderLinkId":"another-id"}]}}`))
	}))
	defer server.Close()
	client := NewBybitClient("", "", false)
	client.baseURL = server.URL
	if _, err := client.GetOrderHistoryByClientID(context.Background(), "spot", "BTCUSDT", "qm-close-1"); err == nil {
		t.Fatal("expected a missing exact client order ID to fail closed")
	}
}
