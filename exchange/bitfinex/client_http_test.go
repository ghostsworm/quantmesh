package bitfinex

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type bitfinexRoundTripFunc func(*http.Request) (*http.Response, error)

func (f bitfinexRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func newMockBitfinexClient(handler func(*http.Request) (int, string)) *BitfinexClient {
	client := NewBitfinexClient("api-key", "secret-key")
	client.httpClient = &http.Client{
		Transport: bitfinexRoundTripFunc(func(req *http.Request) (*http.Response, error) {
			status, body := handler(req)
			return &http.Response{
				StatusCode: status,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(body)),
				Request:    req,
			}, nil
		}),
	}
	return client
}

func TestBitfinexPublicMethodsWithMockTransport(t *testing.T) {
	client := newMockBitfinexClient(func(req *http.Request) (int, string) {
		switch req.URL.Path {
		case "/v2/conf/pub:list:pair:exchange":
			return http.StatusOK, `[0,["BTCUSD","ETHUSD"]]`
		case "/v2/ticker/tBTCUSD":
			return http.StatusOK, `[65000,1,65010,2,100,0.01,65005,123,66000,64000]`
		case "/v2/book/tBTCUSD/P0":
			return http.StatusOK, `[[65000,2,0.5],[65010,1,-0.4],[64990,1,0.2],[65020,1,-0.1]]`
		case "/v2/candles/trade:1m:tBTCUSD/hist":
			if req.URL.Query().Get("limit") != "2" {
				t.Fatalf("limit query = %s", req.URL.RawQuery)
			}
			return http.StatusOK, `[[1,10,11,12,9,100],[2,11,12,13,10,120]]`
		default:
			return http.StatusNotFound, `missing`
		}
	})

	ctx := context.Background()
	pairs, err := client.GetTradingPairs(ctx)
	if err != nil || len(pairs) != 2 || pairs[0] != "BTCUSD" {
		t.Fatalf("GetTradingPairs() = %#v, %v", pairs, err)
	}
	ticker, err := client.GetTicker(ctx, "BTCUSD")
	if err != nil || ticker.LastPrice != 65005 || ticker.High != 66000 {
		t.Fatalf("GetTicker() = %#v, %v", ticker, err)
	}
	bids, asks, err := client.GetOrderBook(ctx, "BTCUSD", 1)
	if err != nil || len(bids) != 1 || len(asks) != 1 || bids[0].Quantity != 0.5 || asks[0].Quantity != 0.4 {
		t.Fatalf("GetOrderBook() bids=%#v asks=%#v err=%v", bids, asks, err)
	}
	candles, err := client.GetCandles(ctx, "BTCUSD", "1m", 2)
	if err != nil || len(candles) != 2 || candles[1].Close != 12 {
		t.Fatalf("GetCandles() = %#v, %v", candles, err)
	}
}

