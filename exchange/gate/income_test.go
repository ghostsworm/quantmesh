package gate

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

func TestGetIncomeHistoryUsesSignedPaginatedContractAccountBook(t *testing.T) {
	start := int64(1710000000000)
	end := start + 5000
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v4/futures/usdt/account_book" || r.Method != http.MethodGet {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.RequestURI())
		}
		query := r.URL.Query()
		if query.Get("contract") != "BTC_USDT" || query.Get("from") != strconv.FormatInt(start/1000, 10) ||
			query.Get("to") != strconv.FormatInt(end/1000, 10) || query.Get("type") != "fund" || query.Get("limit") != "100" {
			t.Fatalf("unexpected query: %s", r.URL.RawQuery)
		}
		timestamp, err := strconv.ParseInt(r.Header.Get("Timestamp"), 10, 64)
		if err != nil || r.Header.Get("KEY") != "api-key" {
			t.Fatal("missing Gate authentication headers")
		}
		if got, want := r.Header.Get("SIGN"), NewSigner("api-key", "secret").SignREST(http.MethodGet, "/api/v4/futures/usdt/account_book", r.URL.RawQuery, "", timestamp); got != want {
			t.Fatalf("signature=%s want=%s", got, want)
		}
		requests++
		if query.Get("offset") != strconv.Itoa((requests-1)*gateFuturesAccountBookPageSize) {
			t.Fatalf("unexpected offset: %s", r.URL.RawQuery)
		}
		count := gateFuturesAccountBookPageSize
		if requests == 2 {
			count = 1
		} else if requests > 2 {
			t.Fatalf("unexpected request number %d", requests)
		}
		rows := make([]map[string]any, count)
		for i := range rows {
			id := (requests-1)*gateFuturesAccountBookPageSize + i + 1
			rows[i] = map[string]any{"id": fmt.Sprintf("ledger-%d", id), "type": "fund", "contract": "BTC_USDT",
				"change": "-0.125", "balance": "4.5", "text": "BTC_USDT funding", "time": float64(start/1000) + float64(id)/100,
			}
		}
		if err := json.NewEncoder(w).Encode(rows); err != nil {
			t.Fatalf("encode response: %v", err)
		}
	}))
	defer server.Close()

	client := NewClient("api-key", "secret", false)
	client.baseURL = server.URL + "/api/v4"
	client.httpClient = server.Client()
	adapter := &GateAdapter{client: client, symbol: "BTCUSDT", gateSymbol: "BTC_USDT", settle: "usdt"}
	got, err := adapter.GetIncomeHistory(context.Background(), "BTCUSDT", "FUNDING_FEE", start, end)
	if err != nil {
		t.Fatalf("GetIncomeHistory: %v", err)
	}
	if requests != 2 || len(got) != 101 {
		t.Fatalf("requests=%d entries=%d, want 2 pages and 101 entries", requests, len(got))
	}
	firstDelta := got[0].TradeTime.Sub(time.UnixMilli(start))
	if got[0].Income != -0.125 || got[0].Asset != "USDT" || got[0].IncomeType != "FUNDING_FEE" ||
		firstDelta < 9*time.Millisecond || firstDelta > 11*time.Millisecond {
		t.Fatalf("first account-book entry mapped incorrectly: %+v", got[0])
	}
	if got[100].TransactionID <= 0 || got[100].TransactionID == got[0].TransactionID {
		t.Fatalf("account-book identity mapping invalid: first=%d last=%d", got[0].TransactionID, got[100].TransactionID)
	}
}

func TestGetIncomeHistoryRejectsUnverifiableRangeAndMismatchedSymbol(t *testing.T) {
	adapter := &GateAdapter{client: NewClient("key", "secret", false), symbol: "BTCUSDT", gateSymbol: "BTC_USDT", settle: "usdt"}
	if _, err := adapter.GetIncomeHistory(context.Background(), "BTCUSDT", "FUNDING_FEE", gateContractLedgerFilterStartMs-1, gateContractLedgerFilterStartMs); err == nil {
		t.Fatal("pre-contract-identity range unexpectedly succeeded")
	}
	if _, err := adapter.GetIncomeHistory(context.Background(), "ETHUSDT", "FUNDING_FEE", gateContractLedgerFilterStartMs, gateContractLedgerFilterStartMs+1); err == nil {
		t.Fatal("mismatched symbol unexpectedly succeeded")
	}
	if _, err := adapter.GetIncomeHistory(context.Background(), "BTCUSDT", "REALIZED_PNL", gateContractLedgerFilterStartMs, gateContractLedgerFilterStartMs+1); err == nil {
		t.Fatal("unsupported income type unexpectedly succeeded")
	}
}
