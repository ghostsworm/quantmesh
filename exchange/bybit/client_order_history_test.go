package bybit

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

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
