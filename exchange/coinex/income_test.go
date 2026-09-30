package coinex

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetIncomeHistoryUsesSignedFundingHistoryAndMapsCashFlow(t *testing.T) {
	page := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/futures/position-funding-history" || r.Method != http.MethodGet {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.RequestURI())
		}
		query := r.URL.Query()
		if query.Get("market") != "BTCUSDT" || query.Get("market_type") != "FUTURES" ||
			query.Get("start_time") != "1700000000000" || query.Get("end_time") != "1700000100000" || query.Get("limit") != "100" {
			t.Fatalf("unexpected query: %s", r.URL.RawQuery)
		}
		timestamp := r.Header.Get("X-COINEX-TIMESTAMP")
		if r.Header.Get("X-COINEX-KEY") != "api-key" || timestamp == "" {
			t.Fatalf("missing authentication headers")
		}
		mac := hmac.New(sha256.New, []byte("secret"))
		_, _ = mac.Write([]byte(http.MethodGet + r.URL.RequestURI() + timestamp))
		if got, want := r.Header.Get("X-COINEX-SIGN"), hex.EncodeToString(mac.Sum(nil)); got != want {
			t.Fatalf("signature=%s want=%s", got, want)
		}
		page++
		if page == 1 && query.Get("page") != "1" || page == 2 && query.Get("page") != "2" {
			t.Fatalf("unexpected page query: %s", r.URL.RawQuery)
		}
		data := []map[string]any{}
		if page == 1 || page == 3 {
			data = append(data, map[string]any{"market": "BTCUSDT", "market_type": "FUTURES", "ccy": "USDT", "position_id": 17,
				"side": "long", "funding_rate": "0.001", "funding_value": "2.5", "created_at": 1700000001000})
		} else {
			data = append(data, map[string]any{"market": "BTCUSDT", "market_type": "FUTURES", "ccy": "USDT", "position_id": 18,
				"side": "short", "funding_rate": "0.001", "funding_value": "1.25", "created_at": 1700000002000})
		}
		if err := json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": data, "pagination": map[string]bool{"has_next": page == 1}}); err != nil {
			t.Fatalf("encode response: %v", err)
		}
	}))
	defer server.Close()

	client := NewCoinExClient("api-key", "secret", false)
	client.baseURL = server.URL
	client.httpClient = server.Client()
	adapter := &Adapter{client: client, market: "BTCUSDT"}
	got, err := adapter.GetIncomeHistory(context.Background(), "BTCUSDT", "FUNDING_FEE", 1700000000000, 1700000100000)
	if err != nil {
		t.Fatalf("GetIncomeHistory: %v", err)
	}
	if page != 2 || len(got) != 2 {
		t.Fatalf("pages=%d entries=%d, want 2 pages and entries", page, len(got))
	}
	if got[0].Income != -2.5 || got[0].Asset != "USDT" || got[0].IncomeType != "FUNDING_FEE" || got[0].TradeTime.UnixMilli() != 1700000001000 {
		t.Fatalf("long funding entry mapped incorrectly: %+v", got[0])
	}
	if got[1].Income != 1.25 || got[1].TransactionID <= 0 || got[1].Info == "" {
		t.Fatalf("short funding entry mapped incorrectly: %+v", got[1])
	}
	if got[0].TransactionID == got[1].TransactionID {
		t.Fatalf("distinct funding entries share transaction ID %d", got[0].TransactionID)
	}
	again, err := adapter.GetIncomeHistory(context.Background(), "BTCUSDT", "FUNDING_FEE", 1700000000000, 1700000100000)
	if err != nil || again[0].TransactionID != got[0].TransactionID {
		t.Fatalf("transaction ID is not stable across calls: first=%d second=%v err=%v", got[0].TransactionID, again, err)
	}
}

func TestGetIncomeHistoryRejectsUnsupportedTypeAndWrongSymbol(t *testing.T) {
	adapter := &Adapter{client: NewCoinExClient("key", "secret", false), market: "BTCUSDT"}
	if _, err := adapter.GetIncomeHistory(context.Background(), "BTCUSDT", "REALIZED_PNL", 1, 2); err == nil {
		t.Fatal("unsupported income type unexpectedly succeeded")
	}
	if _, err := adapter.GetIncomeHistory(context.Background(), "ETHUSDT", "FUNDING_FEE", 1, 2); err == nil {
		t.Fatal("mismatched market unexpectedly succeeded")
	}
}

func TestGetIncomeHistoryRejectsNonzeroValueWithZeroFundingRate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": []map[string]any{{
			"market": "BTCUSDT", "market_type": "FUTURES", "ccy": "USDT", "position_id": 77,
			"side": "long", "funding_rate": "0", "funding_value": "2.5", "created_at": 1700000001000,
		}}, "pagination": map[string]bool{"has_next": false}}); err != nil {
			t.Fatalf("encode response: %v", err)
		}
	}))
	defer server.Close()

	client := NewCoinExClient("api-key", "secret", false)
	client.baseURL = server.URL
	client.httpClient = server.Client()
	adapter := &Adapter{client: client, market: "BTCUSDT"}
	if entries, err := adapter.GetIncomeHistory(context.Background(), "BTCUSDT", "FUNDING_FEE", 1700000000000, 1700000100000); err == nil || entries != nil {
		t.Fatalf("ambiguous zero-rate funding entry accepted: entries=%+v err=%v", entries, err)
	}
}
