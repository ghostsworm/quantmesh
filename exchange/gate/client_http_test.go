package gate

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGetAllFuturesOpenOrdersUsesContractWideLastIDPagination(t *testing.T) {
	requests := 0
	client, closeServer := newMockGateClient(t, func(w http.ResponseWriter, r *http.Request) {
		requests++
		query := r.URL.Query()
		if r.URL.Path != "/futures/usdt/orders" || query.Get("status") != "open" || query.Has("contract") || query.Get("limit") != "100" {
			t.Errorf("unexpected all-contracts futures query: %s", r.URL.RequestURI())
		}
		var payload []byte
		if query.Get("last_id") == "" {
			rows := make([]FuturesOrder, 100)
			for i := range rows {
				rows[i] = FuturesOrder{ID: int64(i + 1), Contract: "ETH_USDT", Status: "open", Size: 1, Left: 1, LeftKnown: true}
			}
			payload, _ = json.Marshal(rows)
		} else if query.Get("last_id") != "100" {
			t.Errorf("last_id=%q, want 100", query.Get("last_id"))
			payload = []byte("[]")
		} else {
			payload = []byte("[]")
		}
		_, _ = w.Write(payload)
	})
	defer closeServer()
	adapter := &GateAdapter{client: client, settle: "usdt", symbol: "BTCUSDT"}
	orders, err := adapter.GetAccountOpenOrders(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if requests != 2 || len(orders) != 100 || orders[99].OrderID != 100 || orders[0].Symbol != "ETH_USDT" {
		t.Fatalf("requests=%d order_count=%d", requests, len(orders))
	}
}

func TestGetAllSpotOpenOrdersQueriesEveryAccountAndPairPage(t *testing.T) {
	accounts := map[string]bool{}
	client, closeServer := newMockGateClient(t, func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		account, page := query.Get("account"), query.Get("page")
		accounts[account] = true
		if r.URL.Path != "/spot/open_orders" || query.Has("currency_pair") || query.Get("limit") != "100" {
			t.Errorf("unexpected all-pairs spot query: %s", r.URL.RequestURI())
		}
		var payload []byte
		if page == "1" {
			payload = []byte(fmt.Sprintf(`[{"currency_pair":"ETH_USDT","orders":[{"id":"100","currency_pair":"ETH_USDT","side":"buy","type":"limit","amount":"1","filled_amount":"0","price":"10","status":"open","text":%q}]}]`, account))
		} else if page == "2" {
			payload = []byte(fmt.Sprintf(`[{"currency_pair":"BTC_USDT","orders":[{"id":"200","currency_pair":"BTC_USDT","side":"sell","type":"limit","amount":"1","filled_amount":"0","price":"20","status":"open","text":%q}]}]`, account))
		} else if page == "3" {
			payload = []byte("[]")
		} else {
			t.Errorf("unexpected page=%q", page)
			payload = []byte("[]")
		}
		_, _ = w.Write(payload)
	})
	defer closeServer()
	adapter := &GateSpotAdapter{client: client}
	all, err := adapter.GetAccountOpenOrders(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(accounts) != 4 || len(all) != 8 {
		t.Fatalf("accounts=%v order_count=%d", accounts, len(all))
	}
	for _, account := range []string{"spot", "margin", "cross_margin", "unified"} {
		if !accounts[account] {
			t.Errorf("account %q was not queried", account)
		}
	}
}

func TestGetAllFuturesOpenOrdersRejectsRepeatedLastID(t *testing.T) {
	client, closeServer := newMockGateClient(t, func(w http.ResponseWriter, r *http.Request) {
		rows := make([]FuturesOrder, 100)
		for i := range rows {
			rows[i] = FuturesOrder{ID: int64(i + 1), Contract: "ETH_USDT", Status: "open", Size: 1, Left: 1, LeftKnown: true}
		}
		payload, _ := json.Marshal(rows)
		_, _ = w.Write(payload)
	})
	defer closeServer()
	if orders, err := client.GetAllFuturesOpenOrders(context.Background(), "usdt"); err == nil || orders != nil {
		t.Fatalf("repeated last_id returned orders=%v err=%v", orders, err)
	}
}

func TestGetAllSpotOpenOrdersRejectsNullPage(t *testing.T) {
	client, closeServer := newMockGateClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("null"))
	})
	defer closeServer()
	if orders, err := client.GetAllSpotOpenOrders(context.Background(), "spot"); err == nil || orders != nil {
		t.Fatalf("null page returned orders=%v err=%v", orders, err)
	}
}

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

