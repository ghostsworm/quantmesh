package binance

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	binancesdk "github.com/adshao/go-binance/v2"
)

func TestMarginBalanceUsesMarginFreeNotSpotOrLockedFunds(t *testing.T) {
	spotQueries := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/sapi/v1/margin/account":
			_, _ = w.Write([]byte(`{"userAssets":[{"asset":"BTC","free":"0.25","locked":"0.2","borrowed":"0.4","interest":"0.0002","netAsset":"0.0498"}]}`))
		case "/api/v3/account":
			spotQueries++
			_, _ = w.Write([]byte(`{"balances":[{"asset":"BTC","free":"999","locked":"0"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client := binancesdk.NewClient("fixture-key", "fixture-secret")
	client.BaseURL = server.URL
	adapter := &BinanceSpotMarginAdapter{BinanceSpotAdapter: &BinanceSpotAdapter{client: client, baseAsset: "BTC"}}
	available, err := adapter.GetBalance(context.Background(), "BTC")
	if err != nil || available != 0.25 || spotQueries != 0 {
		t.Fatalf("margin funds confused with spot/locked: amount=%v spot_queries=%d err=%v", available, spotQueries, err)
	}
	principal, interest, free, err := adapter.GetMarginRepaymentFunds(context.Background(), "BTC")
	if err != nil || principal != 0.4 || interest != 0.0002 || free != 0.25 || spotQueries != 0 {
		t.Fatalf("debt/free balance snapshot not from margin: %v %v %v %v", principal, interest, free, err)
	}
	if _, err := adapter.GetBalance(context.Background(), "ETH"); err == nil {
		t.Fatal("missing margin asset normalized to zero")
	}
}

func TestMarginFreeBalanceRejectsInvalidEvidence(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		valid      bool
	}{
		{"zero", `{"userAssets":[{"asset":"BTC","free":"0","borrowed":"0","interest":"0","netAsset":"0"}]}`, true},
		{"missing", `{}`, false},
		{"empty", `{"userAssets":[]}`, false},
		{"duplicate", `{"userAssets":[{"asset":"BTC"},{"asset":"btc"}]}`, false},
		{"nan", `{"userAssets":[{"asset":"BTC","free":"NaN","borrowed":"0","interest":"0","netAsset":"0"}]}`, false},
		{"negative", `{"userAssets":[{"asset":"BTC","free":"-1","borrowed":"0","interest":"0","netAsset":"0"}]}`, false},
		{"infinite", `{"userAssets":[{"asset":"BTC","free":"Inf","borrowed":"0","interest":"0","netAsset":"0"}]}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/sapi/v1/margin/account" {
					t.Error("queried non-margin account")
					http.NotFound(w, r)
					return
				}
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			client := binancesdk.NewClient("fixture-key", "fixture-secret")
			client.BaseURL = server.URL
			adapter := &BinanceSpotMarginAdapter{BinanceSpotAdapter: &BinanceSpotAdapter{client: client, baseAsset: "BTC"}}
			value, err := adapter.GetBalance(context.Background(), "BTC")
			if tc.valid {
				if err != nil || value != 0 {
					t.Fatalf("authoritative zero rejected: %v", err)
				}
			} else if err == nil {
				t.Fatal("invalid funds became usable balance")
			}
		})
	}
}
