package coinex

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

func TestGetOrderFillsUsesSignedPaginationAndMapsFinancialFields(t *testing.T) {
	pageRequests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/futures/order-deals" || r.Method != http.MethodGet {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.RequestURI())
		}
		query := r.URL.Query()
		if query.Get("market") != "BTCUSDT" || query.Get("market_type") != "FUTURES" || query.Get("order_id") != "900" || query.Get("limit") != "100" {
			t.Fatalf("unexpected filters: %s", r.URL.RawQuery)
		}
		timestamp := r.Header.Get("X-COINEX-TIMESTAMP")
		if r.Header.Get("X-COINEX-KEY") != "api-key" || timestamp == "" {
			t.Fatalf("missing auth headers: %#v", r.Header)
		}
		mac := hmac.New(sha256.New, []byte("secret"))
		_, _ = mac.Write([]byte(http.MethodGet + r.URL.RequestURI() + timestamp))
		if got, want := r.Header.Get("X-COINEX-SIGN"), hex.EncodeToString(mac.Sum(nil)); got != want {
			t.Fatalf("signature=%s want=%s", got, want)
		}
		pageRequests++
		if pageRequests == 2 && query.Get("page") != "2" {
			t.Fatalf("second page not requested: %s", r.URL.RawQuery)
		}
		count := 100
		hasNext := pageRequests == 1
		if !hasNext {
			count = 1
		}
		deals := make([]map[string]interface{}, count)
		for i := range deals {
			id := (pageRequests-1)*100 + i + 1
			deals[i] = map[string]interface{}{"deal_id": id, "created_at": 1700000000000 + id, "order_id": 900,
				"market": "BTCUSDT", "side": "buy", "price": "65000", "amount": "0.01", "role": "taker",
				"fee": "0.02", "fee_ccy": "USDT", "realized_pnl": "1.5"}
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]interface{}{"code": 0, "data": deals, "pagination": map[string]bool{"has_next": hasNext}}); err != nil {
			t.Fatalf("encode response: %v", err)
		}
	}))
	defer server.Close()

	client := NewCoinExClient("api-key", "secret", false)
	client.baseURL = server.URL
	client.httpClient = server.Client()
	adapter := &Adapter{client: client, market: "BTCUSDT", quoteAsset: "USDT"}
	fills, err := adapter.GetOrderFills(context.Background(), 900)
	if err != nil {
		t.Fatalf("GetOrderFills: %v", err)
	}
	if pageRequests != 2 || len(fills) != 101 {
		t.Fatalf("requests=%d fills=%d, want 2 requests and 101 fills", pageRequests, len(fills))
	}
	first := fills[0]
	if first.TradeID != "1" || first.Commission != 0.02 || first.CommissionAsset != "USDT" ||
		first.RealizedPnL != 1.5 || !first.RealizedPnLKnown || first.RealizedPnLAsset != "USDT" || first.IsMaker {
		t.Fatalf("financial mapping incorrect: %+v", first)
	}
	if _, err := strconv.ParseInt(fills[len(fills)-1].TradeID, 10, 64); err != nil {
		t.Fatalf("trade ID is not numeric: %v", err)
	}
	if fmt.Sprint(fills[len(fills)-1].TradeTime) != "1700000000101" {
		t.Fatalf("last fill time = %d", fills[len(fills)-1].TradeTime)
	}
}
