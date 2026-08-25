package phemex

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newMockPhemexClient(t *testing.T, handler http.HandlerFunc) (*PhemexClient, func()) {
	t.Helper()
	server := httptest.NewServer(handler)
	client := NewPhemexClient("api-key", "secret-key", false)
	client.baseURL = server.URL
	client.httpClient = server.Client()
	return client, server.Close
}

func TestPhemexClientHTTPMethodsWithMockServer(t *testing.T) {
	accountPositionsPayload := `{"code":0,"msg":"","data":{"account":{"accountId":1,"currency":"BTC","accountBalanceEv":1000},"positions":[{"symbol":"BTCUSD","currency":"BTC","side":"Buy","size":2,"leverageEr":5}]}}`
	client, closeServer := newMockPhemexClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/public/products":
			_, _ = w.Write([]byte(`{"code":0,"msg":"","data":{"perpProductsV2":[{"symbol":"BTCUSD","displaySymbol":"BTC/USD","quoteCurrency":"USD","priceScale":"10000","qtyStepSize":1,"tickSize":"5"}]}}`))
		case "/g-orders":
			if r.Header.Get("x-phemex-access-token") != "api-key" || r.Header.Get("x-phemex-request-expiry") == "" || r.Header.Get("x-phemex-request-signature") == "" {
				t.Fatalf("signed headers missing")
			}
			var req OrderRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode order: %v", err)
			}
			if req.Symbol != "BTCUSD" || req.Side != "Buy" {
				t.Fatalf("unexpected order request: %#v", req)
			}
			_, _ = w.Write([]byte(`{"code":0,"msg":"","data":{"orderID":"order-1","order":{"orderID":"order-1","symbol":"BTCUSD","side":"Buy","ordStatus":"New"}}}`))
		case "/g-orders/cancel":
			if r.Method != http.MethodDelete || r.Header.Get("x-phemex-access-token") != "api-key" {
				t.Fatalf("cancel request not signed")
			}
			_, _ = w.Write([]byte(`{"code":0,"msg":"","data":{}}`))
		case "/exchange/order":
			if r.URL.Query().Get("symbol") != "BTCUSD" || r.URL.Query().Get("orderID") != "order-1" {
				t.Fatalf("order query = %s", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(`{"code":0,"msg":"","data":{"orderID":"order-1","symbol":"BTCUSD","ordStatus":"New"}}`))
		case "/g-orders/activeList":
			_, _ = w.Write([]byte(`{"code":0,"msg":"","data":{"rows":[{"orderID":"order-2","symbol":"BTCUSD","ordStatus":"New"}]}}`))
		case "/g-accounts/accountPositions":
			if r.URL.Query().Get("currency") == "" {
				t.Fatalf("currency query missing")
			}
			_, _ = w.Write([]byte(accountPositionsPayload))
		case "/md/trade":
			_, _ = w.Write([]byte(`{"code":0,"msg":"","data":{"trades":[{"timestamp":1,"symbol":"BTCUSD","side":"Buy","priceEp":650000000,"qty":1,"tradeID":"t1"}]}}`))
		case "/exchange/public/md/v2/kline/list":
			if r.URL.Query().Get("symbol") != "BTCUSD" || r.URL.Query().Get("resolution") != "60" {
				t.Fatalf("kline query = %s", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(`{"code":0,"msg":"","data":{"rows":[[1,60,10,11,12,9,100,1100],{"timestamp":2,"interval":60,"openEp":11,"highEp":13,"lowEp":10,"closeEp":12,"volume":120,"turnoverEv":1300}]}}`))
		default:
			http.NotFound(w, r)
		}
	})
	defer closeServer()

	ctx := context.Background()
	product, err := client.GetProduct(ctx, "BTCUSD")
	if err != nil || product.Symbol != "BTCUSD" || int64(product.PriceScale) != 10000 {
		t.Fatalf("GetProduct() = %#v, %v", product, err)
	}
	order, err := client.PlaceOrder(ctx, &OrderRequest{Symbol: "BTCUSD", Side: "Buy", OrderQty: 1, PriceEp: 650000000, OrdType: "Limit"})
	if err != nil || order.OrderID != "order-1" {
		t.Fatalf("PlaceOrder() = %#v, %v", order, err)
	}
	if err := client.CancelOrder(ctx, "BTCUSD", "order-1"); err != nil {
		t.Fatalf("CancelOrder() error = %v", err)
	}
	gotOrder, err := client.GetOrder(ctx, "BTCUSD", "order-1")
	if err != nil || gotOrder.OrderID != "order-1" {
		t.Fatalf("GetOrder() = %#v, %v", gotOrder, err)
	}
	openOrders, err := client.GetOpenOrders(ctx, "BTCUSD")
	if err != nil || len(openOrders) != 1 || openOrders[0].OrderID != "order-2" {
		t.Fatalf("GetOpenOrders() = %#v, %v", openOrders, err)
	}
	position, err := client.GetPosition(ctx, "BTCUSD")
	if err != nil || position.Symbol != "BTCUSD" || position.Size != 2 {
		t.Fatalf("GetPosition() = %#v, %v", position, err)
	}
	emptyPosition, err := client.GetPosition(ctx, "ETHUSD")
	if err != nil || emptyPosition.Symbol != "ETHUSD" || emptyPosition.Size != 0 {
		t.Fatalf("empty GetPosition() = %#v, %v", emptyPosition, err)
	}
	account, err := client.GetAccount(ctx, "BTC")
	if err != nil || account.AccountID != 1 {
		t.Fatalf("GetAccount() = %#v, %v", account, err)
	}
	trades, err := client.GetTrades(ctx, "BTCUSD")
	if err != nil || len(trades) != 1 || trades[0].TradeID != "t1" {
		t.Fatalf("GetTrades() = %#v, %v", trades, err)
	}
	klines, err := client.GetKlines(ctx, "BTCUSD", 60, 2)
	if err != nil || len(klines) != 2 || klines[0].CloseEp != 11 || klines[1].CloseEp != 12 {
		t.Fatalf("GetKlines() = %#v, %v", klines, err)
	}
}

