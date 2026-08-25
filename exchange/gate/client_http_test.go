package gate

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newMockGateClient(t *testing.T, handler http.HandlerFunc) (*Client, func()) {
	t.Helper()
	server := httptest.NewServer(handler)
	client := NewClient("api-key", "secret-key", false)
	client.baseURL = server.URL
	client.httpClient = server.Client()
	return client, server.Close
}

func TestGateClientDoRequestAndErrorLabels(t *testing.T) {
	client, closeServer := newMockGateClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("KEY") != "api-key" || r.Header.Get("SIGN") == "" || r.Header.Get("Timestamp") == "" {
			t.Fatalf("signed headers missing")
		}
		if r.Header.Get("X-Gate-Channel-Id") != GateChannelID {
			t.Fatalf("channel header = %q", r.Header.Get("X-Gate-Channel-Id"))
		}
		if r.URL.RawQuery != "a=1" {
			t.Fatalf("query = %q", r.URL.RawQuery)
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	defer closeServer()

	data, err := client.DoRequest(context.Background(), http.MethodGet, "/demo", "a=1", nil)
	if err != nil || !strings.Contains(string(data), `"ok":true`) {
		t.Fatalf("DoRequest() = %s, %v", data, err)
	}

	cases := []struct {
		label string
		want  string
	}{
		{"USER_NOT_FOUND", "合約账戶未激活"},
		{"INVALID_SIGNATURE", "签名錯误"},
		{"INVALID_KEY", "API Key 無效"},
		{"OTHER", "Gate.io API 錯误"},
	}
	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			errClient, closeErrServer := newMockGateClient(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusUnauthorized)
				_ = json.NewEncoder(w).Encode(GateResponse{Label: tc.label, Message: "bad"})
			})
			defer closeErrServer()
			_, err := errClient.DoRequest(context.Background(), http.MethodGet, "/bad", "", nil)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected %q error, got %v", tc.want, err)
			}
		})
	}
}

