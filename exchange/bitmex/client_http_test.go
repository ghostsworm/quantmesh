package bitmex

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newMockBitMEXClient(t *testing.T, handler http.HandlerFunc) (*BitMEXClient, func()) {
	t.Helper()
	server := httptest.NewServer(handler)
	client := NewBitMEXClient("api-key", "secret-key", false)
	client.baseURL = server.URL
	client.httpClient = server.Client()
	return client, server.Close
}

func TestBitMEXClientHTTPMethodsWithMockServer(t *testing.T) {
	client, closeServer := newMockBitMEXClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/instrument":
			if r.URL.Query().Get("symbol") != "XBTUSD" {
				t.Fatalf("instrument query = %s", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(`[{"symbol":"XBTUSD","state":"Open","tickSize":0.5,"lastPrice":65000}]`))
		case "/api/v1/order":
			if r.Method == http.MethodPost || r.Method == http.MethodDelete || r.URL.Query().Get("filter") != "" {
				if r.Header.Get("api-key") != "api-key" || r.Header.Get("api-expires") == "" || r.Header.Get("api-signature") == "" {
					t.Fatalf("signed headers missing")
				}
			}
			switch r.Method {
			case http.MethodPost:
				var req OrderRequest
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					t.Fatalf("decode order: %v", err)
				}
				if req.Symbol != "XBTUSD" || req.Side != "Buy" {
					t.Fatalf("unexpected order request: %#v", req)
				}
				_, _ = w.Write([]byte(`{"orderID":"order-1","symbol":"XBTUSD","side":"Buy","orderQty":100,"ordStatus":"New"}`))
			case http.MethodDelete:
				_, _ = w.Write([]byte(`{"orderID":"order-1","ordStatus":"Canceled"}`))
			default:
				filter := r.URL.Query().Get("filter")
				if strings.Contains(filter, `"orderID":"empty"`) {
					_, _ = w.Write([]byte(`[]`))
					return
				}
				_, _ = w.Write([]byte(`[{"orderID":"order-1","symbol":"XBTUSD","ordStatus":"New"}]`))
			}
		case "/api/v1/position":
			if r.Header.Get("api-key") != "api-key" {
				t.Fatalf("position signed header missing")
			}
			if strings.Contains(r.URL.Query().Get("filter"), "EMPTY") {
				_, _ = w.Write([]byte(`[]`))
				return
			}
			_, _ = w.Write([]byte(`[{"symbol":"XBTUSD","currentQty":100,"markPrice":65000,"unrealisedPnl":12}]`))
		case "/api/v1/user/margin":
			if r.Header.Get("api-key") != "api-key" {
				t.Fatalf("margin signed header missing")
			}
			_, _ = w.Write([]byte(`{"account":1,"currency":"XBt","walletBalance":1000,"availableMargin":800}`))
		case "/api/v1/trade":
			if r.URL.Query().Get("count") != "2" || r.URL.Query().Get("reverse") != "true" {
				t.Fatalf("trade query = %s", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(`[{"symbol":"XBTUSD","side":"Buy","size":10,"price":65000}]`))
		case "/api/v1/trade/bucketed":
			if r.URL.Query().Get("binSize") != "1m" || r.URL.Query().Get("count") != "2" {
				t.Fatalf("bucket query = %s", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(`[{"symbol":"XBTUSD","open":1,"high":2,"low":0.5,"close":1.5,"volume":100}]`))
		default:
			http.NotFound(w, r)
		}
	})
	defer closeServer()

	ctx := context.Background()
	instrument, err := client.GetInstrument(ctx, "XBTUSD")
	if err != nil || instrument.Symbol != "XBTUSD" || instrument.TickSize != 0.5 {
		t.Fatalf("GetInstrument() = %#v, %v", instrument, err)
	}
	order, err := client.PlaceOrder(ctx, &OrderRequest{Symbol: "XBTUSD", Side: "Buy", OrderQty: 100, Price: 65000, OrdType: "Limit"})
	if err != nil || order.OrderID != "order-1" {
		t.Fatalf("PlaceOrder() = %#v, %v", order, err)
	}
	if err := client.CancelOrder(ctx, "order-1"); err != nil {
		t.Fatalf("CancelOrder() error = %v", err)
	}
	gotOrder, err := client.GetOrder(ctx, "order-1")
	if err != nil || gotOrder.OrderID != "order-1" {
		t.Fatalf("GetOrder() = %#v, %v", gotOrder, err)
	}
	if _, err := client.GetOrder(ctx, "empty"); err == nil {
		t.Fatal("expected missing order error")
	}
	openOrders, err := client.GetOpenOrders(ctx, "XBTUSD")
	if err != nil || len(openOrders) != 1 {
		t.Fatalf("GetOpenOrders() = %#v, %v", openOrders, err)
	}
	position, err := client.GetPosition(ctx, "XBTUSD")
	if err != nil || position.CurrentQty != 100 {
		t.Fatalf("GetPosition() = %#v, %v", position, err)
	}
	emptyPosition, err := client.GetPosition(ctx, "EMPTY")
	if err != nil || emptyPosition.Symbol != "EMPTY" || emptyPosition.CurrentQty != 0 {
		t.Fatalf("empty GetPosition() = %#v, %v", emptyPosition, err)
	}
	margin, err := client.GetMargin(ctx)
	if err != nil || margin.AvailableMargin != 800 {
		t.Fatalf("GetMargin() = %#v, %v", margin, err)
	}
	trades, err := client.GetTrade(ctx, "XBTUSD", 2)
	if err != nil || len(trades) != 1 || trades[0].Price != 65000 {
		t.Fatalf("GetTrade() = %#v, %v", trades, err)
	}
	buckets, err := client.GetTradeBucketed(ctx, "XBTUSD", "1m", 2)
	if err != nil || len(buckets) != 1 || buckets[0].Close != 1.5 {
		t.Fatalf("GetTradeBucketed() = %#v, %v", buckets, err)
	}
}

func TestBitMEXClientErrorBranches(t *testing.T) {
	t.Run("api error json", func(t *testing.T) {
		client, closeServer := newMockBitMEXClient(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":{"message":"invalid key","name":"HTTPError"}}`))
		})
		defer closeServer()
		if _, err := client.GetMargin(context.Background()); err == nil || !strings.Contains(err.Error(), "invalid key") {
			t.Fatalf("expected API error, got %v", err)
		}
	})

	t.Run("http error text fallback", func(t *testing.T) {
		client, closeServer := newMockBitMEXClient(t, func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "bad gateway", http.StatusBadGateway)
		})
		defer closeServer()
		if _, err := client.GetTrade(context.Background(), "XBTUSD", 1); err == nil || !strings.Contains(err.Error(), "bad gateway") {
			t.Fatalf("expected HTTP fallback error, got %v", err)
		}
	})

	t.Run("instrument not found and invalid json", func(t *testing.T) {
		emptyClient, closeEmpty := newMockBitMEXClient(t, func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`[]`))
		})
		if _, err := emptyClient.GetInstrument(context.Background(), "XBTUSD"); err == nil || !strings.Contains(err.Error(), "instrument not found") {
			t.Fatalf("expected missing instrument error, got %v", err)
		}
		closeEmpty()

		badClient, closeBad := newMockBitMEXClient(t, func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`not-json`))
		})
		defer closeBad()
		if _, err := badClient.GetInstrument(context.Background(), "XBTUSD"); err == nil || !strings.Contains(err.Error(), "unmarshal error") {
			t.Fatalf("expected unmarshal error, got %v", err)
		}
	})
}
