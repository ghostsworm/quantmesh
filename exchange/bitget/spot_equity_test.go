package bitget

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBitgetSpotAccountEquityUSDTValuesEveryAssetWithDirectMarket(t *testing.T) {
	var tickerRequests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/spot/account/assets":
			_, _ = w.Write([]byte(`{"code":"00000","data":[{"coin":"USDT","available":"10","locked":"2"},{"coin":"BTC","available":"0.5","locked":"0.1"},{"coin":"ETH","available":"0","locked":"0"}]}`))
		case "/api/v2/spot/market/tickers":
			tickerRequests++
			if r.URL.Query().Has("symbol") {
				t.Errorf("expected one all-symbol ticker snapshot, got query %q", r.URL.RawQuery)
			}
			_, _ = fmt.Fprintf(w, `{"code":"00000","data":[{"symbol":"BTCUSDT","lastPr":"100"},{"symbol":"ETHUSDT","lastPr":"200"},{"symbol":"UNRELATED","lastPr":"bad"}]}`)
		default:
			t.Errorf("unexpected endpoint %s", r.URL.Path)
		}
	}))
	defer server.Close()
	client := NewClient("key", "secret", "pass", false)
	client.baseURL = server.URL
	client.httpClient = server.Client()
	adapter := &BitgetSpotAdapter{client: client, symbol: "ETHUSDT"}

	value, available := adapter.AccountEquityUSDT(context.Background())
	if !available || value != 72 {
		t.Fatalf("equity=%v available=%v, want 72 true", value, available)
	}
	if tickerRequests != 1 {
		t.Fatalf("ticker requests=%d, want a single market snapshot", tickerRequests)
	}
}

func TestBitgetSpotAccountEquityUSDTRejectsMissingRequiredMarket(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v2/spot/account/assets" {
			_, _ = w.Write([]byte(`{"code":"00000","data":[{"coin":"XYZ","available":"1","locked":"0"}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"code":"00000","data":[{"symbol":"BTCUSDT","lastPr":"100"}]}`))
	}))
	defer server.Close()
	client := NewClient("key", "secret", "pass", false)
	client.baseURL = server.URL
	client.httpClient = server.Client()
	adapter := &BitgetSpotAdapter{client: client}
	if value, available := adapter.AccountEquityUSDT(context.Background()); available || value != 0 {
		t.Fatalf("missing XYZUSDT market returned equity=%v available=%v", value, available)
	}
}

func TestValueBitgetSpotBalancesUSDTRejectsIncompleteOrInvalidAssets(t *testing.T) {
	price := func(context.Context, string) (float64, error) { return 10, nil }
	for name, balances := range map[string][]bitgetSpotAccountAsset{
		"duplicate asset":  {{Coin: "BTC", Available: "1", Locked: "0"}, {Coin: "btc", Available: "2", Locked: "0"}},
		"missing currency": {{Coin: "", Available: "1", Locked: "0"}},
		"invalid amount":   {{Coin: "BTC", Available: "NaN", Locked: "0"}},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := valueBitgetSpotBalancesUSDT(context.Background(), balances, price); err == nil {
				t.Fatal("invalid balances were accepted")
			}
		})
	}
	if _, err := valueBitgetSpotBalancesUSDT(context.Background(), []bitgetSpotAccountAsset{{Coin: "XYZ", Available: "1", Locked: "0"}}, func(context.Context, string) (float64, error) {
		return 0, fmt.Errorf("ticker missing")
	}); err == nil {
		t.Fatal("an asset without a direct USDT market must not yield a partial equity")
	}
}
