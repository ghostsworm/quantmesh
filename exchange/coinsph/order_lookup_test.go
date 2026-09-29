package coinsph

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetOrderByClientOrderIDRequiresUniqueExactMatch(t *testing.T) {
	tests := []struct {
		name      string
		response  string
		wantErr   bool
		wantOrder int64
	}{
		{name: "exact list result", response: `[{"clientOrderId":"other"},{"clientOrderId":"close-1","orderId":7}]`, wantOrder: 7},
		{name: "duplicate id", response: `[{"clientOrderId":"close-1"},{"clientOrderId":"close-1"}]`, wantErr: true},
		{name: "mismatch", response: `[{"clientOrderId":"other"}]`, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Query().Get("origClientOrderId") != "close-1" {
					t.Errorf("query=%s", r.URL.RawQuery)
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(tt.response))
			}))
			defer server.Close()
			client := NewCoinsphClient("key", "secret", false)
			client.baseURL = server.URL
			order, err := client.GetOrderByClientOrderID(context.Background(), "BTCUSDT", "close-1")
			if (err != nil) != tt.wantErr {
				t.Fatalf("order=%+v error=%v", order, err)
			}
			if err == nil && tt.wantOrder != 0 && order.OrderID != tt.wantOrder {
				t.Fatalf("order=%+v", order)
			}
		})
	}
}
