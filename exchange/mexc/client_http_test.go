package mexc

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newMockMEXCClient(t *testing.T, handler http.HandlerFunc) (*MEXCClient, func()) {
	t.Helper()
	server := httptest.NewServer(handler)
	client := NewMEXCClient("api-key", "secret-key", false)
	client.baseURL = server.URL
	client.httpClient = server.Client()
	return client, server.Close
}

func TestMEXCClientHTTPMethodsWithMockServer(t *testing.T) {
	client, closeServer := newMockMEXCClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/contract/detail":
			_, _ = w.Write([]byte(`{"code":0,"success":true,"data":[{"symbol":"BTC_USDT","displayName":"BTC_USDT","baseCoin":"BTC","quoteCoin":"USDT","state":1}]}`))
		case "/api/v1/private/order/create":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode submit body: %v", err)
			}
			if body["symbol"] != "BTC_USDT" || body["externalOid"] != "cid-1" || body["side"] != float64(1) ||
				body["price"] != float64(65000) || r.Header.Get("Content-Type") != "application/json" {
				t.Fatalf("unexpected submit body: %#v", body)
			}
			assertMEXCOpenAPIAuth(t, r, `{"externalOid":"cid-1","leverage":5,"openType":2,"price":65000.00000000,"side":1,"symbol":"BTC_USDT","type":1,"vol":2}`)
			_, _ = w.Write([]byte(`{"code":0,"success":true,"data":{"orderId":"order-1","ts":1760000000000}}`))
		case "/api/v1/private/order/cancel":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode cancel body: %v", err)
			}
			if body["orderId"] != "order-1" {
				t.Fatalf("unexpected cancel body: %#v", body)
			}
			assertMEXCOpenAPIAuth(t, r, `{"orderId":"order-1","symbol":"BTC_USDT"}`)
			_, _ = w.Write([]byte(`{"code":0,"success":true}`))
		case "/api/v1/private/order/get":
			_, _ = w.Write([]byte(`{"code":0,"success":true,"data":{"orderId":"order-1","symbol":"BTC_USDT","price":65000,"vol":2}}`))
		case "/api/v1/private/order/list/open_orders/BTC_USDT":
			if r.URL.Query().Get("page_size") != "100" {
				t.Fatalf("open orders query = %s", r.URL.RawQuery)
			}
			assertMEXCOpenAPIAuth(t, r, r.URL.Query().Encode())
			_, _ = w.Write([]byte(`{"code":0,"success":true,"data":[{"orderId":"order-2","symbol":"BTC_USDT"}]}`))
		case "/api/v1/private/account/assets":
			_, _ = w.Write([]byte(`{"code":0,"success":true,"data":[{"currency":"BTC","availableBalance":2,"equity":2.5},{"currency":"USDT","availableBalance":100,"equity":120}]}`))
		case "/api/v1/private/position/open_positions":
			_, _ = w.Write([]byte(`{"code":0,"success":true,"data":[{"positionId":1,"symbol":"BTC_USDT","holdVol":2,"unrealizedPNL":3}]}`))
		case "/api/v1/contract/ticker":
			if r.Header.Get("ApiKey") != "" || r.Header.Get("X-MEXC-APIKEY") != "" {
				t.Fatal("public ticker request must not send API credentials")
			}
			if r.URL.Query().Get("symbol") != "BTC_USDT" {
				t.Fatalf("ticker query = %s", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(`{"code":0,"success":true,"data":{"symbol":"BTC_USDT","lastPrice":65000,"bid1":64990,"ask1":65010}}`))
		case "/api/v1/contract/depth/BTC_USDT":
			_, _ = w.Write([]byte(`{"code":0,"success":true,"data":{"bids":[[1,2]],"asks":[[3,4]],"timestamp":9}}`))
		case "/api/v1/contract/kline/BTC_USDT":
			if r.URL.Query().Get("interval") != "Min1" || r.URL.Query().Get("limit") != "2" {
				t.Fatalf("kline query = %s", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(`{"code":0,"success":true,"data":{"time":[1,2],"open":[10,11],"high":[12,13],"low":[9,10],"close":[11,12],"vol":[100,120]}}`))
		default:
			http.NotFound(w, r)
		}
	})
	defer closeServer()

	ctx := context.Background()
	info, err := client.GetExchangeInfo(ctx)
	if err != nil || info.Symbols["BTC_USDT"].BaseCoin != "BTC" {
		t.Fatalf("GetExchangeInfo() = %#v, %v", info, err)
	}
	order, err := client.PlaceOrder(ctx, &OrderRequest{Symbol: "BTC_USDT", Price: 65000, Volume: 2, Side: 1, Type: 1, OpenType: 2, Leverage: 5, ClientOrderID: "cid-1"})
	if err != nil || order.OrderID != "order-1" {
		t.Fatalf("PlaceOrder() = %#v, %v", order, err)
	}
	if err := client.CancelOrder(ctx, "BTC_USDT", "order-1"); err != nil {
		t.Fatalf("CancelOrder() error = %v", err)
	}
	gotOrder, err := client.GetOrderInfo(ctx, "BTC_USDT", "order-1")
	if err != nil || gotOrder.OrderID != "order-1" || gotOrder.Price != 65000 {
		t.Fatalf("GetOrderInfo() = %#v, %v", gotOrder, err)
	}
	openOrders, err := client.GetOpenOrders(ctx, "BTC_USDT")
	if err != nil || len(openOrders) != 1 || openOrders[0].OrderID != "order-2" {
		t.Fatalf("GetOpenOrders() = %#v, %v", openOrders, err)
	}
	account, err := client.GetAccount(ctx)
	if err != nil || len(account) != 2 || account[1].AvailableBalance != 100 {
		t.Fatalf("GetAccount() = %#v, %v", account, err)
	}
	positions, err := client.GetPositions(ctx, "BTC_USDT")
	if err != nil || len(positions) != 1 || positions[0].PositionID != 1 {
		t.Fatalf("GetPositions() = %#v, %v", positions, err)
	}
	ticker, err := client.GetTicker(ctx, "BTC_USDT")
	if err != nil || ticker.LastPrice != 65000 {
		t.Fatalf("GetTicker() = %#v, %v", ticker, err)
	}
	bids, asks, ts, err := client.GetContractDepth(ctx, "BTC_USDT")
	if err != nil || ts != 9 || len(bids) != 1 || len(asks) != 1 {
		t.Fatalf("GetContractDepth() bids=%#v asks=%#v ts=%d err=%v", bids, asks, ts, err)
	}
	klines, err := client.GetKlines(ctx, "BTC_USDT", "Min1", 2)
	if err != nil || len(klines) != 2 || klines[1].Time != 2000 || klines[1].Close != 12 {
		t.Fatalf("GetKlines() = %#v, %v", klines, err)
	}
}

