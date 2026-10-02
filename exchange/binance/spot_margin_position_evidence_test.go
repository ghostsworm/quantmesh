package binance

import (
	"context"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"

	binancesdk "github.com/adshao/go-binance/v2"
)

func TestMarginPositionRejectsIncompleteOrContradictoryAccount(t *testing.T) {
	cases := []struct{ name, response string }{
		{"missing assets", `{}`},
		{"null assets despite zero summary", `{"totalAssetOfBtc":"0","totalLiabilityOfBtc":"0","totalNetAssetOfBtc":"0","userAssets":null}`},
		{"empty assets without summary", `{"userAssets":[]}`},
		{"empty assets with debt", `{"totalAssetOfBtc":"0","totalLiabilityOfBtc":"1","totalNetAssetOfBtc":"-1","userAssets":[]}`},
		{"missing target asset", `{"userAssets":[{"asset":"USDT","free":"10","locked":"0","borrowed":"0","interest":"0","netAsset":"10"}]}`},
		{"duplicate target asset", `{"userAssets":[{"asset":"BTC","free":"0","locked":"0","borrowed":"0","interest":"0","netAsset":"0"},{"asset":"btc","free":"0","locked":"0","borrowed":"1","interest":"0","netAsset":"-1"}]}`},
		{"debt hidden by zero net", `{"userAssets":[{"asset":"BTC","free":"0","locked":"0","borrowed":"1","interest":"0.01","netAsset":"0"}]}`},
		{"net debt without liability", `{"userAssets":[{"asset":"BTC","free":"0","locked":"0","borrowed":"0","interest":"0","netAsset":"-1"}]}`},
		{"overflowing liabilities", `{"userAssets":[{"asset":"BTC","free":"0","locked":"0","borrowed":"1e308","interest":"1e308","netAsset":"-1e308"}]}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/sapi/v1/margin/account" {
					http.NotFound(w, r)
					return
				}
				_, _ = w.Write([]byte(tc.response))
			}))
			defer server.Close()
			client := binancesdk.NewClient("fixture-key", "fixture-secret")
			client.BaseURL = server.URL
			adapter := &BinanceSpotMarginAdapter{BinanceSpotAdapter: &BinanceSpotAdapter{client: client, baseAsset: "BTC"}}
			positions, err := adapter.GetPositions(context.Background(), "BTCUSDT")
			if err == nil || positions != nil {
				t.Fatalf("unverified account became usable position evidence: positions=%v err=%v", positions, err)
			}
		})
	}
}

func TestMarginPositionVerifiedDebtRetainsSmallLiabilities(t *testing.T) {
	cases := []struct {
		name, borrowed, interest, net string
		want                          float64
	}{
		{"explicit flat asset", "0", "0", "0", 0},
		{"principal and interest", "1", "0.01", "-1.01", 1.01},
		{"small debt with rounded net", "0.0000000000001", "0", "0", 1e-13},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			account := &binancesdk.MarginAccount{UserAssets: []binancesdk.UserAsset{{
				Asset: "BTC", Free: "0", Locked: "0", Borrowed: tc.borrowed, Interest: tc.interest, NetAsset: tc.net,
			}}}
			principal, interest, err := verifiedMarginShortDebt(account, "BTC")
			if err != nil || math.Abs(principal+interest-tc.want) > tc.want*1e-12 {
				t.Fatalf("verified liability=%v err=%v want=%v", principal+interest, err, tc.want)
			}
		})
	}
}

func TestMarginPositionRoundedNetDoesNotEraseSmallDebt(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/sapi/v1/margin/account":
			_, _ = w.Write([]byte(`{"userAssets":[{"asset":"BTC","free":"0","locked":"0","borrowed":"0.0000000000001","interest":"0","netAsset":"0"}]}`))
		case "/api/v3/ticker/price":
			_, _ = w.Write([]byte(`{"symbol":"BTCUSDT","price":"100"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client := binancesdk.NewClient("fixture-key", "fixture-secret")
	client.BaseURL = server.URL
	adapter := &BinanceSpotMarginAdapter{BinanceSpotAdapter: &BinanceSpotAdapter{client: client, baseAsset: "BTC"}}
	positions, err := adapter.GetPositions(context.Background(), "BTCUSDT")
	if err != nil || len(positions) != 1 || positions[0].Size != -1e-13 || !positions[0].MarginDebtKnown {
		t.Fatalf("rounded net asset erased confirmed liability: positions=%v err=%v", positions, err)
	}
}
