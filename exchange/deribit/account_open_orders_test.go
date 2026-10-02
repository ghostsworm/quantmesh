package deribit

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGetOpenOrdersWithoutInstrumentAuthenticatesAndReturnsAllCurrencies(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v2" {
			t.Errorf("path = %q, want /api/v2", r.URL.Path)
		}
		var request JSONRPCRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		switch request.Method {
		case "public/auth":
			if r.Header.Get("Authorization") != "" {
				t.Errorf("authentication request unexpectedly has bearer token")
			}
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","result":{"access_token":"read-token","refresh_token":"refresh"}}`))
		case "private/get_open_orders":
			if r.Header.Get("Authorization") != "Bearer read-token" {
				t.Errorf("Authorization = %q, want authenticated read token", r.Header.Get("Authorization"))
			}
			if len(request.Params) != 0 {
				t.Errorf("params = %#v, want unfiltered all-account request", request.Params)
			}
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","result":[{"order_id":"btc-1","instrument_name":"BTC-PERPETUAL","direction":"buy","price":68000,"amount":100,"filled_amount":10,"order_state":"open","order_type":"limit","label":"","creation_timestamp":1000,"last_update_timestamp":1001},{"order_id":"eth-2","instrument_name":"ETH-27DEC30-5000-C","direction":"sell","price":"market_price","amount":2,"filled_amount":0,"order_state":"untriggered","order_type":"stop_market","label":"","creation_timestamp":2000,"last_update_timestamp":2001}]}`))
		default:
			t.Errorf("unexpected method %q", request.Method)
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","result":null}`))
		}
	}))
	defer server.Close()
	client := NewDeribitClient("key", "secret", false)
	client.baseURL = server.URL
	client.httpClient = server.Client()

	orders, err := client.GetOpenOrders(context.Background(), "")
	if err != nil {
		t.Fatalf("GetOpenOrders(account-wide): %v", err)
	}
	if len(orders) != 2 || orders[0].InstrumentName != "BTC-PERPETUAL" || orders[1].InstrumentName != "ETH-27DEC30-5000-C" || orders[1].Price != 0 {
		t.Fatalf("orders = %#v, want all currencies, including a market-trigger order", orders)
	}
}

func TestGetOpenOrdersRejectsIncompleteOrUnknownSnapshot(t *testing.T) {
	valid := `{"order_id":"o-1","instrument_name":"BTC-PERPETUAL","direction":"buy","price":100,"amount":2,"filled_amount":0,"order_state":"open","order_type":"limit","creation_timestamp":1,"last_update_timestamp":2}`
	tests := []struct {
		name   string
		result string
	}{
		{name: "missing result"},
		{name: "null result", result: `null`},
		{name: "missing identity", result: `[{"order_id":"o-1"}]`},
		{name: "duplicate identity", result: `[` + valid + `,` + valid + `]`},
		{name: "unknown direction", result: `[` + strings.Replace(valid, `"buy"`, `"hold"`, 1) + `]`},
		{name: "unknown state", result: `[` + strings.Replace(valid, `"open"`, `"pending"`, 1) + `]`},
		{name: "unknown type", result: `[` + strings.Replace(valid, `"limit"`, `"unknown"`, 1) + `]`},
		{name: "null price", result: `[` + strings.Replace(valid, `"price":100`, `"price":null`, 1) + `]`},
		{name: "filled exceeds amount", result: `[` + strings.Replace(valid, `"filled_amount":0`, `"filled_amount":3`, 1) + `]`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var request JSONRPCRequest
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Fatalf("decode request: %v", err)
				}
				body := `{"jsonrpc":"2.0","result":` + tt.result + `}`
				if tt.result == "" {
					body = `{"jsonrpc":"2.0"}`
				}
				_, _ = w.Write([]byte(body))
			}))
			defer server.Close()
			client := NewDeribitClient("key", "secret", false)
			client.baseURL = server.URL
			client.httpClient = server.Client()
			client.accessToken = "pre-authenticated"
			if _, err := client.GetOpenOrders(context.Background(), ""); err == nil {
				t.Fatal("GetOpenOrders() accepted an incomplete or unknown snapshot")
			}
		})
	}
}