func TestGateClientFuturesMethodsWithMockServer(t *testing.T) {
	client, closeServer := newMockGateClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/futures/usdt/contracts/BTC_USDT":
			_, _ = w.Write([]byte(`{"name":"BTC_USDT","type":"direct","order_price_round":"0.1","order_size_min":1}`))
		case "/futures/usdt/accounts":
			_, _ = w.Write([]byte(`{"user":1,"currency":"USDT","total":"1000","available":"800","in_dual_mode":true}`))
		case "/futures/usdt/positions":
			_, _ = w.Write([]byte(`[{"contract":"BTC_USDT","size":2,"leverage":"5","entry_price":"60000"}]`))
		case "/futures/usdt/positions/BTC_USDT":
			_, _ = w.Write([]byte(`{"contract":"BTC_USDT","size":2,"leverage":"5","entry_price":"60000"}`))
		case "/futures/usdt/orders":
			if r.Method == http.MethodPost {
				_, _ = w.Write([]byte(`{"id":11,"contract":"BTC_USDT","status":"open","size":1,"price":"65000"}`))
				return
			}
			if r.URL.Query().Get("contract") != "BTC_USDT" || r.URL.Query().Get("status") != "open" {
				t.Fatalf("open order query = %s", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(`[{"id":12,"contract":"BTC_USDT","status":"open"}]`))
		case "/futures/usdt/orders/11":
			if r.Method == http.MethodDelete {
				_, _ = w.Write([]byte(`{"id":11,"status":"finished","finish_as":"cancelled"}`))
				return
			}
			_, _ = w.Write([]byte(`{"id":11,"contract":"BTC_USDT","status":"open"}`))
		case "/futures/usdt/batch_cancel_orders":
			var ids []string
			if err := json.NewDecoder(r.Body).Decode(&ids); err != nil {
				t.Fatalf("decode ids: %v", err)
			}
			if len(ids) != 20 {
				t.Fatalf("batch cancel should trim to 20 ids, got %d", len(ids))
			}
			_, _ = w.Write([]byte(`[{"id":"1","succeeded":true}]`))
		case "/futures/usdt/candlesticks":
			if r.URL.Query().Get("contract") != "BTC_USDT" || r.URL.Query().Get("interval") != "1m" {
				t.Fatalf("candlestick query = %s", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(`[{"t":1,"v":100,"o":"1","h":"2","l":"0.5","c":"1.5"}]`))
		case "/futures/usdt/order_book":
			_, _ = w.Write([]byte(`{"id":1,"asks":[{"p":"2","s":1}],"bids":[{"p":"1","s":2}]}`))
		case "/futures/usdt/positions/BTC_USDT/leverage":
			_, _ = w.Write([]byte(`{}`))
		case "/wallet/transfers":
			_, _ = w.Write([]byte(`{"tx_id":99}`))
		default:
			http.NotFound(w, r)
		}
	})
	defer closeServer()

	ctx := context.Background()
	contract, err := client.GetContract(ctx, "usdt", "BTC_USDT")
	if err != nil || contract.Name != "BTC_USDT" {
		t.Fatalf("GetContract() = %#v, %v", contract, err)
	}
	account, err := client.GetAccount(ctx, "usdt")
	if err != nil || account.User != 1 || !account.InDualMode {
		t.Fatalf("GetAccount() = %#v, %v", account, err)
	}
	positions, err := client.GetPositions(ctx, "usdt")
	if err != nil || len(positions) != 1 || positions[0].Contract != "BTC_USDT" {
		t.Fatalf("GetPositions() = %#v, %v", positions, err)
	}
	position, err := client.GetPosition(ctx, "usdt", "BTC_USDT")
	if err != nil || position.Size != 2 {
		t.Fatalf("GetPosition() = %#v, %v", position, err)
	}
	order, err := client.PlaceOrder(ctx, "usdt", map[string]interface{}{"contract": "BTC_USDT", "size": 1})
	if err != nil || order.ID != 11 {
		t.Fatalf("PlaceOrder() = %#v, %v", order, err)
	}
	gotOrder, err := client.GetOrder(ctx, "usdt", "11")
	if err != nil || gotOrder.ID != 11 {
		t.Fatalf("GetOrder() = %#v, %v", gotOrder, err)
	}
	ids := make([]string, 25)
	for i := range ids {
		ids[i] = "1"
	}
	results, err := client.BatchCancelOrders(ctx, "usdt", ids)
	if err != nil || len(results) != 1 {
		t.Fatalf("BatchCancelOrders() = %#v, %v", results, err)
	}
	emptyResults, err := client.BatchCancelOrders(ctx, "usdt", nil)
	if err != nil || emptyResults != nil {
		t.Fatalf("empty BatchCancelOrders() = %#v, %v", emptyResults, err)
	}
	canceled, err := client.CancelOrder(ctx, "usdt", "11")
	if err != nil || canceled.FinishAs != "cancelled" {
		t.Fatalf("CancelOrder() = %#v, %v", canceled, err)
	}
	candles, err := client.GetCandlesticks(ctx, "usdt", "BTC_USDT", "1m", 1)
	if err != nil || len(candles) != 1 || candles[0].Close != "1.5" {
		t.Fatalf("GetCandlesticks() = %#v, %v", candles, err)
	}
	book, err := client.GetOrderBook(ctx, "usdt", "BTC_USDT", 5)
	if err != nil || len(book.Asks) != 1 || len(book.Bids) != 1 {
		t.Fatalf("GetOrderBook() = %#v, %v", book, err)
	}
	openOrders, err := client.GetOpenOrders(ctx, "usdt", "BTC_USDT")
	if err != nil || len(openOrders) != 1 || openOrders[0].ID != 12 {
		t.Fatalf("GetOpenOrders() = %#v, %v", openOrders, err)
	}
	if err := client.SetLeverage(ctx, "usdt", "BTC_USDT", 7); err != nil {
		t.Fatalf("SetLeverage() error = %v", err)
	}
	txID, err := client.WalletTransfer(ctx, "USDT", "10", "spot", "futures", "usdt")
	if err != nil || txID != 99 {
		t.Fatalf("WalletTransfer() = %d, %v", txID, err)
	}
}

func TestGateGetPositionArrayFallbackAndInvalidJSON(t *testing.T) {
	t.Run("array fallback", func(t *testing.T) {
		client, closeServer := newMockGateClient(t, func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`[{"contract":"ETH_USDT","size":3}]`))
		})
		defer closeServer()
		position, err := client.GetPosition(context.Background(), "usdt", "ETH_USDT")
		if err != nil || position.Contract != "ETH_USDT" || position.Size != 3 {
			t.Fatalf("GetPosition array fallback = %#v, %v", position, err)
		}
	})

	t.Run("invalid json", func(t *testing.T) {
		client, closeServer := newMockGateClient(t, func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`not-json`))
		})
		defer closeServer()
		if _, err := client.GetContract(context.Background(), "usdt", "BTC_USDT"); err == nil {
			t.Fatal("expected JSON parse error")
		}
	})
}
