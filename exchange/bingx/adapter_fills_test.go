package bingx

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

func TestGetOrderFillsUsesAuthenticatedCursorAndMapsFinanceFields(t *testing.T) {
	var pageRequests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/openApi/swap/v2/trade/fillHistory" || r.Method != http.MethodGet {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.RequestURI())
		}
		query := r.URL.Query()
		if r.Header.Get("X-BX-APIKEY") != "api-key" || query.Get("signature") == "" || query.Get("orderId") != "77" ||
			query.Get("currency") != "USDT" || query.Get("startTs") != "0" || query.Get("pageSize") != "1000" {
			t.Fatalf("request missing auth or filter parameters: %s", r.URL.RawQuery)
		}
		pageRequests++
		if pageRequests == 2 && (query.Get("pageIndex") != "2" || query.Get("lastFillId") != "1000") {
			t.Fatalf("cursor did not advance: %s", r.URL.RawQuery)
		}
		count := 1000
		if pageRequests == 2 {
			count = 1
		}
		fills := make([]map[string]interface{}, count)
		for i := 0; i < count; i++ {
			id := (pageRequests-1)*1000 + i + 1
			fills[i] = map[string]interface{}{"tradeId": id, "symbol": "BTC-USDT", "orderId": 77, "side": "BUY",
				"price": "65000", "qty": "0.01", "realizedPnl": "1.25", "fee": "-0.02", "time": 1700000000000 + id}
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]interface{}{"code": 0, "data": map[string]interface{}{"fill_orders": fills}}); err != nil {
			t.Fatalf("encode mock response: %v", err)
		}
	}))
	defer server.Close()

	client := NewBingXClient("api-key", "secret", false)
	client.baseURL = server.URL
	client.httpClient = server.Client()
	adapter := &Adapter{client: client, symbol: "BTC-USDT", quoteAsset: "USDT"}
	fills, err := adapter.GetOrderFills(context.Background(), 77)
	if err != nil {
		t.Fatalf("GetOrderFills: %v", err)
	}
	if pageRequests != 2 || len(fills) != 1001 {
		t.Fatalf("requests=%d fills=%d, want 2 requests and 1001 fills", pageRequests, len(fills))
	}
	if fills[0].TradeID != "1" || fills[0].Symbol != "BTCUSDT" || fills[0].Commission != 0.02 ||
		fills[0].RealizedPnL != 1.25 || !fills[0].RealizedPnLKnown || fills[0].RealizedPnLAsset != "USDT" {
		t.Fatalf("fill mapping incorrect: %+v", fills[0])
	}
	if _, err := strconv.ParseInt(fills[len(fills)-1].TradeID, 10, 64); err != nil {
		t.Fatalf("last trade ID %q is invalid: %v", fills[len(fills)-1].TradeID, err)
	}
	if fmt.Sprint(fills[len(fills)-1].TradeTime) != "1700000001001" {
		t.Fatalf("last fill timestamp = %d", fills[len(fills)-1].TradeTime)
	}
}