func TestGateClientGetOrderByClientOrderID(t *testing.T) {
	client, closeServer := newMockGateClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/futures/usdt/orders/t-close-1" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"id":17,"contract":"BTC_USDT","text":"t-close-1","status":"finished","finish_as":"cancelled","size":"-3","left":"-1","price":"60000","fill_price":"59990"}`))
	})
	defer closeServer()

	order, err := client.GetOrderByClientOrderID(context.Background(), "usdt", "t-close-1")
	if err != nil {
		t.Fatal(err)
	}
	if order.ID != 17 || order.Text != "t-close-1" || order.Contract != "BTC_USDT" || order.FillSize != 2 {
		t.Fatalf("unexpected order: %#v", order)
	}
}

func TestGateClientGetMyFuturesTradesPagesByOrder(t *testing.T) {
	client, closeServer := newMockGateClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/futures/usdt/my_trades" || r.URL.Query().Get("contract") != "BTC_USDT" ||
			r.URL.Query().Get("order") != "17" || r.URL.Query().Get("limit") != "1000" || r.URL.Query().Get("offset") != "0" {
			t.Fatalf("unexpected request: %s?%s", r.URL.Path, r.URL.RawQuery)
		}
		_, _ = w.Write([]byte(`[{"id":91,"create_time":1700000000.25,"contract":"BTC_USDT","order_id":"17","size":"-2","price":"59990","text":"t-close-1","fee":"0.01","role":"maker"}]`))
	})
	defer closeServer()

	trades, err := client.GetMyFuturesTrades(context.Background(), "usdt", "BTC_USDT", 17)
	if err != nil {
		t.Fatal(err)
	}
	if len(trades) != 1 || trades[0].ID != 91 || trades[0].OrderID != "17" || trades[0].Size != "-2" {
		t.Fatalf("unexpected trades: %#v", trades)
	}
}

func TestGateAdapterGetOrderFillsConvertsContractsAndFee(t *testing.T) {
	client, closeServer := newMockGateClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/futures/usdt/my_trades" || r.URL.Query().Get("order") != "17" {
			t.Fatalf("unexpected request: %s?%s", r.URL.Path, r.URL.RawQuery)
		}
		_, _ = w.Write([]byte(`[{"id":91,"create_time":1700000000.25,"contract":"BTC_USDT","order_id":"17","size":"-2","price":"59990","text":"t-close-1","fee":"0.01","role":"maker"}]`))
	})
	defer closeServer()
	adapter := &GateAdapter{client: client, symbol: "BTCUSDT", gateSymbol: "BTC_USDT", settle: "usdt", quantoMultiplier: 0.01}

	fills, err := adapter.GetOrderFills(context.Background(), "BTCUSDT", 17)
	if err != nil {
		t.Fatal(err)
	}
	if len(fills) != 1 || fills[0].TradeID != "91" || fills[0].Side != SideSell ||
		fills[0].Quantity != 0.02 || fills[0].Price != 59990 || fills[0].Commission != 0.01 ||
		fills[0].CommissionAsset != "USDT" || fills[0].TradeTime != 1700000000250 {
		t.Fatalf("unexpected converted fills: %#v", fills)
	}
}

