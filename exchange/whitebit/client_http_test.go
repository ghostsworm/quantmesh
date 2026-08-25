package whitebit

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newMockWhiteBITClient(t *testing.T, handler http.HandlerFunc) (*WhiteBITClient, func()) {
	t.Helper()
	server := httptest.NewServer(handler)
	client := NewWhiteBITClient("api-key", "secret-key", false)
	client.baseURL = server.URL
	client.httpClient = server.Client()
	return client, server.Close
}

func TestWhiteBITClientBasicsAndRequestErrors(t *testing.T) {
	client := NewWhiteBITClient("key", "secret", true)
	if client.apiKey != "key" || client.secretKey != "secret" || !client.useTestnet {
		t.Fatalf("client fields were not initialized correctly")
	}
	if n1, n2 := client.getNonce(), client.getNonce(); n2 <= n1 {
		t.Fatalf("nonce should increase: %d -> %d", n1, n2)
	}
	if got := client.sign("payload"); got == "" || got != client.sign("payload") {
		t.Fatalf("signature should be deterministic and non-empty")
	}

	mock, closeServer := newMockWhiteBITClient(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusTeapot)
	})
	defer closeServer()
	if _, err := mock.request(context.Background(), "GET", "/bad", nil); err == nil || !strings.Contains(err.Error(), "HTTP 錯误 418") {
		t.Fatalf("expected GET HTTP error, got %v", err)
	}
	if _, err := mock.request(context.Background(), "POST", "/bad", nil); err == nil || !strings.Contains(err.Error(), "HTTP 錯误 418") {
		t.Fatalf("expected POST HTTP error, got %v", err)
	}
}

func TestWhiteBITPublicMethodsWithMockServer(t *testing.T) {
	client, closeServer := newMockWhiteBITClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v4/public/markets":
			_, _ = w.Write([]byte(`[{"name":"BTC_USDT","stock":"BTC","money":"USDT","type":"spot"}]`))
		case "/api/v4/public/futures":
			_, _ = w.Write([]byte(`{"success":true,"result":[{"ticker_id":"BTC_PERP","funding_rate":"0.0001"}]}`))
		case "/api/v4/public/funding-history/BTC_PERP":
			_, _ = w.Write([]byte(`[{"fundingTime":"1","fundingRate":"0.0002","market":"BTC_PERP"}]`))
		case "/api/v4/public/ticker":
			_, _ = w.Write([]byte(`{"BTC_USDT":{"last_price":"65000","quote_volume":"10"}}`))
		case "/api/v4/public/orderbook/BTC_USDT":
			if r.URL.Query().Get("limit") != "5" {
				t.Fatalf("limit query not propagated: %s", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(`{"ticker_id":"BTC_USDT","timestamp":1,"asks":[["2","1"]],"bids":[["1","1"]]}`))
		default:
			http.NotFound(w, r)
		}
	})
	defer closeServer()

	ctx := context.Background()
	markets, err := client.GetMarkets(ctx)
	if err != nil || len(markets) != 1 || markets[0].Name != "BTC_USDT" {
		t.Fatalf("GetMarkets() = %#v, %v", markets, err)
	}
	futures, err := client.GetFuturesMarkets(ctx)
	if err != nil || len(futures) != 1 || futures[0].TickerID != "BTC_PERP" {
		t.Fatalf("GetFuturesMarkets() = %#v, %v", futures, err)
	}
	future, err := client.GetFuturesMarketByTicker(ctx, "BTC_PERP")
	if err != nil || future.TickerID != "BTC_PERP" {
		t.Fatalf("GetFuturesMarketByTicker() = %#v, %v", future, err)
	}
	if _, err := client.GetFuturesMarketByTicker(ctx, "ETH_PERP"); err == nil {
		t.Fatal("expected missing futures market error")
	}
	rate, err := client.GetFundingRate(ctx, "BTC_PERP")
	if err != nil || rate != 0.0002 {
		t.Fatalf("GetFundingRate() = %v, %v", rate, err)
	}
	tickers, err := client.GetTicker(ctx, "BTC_USDT")
	if err != nil || tickers["BTC_USDT"].LastPrice != "65000" {
		t.Fatalf("GetTicker() = %#v, %v", tickers, err)
	}
	book, err := client.GetOrderBook(ctx, "BTC_USDT", 5)
	if err != nil || book.TickerID != "BTC_USDT" || len(book.Asks) != 1 {
		t.Fatalf("GetOrderBook() = %#v, %v", book, err)
	}
}

