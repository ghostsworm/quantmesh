package bingx

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPlaceOrderWithOptionsSelectsCorrectHedgeLeg(t *testing.T) {
	tests := []struct {
		name        string
		side        OrderSide
		reduceOnly  bool
		wantSide    string
		wantPosSide string
	}{
		{name: "open long", side: SideBuy, wantSide: "BUY", wantPosSide: "LONG"},
		{name: "open short", side: SideSell, wantSide: "SELL", wantPosSide: "SHORT"},
		{name: "close short", side: SideBuy, reduceOnly: true, wantSide: "BUY", wantPosSide: "SHORT"},
		{name: "close long", side: SideSell, reduceOnly: true, wantSide: "SELL", wantPosSide: "LONG"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if err := r.ParseForm(); err != nil {
					t.Fatalf("parse venue request: %v", err)
				}
				if r.Form.Get("side") != test.wantSide || r.Form.Get("positionSide") != test.wantPosSide {
					t.Fatalf("side=%q positionSide=%q", r.Form.Get("side"), r.Form.Get("positionSide"))
				}
				if r.Form.Has("reduceOnly") {
					t.Fatal("reduceOnly must be omitted in BingX hedge mode")
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"code":0,"data":{"orderId":1,"symbol":"BTC-USDT"}}`))
			}))
			defer server.Close()

			client := NewBingXClient("api-key", "secret-key", false)
			client.baseURL = server.URL
			client.httpClient = server.Client()
			adapter := &Adapter{client: client, symbol: "BTC-USDT"}
			if _, err := adapter.PlaceOrderWithOptions(context.Background(), test.side, 100, 1, "cid", test.reduceOnly); err != nil {
				t.Fatalf("PlaceOrderWithOptions: %v", err)
			}
		})
	}
}
