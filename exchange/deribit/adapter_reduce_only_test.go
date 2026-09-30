package deribit

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPlaceOrderWithOptionsTransmitsReduceOnly(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request JSONRPCRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode JSON-RPC request: %v", err)
		}
		if request.Method != "private/sell" || request.Params["reduce_only"] != true {
			t.Fatalf("method=%q params=%#v, want private/sell reduce_only=true", request.Method, request.Params)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"order":{"order_id":"order-1","instrument_name":"BTC-PERPETUAL","direction":"sell","price":100,"amount":1,"filled_amount":0,"order_state":"open"}}}`))
	}))
	defer server.Close()

	client := NewDeribitClient("api-key", "secret-key", false)
	client.baseURL = server.URL
	client.httpClient = server.Client()
	client.accessToken = "test-access-token"
	adapter := &Adapter{client: client, instrumentName: "BTC-PERPETUAL"}
	if _, err := adapter.PlaceOrderWithOptions(context.Background(), SideSell, 100, 1, "label-1", true); err != nil {
		t.Fatalf("PlaceOrderWithOptions: %v", err)
	}
}