func TestBitfinexAuthMethodsWithMockTransport(t *testing.T) {
	client := newMockBitfinexClient(func(req *http.Request) (int, string) {
		if req.Header.Get("bfx-apikey") != "api-key" || req.Header.Get("bfx-nonce") == "" || req.Header.Get("bfx-signature") == "" {
			t.Fatalf("auth headers missing")
		}
		bodyBytes, err := io.ReadAll(req.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		body := string(bodyBytes)
		switch req.URL.Path {
		case "/v2/auth/w/order/submit":
			if !strings.Contains(body, `"symbol":"tBTCUSD"`) || !strings.Contains(body, `"amount":"-0.25000000"`) {
				t.Fatalf("unexpected order body: %s", body)
			}
			return http.StatusOK, `[1,"on-req",null,null,[[12345]],null,"SUCCESS","Order placed"]`
		case "/v2/auth/w/order/cancel":
			if !strings.Contains(body, `"id":12345`) {
				t.Fatalf("unexpected cancel body: %s", body)
			}
			return http.StatusOK, `[1,"oc-req",null,null,[],null,"SUCCESS","Order canceled"]`
		case "/v2/auth/r/orders/tBTCUSD":
			return http.StatusOK, `[[1,2,3,"tBTCUSD",4,5,"0.1","0.2","LIMIT","LIMIT"]]`
		case "/v2/auth/r/wallets":
			return http.StatusOK, `[["exchange","USD","100.5","0","99.5"]]`
		case "/v2/auth/r/positions":
			return http.StatusOK, `[["tBTCUSD","ACTIVE","0.1","65000",0,0,"12.5","0.02"]]`
		default:
			return http.StatusNotFound, `missing`
		}
	})

	ctx := context.Background()
	order, err := client.PlaceOrder(ctx, &OrderRequest{Symbol: "BTCUSD", Side: "SELL", Type: "LIMIT", Price: 65000, Quantity: 0.25, ClientOrderID: "cid-1"})
	if err != nil || order.OrderID != "12345" {
		t.Fatalf("PlaceOrder() = %#v, %v", order, err)
	}
	if err := client.CancelOrder(ctx, "12345"); err != nil {
		t.Fatalf("CancelOrder() error = %v", err)
	}
	if err := client.CancelOrder(ctx, "bad-id"); err == nil {
		t.Fatal("expected invalid order id error")
	}
	orders, err := client.GetActiveOrders(ctx, "BTCUSD")
	if err != nil || len(orders) != 1 || orders[0].Symbol != "BTCUSD" || orders[0].Amount != 0.1 {
		t.Fatalf("GetActiveOrders() = %#v, %v", orders, err)
	}
	wallets, err := client.GetWallets(ctx)
	if err != nil || len(wallets) != 1 || wallets[0].BalanceAvailable != 99.5 {
		t.Fatalf("GetWallets() = %#v, %v", wallets, err)
	}
	positions, err := client.GetPositions(ctx)
	if err != nil || len(positions) != 1 || positions[0].PL != 12.5 {
		t.Fatalf("GetPositions() = %#v, %v", positions, err)
	}
}

func TestBitfinexErrorBranchesAndParsers(t *testing.T) {
	t.Run("public http error", func(t *testing.T) {
		client := newMockBitfinexClient(func(req *http.Request) (int, string) {
			return http.StatusBadGateway, `bad gateway`
		})
		if _, err := client.GetTicker(context.Background(), "BTCUSD"); err == nil || !strings.Contains(err.Error(), "HTTP 502") {
			t.Fatalf("expected HTTP error, got %v", err)
		}
	})

	t.Run("auth api error array", func(t *testing.T) {
		client := newMockBitfinexClient(func(req *http.Request) (int, string) {
			return http.StatusOK, `["error",10001,"bad request"]`
		})
		if _, err := client.GetWallets(context.Background()); err == nil || !strings.Contains(err.Error(), "API error") {
			t.Fatalf("expected API error, got %v", err)
		}
	})

	t.Run("invalid response shapes", func(t *testing.T) {
		client := newMockBitfinexClient(func(req *http.Request) (int, string) {
			switch req.URL.Path {
			case "/v2/conf/pub:list:pair:exchange":
				return http.StatusOK, `[0]`
			case "/v2/ticker/tBTCUSD":
				return http.StatusOK, `[1,2]`
			case "/v2/auth/w/order/submit":
				return http.StatusOK, `[1,2,3]`
			default:
				return http.StatusOK, `[]`
			}
		})
		if _, err := client.GetTradingPairs(context.Background()); err == nil {
			t.Fatal("expected invalid trading pairs error")
		}
		if _, err := client.GetTicker(context.Background(), "BTCUSD"); err == nil {
			t.Fatal("expected invalid ticker error")
		}
		if _, err := client.PlaceOrder(context.Background(), &OrderRequest{Symbol: "BTCUSD", Side: "BUY", Type: "MARKET", Quantity: 1}); err == nil {
			t.Fatal("expected invalid order response error")
		}
	})

	if got := parseFloat64("12.5"); got != 12.5 {
		t.Fatalf("parseFloat64(string) = %v", got)
	}
	if got := parseFloat64(7); got != 7 {
		t.Fatalf("parseFloat64(int) = %v", got)
	}
	if got := parseFloat64(int64(8)); got != 8 {
		t.Fatalf("parseFloat64(int64) = %v", got)
	}
	if got := parseFloat64(struct{}{}); got != 0 {
		t.Fatalf("parseFloat64(unknown) = %v", got)
	}
	if order := parseOrderArray([]interface{}{1}); order.ID != "" {
		t.Fatalf("short parseOrderArray = %#v", order)
	}
}