func TestWhiteBITPrivateMethodsWithMockServer(t *testing.T) {
	client, closeServer := newMockWhiteBITClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Header.Get("X-TXC-APIKEY") != "api-key" || r.Header.Get("X-TXC-PAYLOAD") == "" || r.Header.Get("X-TXC-SIGNATURE") == "" {
			t.Fatalf("auth headers missing")
		}
		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body["request"] != r.URL.Path {
			t.Fatalf("request field = %v, want %s", body["request"], r.URL.Path)
		}

		switch r.URL.Path {
		case "/api/v4/trade-account/balance":
			_, _ = w.Write([]byte(`{"success":true,"result":{"USDT":{"available":"100","freeze":"2"}}}`))
		case "/api/v4/order/new":
			_, _ = w.Write([]byte(`{"success":true,"result":{"orderId":11,"market":"BTC_USDT","side":"buy","amount":"1","price":"10","status":"OPEN"}}`))
		case "/api/v4/order/bulk":
			_, _ = w.Write([]byte(`{"success":true,"result":[{"result":{"orderId":12,"market":"BTC_USDT"}}]}`))
		case "/api/v4/order/cancel":
			_, _ = w.Write([]byte(`{"success":true,"result":{"orderId":13,"status":"CANCELED"}}`))
		case "/api/v4/order/cancel/all":
			_, _ = w.Write([]byte(`{"success":true,"result":{}}`))
		case "/api/v4/orders":
			if body["orderId"] == "404" {
				_, _ = w.Write([]byte(`{"success":true,"result":[]}`))
				return
			}
			_, _ = w.Write([]byte(`{"success":true,"result":[{"orderId":14,"market":"BTC_USDT","status":"OPEN"}]}`))
		case "/api/v4/collateral-account/balance":
			_, _ = w.Write([]byte(`{"success":true,"result":{"USDT":{"balance":"50","borrow":"0"}}}`))
		case "/api/v4/collateral-account/positions":
			_, _ = w.Write([]byte(`{"success":true,"result":{"total":1,"records":[{"id":1,"market":"BTC_PERP","amount":"0.1"}]}}`))
		case "/api/v4/trade-account/order":
			_, _ = w.Write([]byte(`{"success":true,"result":{"records":[{"id":1,"dealOrderId":14,"price":"10","amount":"1"}]}}`))
		case "/api/v4/profile/websocket_token":
			_, _ = w.Write([]byte(`{"success":true,"result":{"websocket_token":"token-1"}}`))
		default:
			http.NotFound(w, r)
		}
	})
	defer closeServer()

	ctx := context.Background()
	balances, err := client.GetBalance(ctx, "USDT")
	if err != nil || balances["USDT"].Available != "100" {
		t.Fatalf("GetBalance() = %#v, %v", balances, err)
	}
	order, err := client.PlaceOrder(ctx, "BTC_USDT", "buy", "1", "10", "cid-1", true, false)
	if err != nil || order.OrderID != 11 {
		t.Fatalf("PlaceOrder() = %#v, %v", order, err)
	}
	bulk, err := client.BatchPlaceOrders(ctx, []BulkOrderRequest{{Market: "BTC_USDT", Side: "buy", Amount: "1", Price: "10"}}, false)
	if err != nil || len(bulk) != 1 || bulk[0].Result.OrderID != 12 {
		t.Fatalf("BatchPlaceOrders() = %#v, %v", bulk, err)
	}
	if _, err := client.BatchPlaceOrders(ctx, make([]BulkOrderRequest, 21), false); err == nil {
		t.Fatal("expected bulk order size error")
	}
	canceled, err := client.CancelOrder(ctx, "BTC_USDT", 13, "")
	if err != nil || canceled.Status != "CANCELED" {
		t.Fatalf("CancelOrder() = %#v, %v", canceled, err)
	}
	if err := client.CancelAllOrders(ctx, "BTC_USDT", []string{"limit"}); err != nil {
		t.Fatalf("CancelAllOrders() error = %v", err)
	}
	openOrder, err := client.GetOrder(ctx, "BTC_USDT", 14, "")
	if err != nil || openOrder.OrderID != 14 {
		t.Fatalf("GetOrder() = %#v, %v", openOrder, err)
	}
	if _, err := client.GetOrder(ctx, "BTC_USDT", 404, ""); err == nil {
		t.Fatal("expected empty order error")
	}
	openOrders, err := client.GetOpenOrders(ctx, "BTC_USDT", 50, 0)
	if err != nil || len(openOrders) != 1 {
		t.Fatalf("GetOpenOrders() = %#v, %v", openOrders, err)
	}
	collateral, err := client.GetCollateralBalance(ctx, "USDT")
	if err != nil || collateral["USDT"].Balance != "50" {
		t.Fatalf("GetCollateralBalance() = %#v, %v", collateral, err)
	}
	positions, err := client.GetPositions(ctx, "BTC_PERP")
	if err != nil || len(positions) != 1 || positions[0].ID != 1 {
		t.Fatalf("GetPositions() = %#v, %v", positions, err)
	}
	deals, err := client.GetOrderDeals(ctx, 14, 0, 0)
	if err != nil || len(deals) != 1 || deals[0].ID != 1 {
		t.Fatalf("GetOrderDeals() = %#v, %v", deals, err)
	}
	if _, err := client.GetOrderDeals(ctx, 0, 0, 0); err == nil {
		t.Fatal("expected order_id required error")
	}
	token, err := client.GetWebSocketToken(ctx)
	if err != nil || token != "token-1" {
		t.Fatalf("GetWebSocketToken() = %q, %v", token, err)
	}
}

func TestWhiteBITAPIEdgeResponses(t *testing.T) {
	t.Run("futures api failure", func(t *testing.T) {
		client, closeServer := newMockWhiteBITClient(t, func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"success":false,"message":"maintenance"}`))
		})
		defer closeServer()
		if _, err := client.GetFuturesMarkets(context.Background()); err == nil || !strings.Contains(err.Error(), "maintenance") {
			t.Fatalf("expected futures failure, got %v", err)
		}
	})

	t.Run("funding history empty and invalid", func(t *testing.T) {
		responses := []string{`[]`, `[{"fundingRate":"not-number"}]`}
		for _, response := range responses {
			client, closeServer := newMockWhiteBITClient(t, func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte(response))
			})
			_, err := client.GetFundingRate(context.Background(), "BTC_PERP")
			closeServer()
			if err == nil {
				t.Fatalf("expected funding rate error for %s", response)
			}
		}
	})

	t.Run("order deals direct array", func(t *testing.T) {
		client, closeServer := newMockWhiteBITClient(t, func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`[{"id":2,"dealOrderId":15,"price":"11","amount":"2"}]`))
		})
		defer closeServer()
		deals, err := client.GetOrderDeals(context.Background(), 15, 10, 1)
		if err != nil || len(deals) != 1 || deals[0].ID != 2 {
			t.Fatalf("direct deals = %#v, %v", deals, err)
		}
	})
}
