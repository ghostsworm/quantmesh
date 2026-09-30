package phemex

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
			t.Fatal("venue request lost reduceOnly=true")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"msg":"","data":{"orderID":"order-1","order":{"orderID":"order-1","symbol":"BTCUSD","side":"Sell","ordStatus":"New"}}}`))
	}))
	defer server.Close()

	client := NewPhemexClient("api-key", "secret-key", false)
	client.baseURL = server.URL
	client.httpClient = server.Client()
	adapter := &Adapter{client: client, symbol: "BTCUSD", priceScale: 1}
	if _, err := adapter.PlaceOrderWithOptions(context.Background(), SideSell, 100, 1, "client-1", true); err != nil {
		t.Fatalf("PlaceOrderWithOptions: %v", err)
	}
}