func TestGateAdapterGetOrderByClientOrderIDValidatesIdentity(t *testing.T) {
	for _, tc := range []struct {
		name, response string
		wantErr        bool
	}{
		{"exact identity", `{"id":17,"contract":"BTC_USDT","text":"t-close-1","status":"finished","finish_as":"cancelled","size":"-3","left":"-1","price":"60000","fill_price":"59990"}`, false},
		{"wrong text", `{"id":17,"contract":"BTC_USDT","text":"t-other","status":"finished","size":-3}`, true},
		{"wrong contract", `{"id":17,"contract":"ETH_USDT","text":"t-close-1","status":"finished","size":-3}`, true},
		{"missing native ID", `{"id":0,"contract":"BTC_USDT","text":"t-close-1","status":"finished","size":-3}`, true},
		{"missing remaining quantity", `{"id":17,"contract":"BTC_USDT","text":"t-close-1","status":"finished","finish_as":"filled","size":"-3"}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client, closeServer := newMockGateClient(t, func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte(tc.response))
			})
			defer closeServer()
			adapter := &GateAdapter{client: client, symbol: "BTCUSDT", gateSymbol: "BTC_USDT", settle: "usdt", quantoMultiplier: 0.01}
			order, err := adapter.GetOrderByClientOrderID(context.Background(), "BTCUSDT", "close-1")
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected identity validation error")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if order.OrderID != 17 || order.ClientOrderID != "t-close-1" || order.Quantity != 0.03 || order.ExecutedQty != 0.02 || order.AvgPrice != 59990 || order.Status != "CANCELED" {
				t.Fatalf("unexpected converted order: %#v", order)
			}
		})
	}
}

func TestFuturesOrderUnmarshalDerivesExecutedContractsFromLeft(t *testing.T) {
	tests := []struct {
		name, data string
		want       int64
		known      bool
		wantErr    bool
	}{
		{"documented string fields", `{"size":"-5","left":"-2"}`, 3, true, false},
		{"numeric fields", `{"size":5,"left":2}`, 3, true, false},
		{"positive remaining on sell", `{"size":"-5","left":"2"}`, 3, true, false},
		{"missing left", `{"size":"5"}`, 0, false, false},
		{"remaining exceeds size", `{"size":"2","left":"3"}`, 0, false, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var order FuturesOrder
			err := json.Unmarshal([]byte(tc.data), &order)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected inconsistent quantity error")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if order.FillSize != tc.want || order.LeftKnown != tc.known {
				t.Fatalf("FillSize=%d LeftKnown=%v; want %d, %v", order.FillSize, order.LeftKnown, tc.want, tc.known)
			}
		})
	}
}

func TestConvertRecoveryOrderStatusUsesFinishReason(t *testing.T) {
	tests := []struct {
		status, finishAs string
		fillSize         int64
		want             OrderStatus
		wantErr          bool
	}{
		{"open", "", 0, "NEW", false},
		{"open", "", 1, "PARTIALLY_FILLED", false},
		{"finished", "filled", 3, "FILLED", false},
		{"finished", "cancelled", 1, "CANCELED", false},
		{"finished", "ioc", 1, "CANCELED", false},
		{"finished", "", 0, "", true},
		{"unknown", "", 0, "", true},
	}
	for _, tc := range tests {
		got, err := convertRecoveryOrderStatus(tc.status, tc.finishAs, tc.fillSize)
		if tc.wantErr {
			if err == nil {
				t.Errorf("convertRecoveryOrderStatus(%q, %q, %d) expected error", tc.status, tc.finishAs, tc.fillSize)
			}
			continue
		}
		if err != nil || got != tc.want {
			t.Errorf("convertRecoveryOrderStatus(%q, %q, %d) = %q, %v; want %q", tc.status, tc.finishAs, tc.fillSize, got, err, tc.want)
		}
	}
}

func TestGateAdapterRecoveryRejectsNonUSDTSettlement(t *testing.T) {
	adapter := &GateAdapter{symbol: "BTCUSD", settle: "btc"}
	if _, err := adapter.GetOrderByClientOrderID(context.Background(), "BTCUSD", "close-1"); err == nil {
		t.Fatal("expected non-USDT order recovery to fail closed")
	}
	if _, err := adapter.GetOrderFills(context.Background(), "BTCUSD", 17); err == nil {
		t.Fatal("expected non-USDT fill recovery to fail closed")
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
