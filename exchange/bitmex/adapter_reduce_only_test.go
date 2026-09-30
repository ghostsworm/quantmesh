package bitmex

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
		if request.ExecInst != "ReduceOnly" {
			t.Fatalf("execInst = %q, want ReduceOnly", request.ExecInst)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"orderID":"order-1","clOrdID":"client-1","symbol":"XBTUSD","side":"Sell","orderQty":1,"ordStatus":"New"}`))
	}))
	defer server.Close()

	client := NewBitMEXClient("api-key", "secret-key", false)
	client.baseURL = server.URL
	client.httpClient = server.Client()
	adapter := &Adapter{client: client, symbol: "XBTUSD"}
	if _, err := adapter.PlaceOrderWithOptions(context.Background(), SideSell, 100, 1, "client-1", true); err != nil {
		t.Fatalf("PlaceOrderWithOptions: %v", err)
	}
}
