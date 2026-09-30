package woox

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPlaceOrderWithOptionsTransmitsReduceOnly(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request OrderRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode venue request: %v", err)
		}
		if !request.ReduceOnly {
			t.Fatal("venue request lost reduce_only=true")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"data":{"order_id":1,"symbol":"BTC_USDT","side":"SELL","order_type":"LIMIT","order_price":100,"order_quantity":1,"status":"NEW"}}`))
	}))
	defer server.Close()

	client := NewWOOXClient("api-key", "secret-key", false)
	client.baseURL = server.URL
	client.httpClient = server.Client()
	adapter := &Adapter{client: client, symbol: "BTC_USDT"}
	if _, err := adapter.PlaceOrderWithOptions(context.Background(), SideSell, 100, 1, "client-1", true); err != nil {
		t.Fatalf("PlaceOrderWithOptions: %v", err)
	}
}
