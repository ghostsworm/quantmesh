package mexc

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPlaceOrderWithOptionsUsesMEXCCloseSides(t *testing.T) {
	tests := []struct {
		name     string
		side     OrderSide
		wantSide string
	}{
		{name: "buy closes short", side: SideBuy, wantSide: "2"},
		{name: "sell closes long", side: SideSell, wantSide: "4"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Fatalf("decode venue request: %v", err)
				}
				wantSide := float64(2)
				if test.wantSide == "4" {
					wantSide = 4
				}
				if got := body["side"]; got != wantSide {
					t.Fatalf("venue close side = %q, want %q", got, test.wantSide)
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"code":0,"success":true,"data":{"orderId":"order-1","ts":1760000000000}}`))
			}))
			defer server.Close()

			client := NewMEXCClient("api-key", "secret-key", false)
			client.baseURL = server.URL
			client.httpClient = server.Client()
			adapter := &Adapter{client: client, symbol: "BTC_USDT"}
			if _, err := adapter.PlaceOrderWithOptions(context.Background(), test.side, 100, 1, "cid", true); err != nil {
				t.Fatalf("PlaceOrderWithOptions: %v", err)
			}
		})
	}
}

func TestConvertOrderUsesMEXCCloseSideDirection(t *testing.T) {
	adapter := &Adapter{symbol: "BTC_USDT"}
	for _, test := range []struct {
		venueSide int
		want      OrderSide
	}{
		{venueSide: int(MEXCOrderSideCloseShort), want: SideBuy},
		{venueSide: int(MEXCOrderSideCloseLong), want: SideSell},
	} {
		got := adapter.convertOrder(&OrderInfo{OrderID: "1", Side: test.venueSide})
		if got.Side != test.want {
			t.Errorf("convertOrder(side=%d) = %s, want %s", test.venueSide, got.Side, test.want)
		}
	}
}