func TestPhemexClientErrorAndJSONBranches(t *testing.T) {
	t.Run("http api error", func(t *testing.T) {
		client, closeServer := newMockPhemexClient(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"code":1001,"msg":"bad key","data":{}}`))
		})
		defer closeServer()
		if _, err := client.GetProduct(context.Background(), "BTCUSD"); err == nil || !strings.Contains(err.Error(), "code=1001") {
			t.Fatalf("expected API error, got %v", err)
		}
	})

	t.Run("api code error", func(t *testing.T) {
		client, closeServer := newMockPhemexClient(t, func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"code":2,"msg":"bad","data":{}}`))
		})
		defer closeServer()
		if _, err := client.GetProduct(context.Background(), "BTCUSD"); err == nil || !strings.Contains(err.Error(), "API error: bad") {
			t.Fatalf("expected product API error, got %v", err)
		}
	})

	t.Run("product not found and invalid data", func(t *testing.T) {
		emptyClient, closeEmpty := newMockPhemexClient(t, func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"code":0,"msg":"","data":{"perpProductsV2":[]}}`))
		})
		if _, err := emptyClient.GetProduct(context.Background(), "BTCUSD"); err == nil || !strings.Contains(err.Error(), "product not found") {
			t.Fatalf("expected missing product, got %v", err)
		}
		closeEmpty()

		badClient, closeBad := newMockPhemexClient(t, func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`not-json`))
		})
		defer closeBad()
		if _, err := badClient.GetProduct(context.Background(), "BTCUSD"); err == nil || !strings.Contains(err.Error(), "unmarshal error") {
			t.Fatalf("expected unmarshal error, got %v", err)
		}
	})

	t.Run("custom numeric decoders", func(t *testing.T) {
		var n asInt64
		for input, want := range map[string]int64{`"123"`: 123, `456`: 456, `""`: 0, `null`: 0} {
			if err := n.UnmarshalJSON([]byte(input)); err != nil || int64(n) != want {
				t.Fatalf("asInt64 %s = %d, %v; want %d", input, n, err, want)
			}
		}
		var k Kline
		if err := json.Unmarshal([]byte(`[1,60,10]`), &k); err == nil {
			t.Fatal("expected short row error")
		}
		if got := int64FromAny(struct{}{}); got != 0 {
			t.Fatalf("int64FromAny unknown = %d", got)
		}
	})
}
