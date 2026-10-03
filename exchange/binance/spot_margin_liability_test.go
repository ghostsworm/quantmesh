package binance

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	binancesdk "github.com/adshao/go-binance/v2"
)

func TestMarginLiabilityReadsDebtWithBoughtBackInventory(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		valid      bool
	}{
		{"bought_back", `{"userAssets":[{"asset":"BTC","free":"0.4008","locked":"0","borrowed":"0.4","interest":"0.0002","netAsset":"0.0006"}]}`, true},
		{"missing_list", `{}`, false},
		{"missing_asset", `{"userAssets":[]}`, false},
		{"duplicate", `{"userAssets":[{"asset":"BTC"},{"asset":"btc"}]}`, false},
		{"invalid_principal", `{"userAssets":[{"asset":"BTC","free":"0.4","borrowed":"NaN","interest":"0.0002","netAsset":"0"}]}`, false},
		{"invalid_interest", `{"userAssets":[{"asset":"BTC","free":"0.4","borrowed":"0.4","interest":"-0.1","netAsset":"0"}]}`, false},
		{"overflow", `{"userAssets":[{"asset":"BTC","free":"0","borrowed":"1e308","interest":"1e308","netAsset":"0"}]}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requests := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/sapi/v1/margin/account" {
					t.Errorf("unexpected endpoint: %s", r.URL.Path)
					http.NotFound(w, r)
					return
				}
				requests++
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			client := binancesdk.NewClient("fixture-key", "fixture-secret")
			client.BaseURL = server.URL
			adapter := &BinanceSpotMarginAdapter{BinanceSpotAdapter: &BinanceSpotAdapter{client: client, baseAsset: "BTC"}}
			principal, interest, err := adapter.GetMarginLiability(context.Background(), "BTC")
			if tc.valid {
				if err != nil || principal != 0.4 || interest != 0.0002 {
					t.Fatalf("inventory hid liability: %v %v %v", principal, interest, err)
				}
				if _, err := adapter.GetPositions(context.Background(), "BTCUSDT"); err == nil {
					t.Fatal("independent liability read weakened inventory ownership gate")
				}
			} else if err == nil {
				t.Fatal("unverified liability accepted")
			}
			before := requests
			if _, _, err := adapter.GetMarginLiability(context.Background(), "ETH"); err == nil || requests != before {
				t.Fatal("foreign asset sent account query")
			}
		})
	}
}
