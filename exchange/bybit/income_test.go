package bybit

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

func TestGetIncomeHistoryUsesSignedCursorAndSplitsSevenDayWindows(t *testing.T) {
	start := int64(1700000000000)
	firstEnd := start + bybitTransactionLogMaxWindowMs
	end := firstEnd + 1000
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v5/account/transaction-log" || r.Method != http.MethodGet {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.RequestURI())
		}
		query := r.URL.Query()
		if query.Get("accountType") != "UNIFIED" || query.Get("category") != "linear" || query.Get("limit") != "50" {
			t.Fatalf("unexpected filters: %s", r.URL.RawQuery)
		}
		timestamp := r.Header.Get("X-BAPI-TIMESTAMP")
		if r.Header.Get("X-BAPI-API-KEY") != "api-key" || timestamp == "" || r.Header.Get("X-BAPI-RECV-WINDOW") != "5000" {
			t.Fatal("missing Bybit authentication headers")
		}
		mac := hmac.New(sha256.New, []byte("secret"))
		_, _ = mac.Write([]byte(timestamp + "api-key5000" + r.URL.RawQuery))
		if got, want := r.Header.Get("X-BAPI-SIGN"), hex.EncodeToString(mac.Sum(nil)); got != want {
			t.Fatalf("signature=%s want=%s", got, want)
		}
		requests++
		var rows []map[string]string
		var next string
		switch requests {
		case 1:
			if query.Get("startTime") != strconv.FormatInt(start, 10) || query.Get("endTime") != strconv.FormatInt(firstEnd, 10) || query.Get("cursor") != "" {
				t.Fatalf("unexpected first window/page: %s", r.URL.RawQuery)
			}
			rows = []map[string]string{bybitFundingTestRow("funding-1", "BTCUSDT", "SETTLEMENT", "-0.5", "USDT", start+1)}
			next = "cursor:next"
		case 2:
			if query.Get("startTime") != strconv.FormatInt(start, 10) || query.Get("endTime") != strconv.FormatInt(firstEnd, 10) || query.Get("cursor") != "cursor:next" {
				t.Fatalf("unexpected cursor page: %s", r.URL.RawQuery)
			}
			rows = []map[string]string{bybitFundingTestRow("trade-row", "BTCUSDT", "TRADE", "", "USDT", start+2)}
		case 3:
			if query.Get("startTime") != strconv.FormatInt(firstEnd+1, 10) || query.Get("endTime") != strconv.FormatInt(end, 10) || query.Get("cursor") != "" {
				t.Fatalf("unexpected second window: %s", r.URL.RawQuery)
			}
			rows = []map[string]string{bybitFundingTestRow("funding-2", "BTCUSDT", "SETTLEMENT", "0.25", "USDT", end)}
		default:
			t.Fatalf("unexpected request number %d", requests)
		}
		if err := json.NewEncoder(w).Encode(map[string]any{"retCode": 0, "retMsg": "OK", "result": map[string]any{
			"list": rows, "nextPageCursor": next,
		}}); err != nil {
			t.Fatalf("encode response: %v", err)
		}
	}))
	defer server.Close()

	client := NewBybitClient("api-key", "secret", false)
	client.baseURL = server.URL
	client.httpClient = server.Client()
	adapter := &BybitAdapter{client: client, symbol: "BTCUSDT"}
	got, err := adapter.GetIncomeHistory(context.Background(), "BTCUSDT", "FUNDING_FEE", start, end)
	if err != nil {
		t.Fatalf("GetIncomeHistory: %v", err)
	}
	if requests != 3 || len(got) != 2 {
		t.Fatalf("requests=%d entries=%d, want 3 requests and 2 funding entries", requests, len(got))
	}
	if got[0].Income != -0.5 || got[0].Asset != "USDT" || got[0].TradeTime.UnixMilli() != start+1 || got[0].IncomeType != "FUNDING_FEE" {
		t.Fatalf("negative funding entry mapped incorrectly: %+v", got[0])
	}
	if got[1].Income != 0.25 || got[1].Asset != "USDT" || got[1].TradeTime.UnixMilli() != end {
		t.Fatalf("positive funding entry mapped incorrectly: %+v", got[1])
	}
	if got[0].TransactionID <= 0 || got[1].TransactionID <= 0 || got[0].TransactionID == got[1].TransactionID {
		t.Fatalf("invalid funding transaction identities: %d, %d", got[0].TransactionID, got[1].TransactionID)
	}
}

func bybitFundingTestRow(id, symbol, transactionType, funding, currency string, timestamp int64) map[string]string {
	return map[string]string{"id": id, "symbol": symbol, "category": "linear", "currency": currency,
		"transactionTime": strconv.FormatInt(timestamp, 10), "type": transactionType, "funding": funding}
}

func TestGetIncomeHistoryRejectsUnsupportedTypeAndSymbol(t *testing.T) {
	adapter := &BybitAdapter{client: NewBybitClient("key", "secret", false), symbol: "BTCUSDT"}
	if _, err := adapter.GetIncomeHistory(context.Background(), "BTCUSDT", "REALIZED_PNL", 1, 2); err == nil {
		t.Fatal("unsupported income type unexpectedly succeeded")
	}
	if _, err := adapter.GetIncomeHistory(context.Background(), "ETHUSDT", "FUNDING_FEE", 1, 2); err == nil {
		t.Fatal("mismatched symbol unexpectedly succeeded")
	}
}

func TestBybitTransactionLogPageReturnsCursor(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("cursor"); got != "next" {
			t.Fatalf("cursor=%q, want next", got)
		}
		_, _ = fmt.Fprint(w, `{"retCode":0,"retMsg":"OK","result":{"list":[],"nextPageCursor":""}}`)
	}))
	defer server.Close()
	client := NewBybitClient("key", "secret", false)
	client.baseURL = server.URL
	client.httpClient = server.Client()
	if _, cursor, err := client.GetTransactionLogPage(context.Background(), "linear", 1, 2, "next"); err != nil || cursor != "" {
		t.Fatalf("page cursor=%q err=%v", cursor, err)
	}
}