func assertMEXCOpenAPIAuth(t *testing.T, r *http.Request, parameterString string) {
	t.Helper()
	requestTime := r.Header.Get("Request-Time")
	if r.Header.Get("ApiKey") != "api-key" || requestTime == "" || r.Header.Get("Signature") == "" {
		t.Fatalf("Open-API auth headers missing: %v", r.Header)
	}
	client := NewMEXCClient("api-key", "secret-key", false)
	if got, want := r.Header.Get("Signature"), client.signOpenAPIRequest(requestTime, parameterString); got != want {
		t.Fatalf("invalid Open-API signature: got %s want %s", got, want)
	}
	if r.URL.Query().Has("timestamp") || r.URL.Query().Has("signature") {
		t.Fatalf("legacy signature query parameters must not be sent: %s", r.URL.RawQuery)
	}
}

func TestMEXCClientErrorBranches(t *testing.T) {
	t.Run("http error", func(t *testing.T) {
		client, closeServer := newMockMEXCClient(t, func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "bad gateway", http.StatusBadGateway)
		})
		defer closeServer()
		if _, err := client.GetTicker(context.Background(), "BTC_USDT"); err == nil || !strings.Contains(err.Error(), "HTTP 502") {
			t.Fatalf("expected HTTP error, got %v", err)
		}
	})

	t.Run("api error code", func(t *testing.T) {
		client, closeServer := newMockMEXCClient(t, func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"code":500,"msg":"boom","success":false}`))
		})
		defer closeServer()
		if _, err := client.GetTicker(context.Background(), "BTC_USDT"); err == nil || !strings.Contains(err.Error(), "API error 500") {
			t.Fatalf("expected API error, got %v", err)
		}
	})

	t.Run("success false", func(t *testing.T) {
		client, closeServer := newMockMEXCClient(t, func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"code":0,"success":false,"data":[]}`))
		})
		defer closeServer()
		if _, err := client.GetExchangeInfo(context.Background()); err == nil || !strings.Contains(err.Error(), "get exchange info failed") {
			t.Fatalf("expected success false error, got %v", err)
		}
	})

	t.Run("depth success false and kline truncation", func(t *testing.T) {
		depthClient, closeDepth := newMockMEXCClient(t, func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"code":0,"success":false,"data":{}}`))
		})
		if _, _, _, err := depthClient.GetContractDepth(context.Background(), "BTC_USDT"); err == nil {
			t.Fatal("expected depth failure")
		}
		closeDepth()

		klineClient, closeKline := newMockMEXCClient(t, func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"code":0,"success":true,"data":{"time":[1,2],"open":[10],"high":[12],"low":[9],"close":[11],"vol":[100]}}`))
		})
		defer closeKline()
		klines, err := klineClient.GetKlines(context.Background(), "BTC_USDT", "Min1", 2)
		if err != nil || len(klines) != 1 {
			t.Fatalf("expected truncated single kline, got %#v, %v", klines, err)
		}
	})
}
