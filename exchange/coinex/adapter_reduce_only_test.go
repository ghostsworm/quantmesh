package coinex

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPlaceOrderWithOptionsUsesSignedV2ReduceOnlyRequest(t *testing.T) {
	const apiKey = "test-api-key"
	const secretKey = "test-secret-key"
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.URL.Path != "/v2/futures/order" {
			t.Errorf("request = %s %s, want POST /v2/futures/order", request.Method, request.URL.Path)
		}
		timestamp := request.Header.Get("X-COINEX-TIMESTAMP")
		if request.Header.Get("X-COINEX-KEY") != apiKey || timestamp == "" {
			t.Errorf("missing CoinEx V2 auth headers")
		}
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Errorf("read request body: %v", err)
		}
		var payload map[string]any
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Errorf("decode request body: %v", err)
		}
		if payload["market_type"] != "FUTURES" || payload["side"] != "sell" || payload["type"] != "limit" || payload["market"] != "BTCUSDT" {
			t.Errorf("unexpected order payload: %s", body)
		}
		if payload["is_reduce_only"] != true {
			t.Errorf("is_reduce_only = %v, want true", payload["is_reduce_only"])
		}
		if payload["client_id"] != "close-123" || payload["amount"] != "0.25000000" || payload["price"] != "60000.00000000" {
			t.Errorf("unexpected order details: %s", body)
		}
		prepared := http.MethodPost + request.URL.EscapedPath() + string(body) + timestamp
		signer := hmac.New(sha256.New, []byte(secretKey))
		_, _ = signer.Write([]byte(prepared))
		if !hmac.Equal([]byte(request.Header.Get("X-COINEX-SIGN")), []byte(hex.EncodeToString(signer.Sum(nil)))) {
			t.Errorf("signature does not match official method+path+body+timestamp payload")
		}
		_, _ = io.WriteString(writer, `{"code":0,"message":"OK","data":{"order_id":"42","market":"BTCUSDT","side":"sell","amount":"0.25","price":"60000","filled_amount":"0.1","client_id":"close-123","updated_at":1700000000000}}`)
	}))
	defer server.Close()

	client := NewCoinExClient(apiKey, secretKey, false)
	client.baseURL = server.URL
	client.httpClient = server.Client()
	adapter := &Adapter{client: client, market: "BTCUSDT"}
	order, err := adapter.PlaceOrderWithOptions(context.Background(), SideSell, 60000, 0.25, "close-123", true)
	if err != nil {
		t.Fatalf("PlaceOrderWithOptions() error = %v", err)
	}
	if order.OrderID != 42 || order.Side != SideSell || order.ExecutedQty != 0.1 || order.Status != OrderStatusPartiallyFilled || order.ClientOrderID != "close-123" {
		t.Fatalf("order = %#v, want mapped CoinEx V2 response", order)
	}
}

func TestCoinExV2OrderStatusUsesReportedFill(t *testing.T) {
	tests := []struct {
		name   string
		filled float64
		want   OrderStatus
	}{
		{name: "unfilled", filled: 0, want: OrderStatusNew},
		{name: "partial", filled: 0.1, want: OrderStatusPartiallyFilled},
		{name: "filled", filled: 0.25, want: OrderStatusFilled},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := coinExV2OrderStatus(test.filled, 0.25); got != test.want {
				t.Fatalf("coinExV2OrderStatus(%v) = %v, want %v", test.filled, got, test.want)
			}
		})
	}
}
