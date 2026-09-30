package mexc

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetOrderFillsMapsAuthenticatedDealLedger(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/private/order/deal_details/123" || r.Method != http.MethodGet {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.RequestURI())
		}
		if r.Header.Get("X-MEXC-APIKEY") != "api-key" || r.URL.Query().Get("signature") == "" || r.URL.Query().Get("timestamp") == "" {
			t.Fatalf("authenticated request missing signature fields: %s", r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"success":true,"data":[
			{"id":"9001","symbol":"BTC_USDT","side":2,"vol":3,"price":65000,"feeCurrency":"USDT","fee":0.12,"timestamp":1700000000000,"profit":2.5,"isTaker":true,"orderId":"123"},
			{"id":9002,"symbol":"BTC_USDT","side":4,"vol":1,"price":65100,"feeCurrency":"USDT","fee":0.01,"timestamp":"1700000000001","profit":0,"isTaker":false,"orderId":123}]}`))
	}))
	defer server.Close()

	client := NewMEXCClient("api-key", "secret-key", false)
	client.baseURL = server.URL
	client.httpClient = server.Client()
	adapter := &Adapter{client: client, symbol: "BTC_USDT", settleAsset: "USDT"}
	fills, err := adapter.GetOrderFills(context.Background(), 123)
	if err != nil {
		t.Fatalf("GetOrderFills: %v", err)
	}
	if len(fills) != 2 {
		t.Fatalf("fills=%d, want 2", len(fills))
	}
	if fills[0].TradeID != "9001" || fills[0].Side != SideBuy || fills[0].Commission != 0.12 ||
		fills[0].RealizedPnL != 2.5 || !fills[0].RealizedPnLKnown || fills[0].RealizedPnLAsset != "USDT" || fills[0].IsMaker {
		t.Fatalf("first fill mapping incorrect: %+v", fills[0])
	}
	if fills[1].TradeID != "9002" || fills[1].Side != SideSell || !fills[1].IsMaker || !fills[1].RealizedPnLKnown {
		t.Fatalf("second fill mapping incorrect: %+v", fills[1])
	}
}
