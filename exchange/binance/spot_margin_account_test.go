package binance

import (
	"context"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	binancesdk "github.com/adshao/go-binance/v2"
)

func TestSummarizeMarginAccountIncludesInterestAndOnlyConfiguredQuoteAvailability(t *testing.T) {
	account := &binancesdk.MarginAccount{TotalAssetOfBTC: "2", TotalNetAssetOfBTC: "1.5", UserAssets: []binancesdk.UserAsset{
		{Asset: "BTC", Free: "0.2", Borrowed: "1", Interest: "0.01", NetAsset: "-0.81"},
		{Asset: "USDT", Free: "50", Borrowed: "0", Interest: "0", NetAsset: "50"},
		{Asset: "USDC", Free: "700", Borrowed: "0", Interest: "0", NetAsset: "700"},
	}}
	wallet, margin, available, err := summarizeMarginAccount(account, "USDT", 100)
	if err != nil {
		t.Fatal(err)
	}
	if wallet != 200 || margin != 150 {
		t.Fatalf("gross wallet=%v net margin=%v, want BTC-valued API totals 200/150", wallet, margin)
	}
	if available != 50 {
		t.Fatalf("available quote balance=%v, want USDT free balance 50 without cross-asset addition/double count", available)
	}
}

func TestParseMarginUserAssetRejectsInvalidDebtAndInterest(t *testing.T) {
	cases := []struct {
		name  string
		asset binancesdk.UserAsset
	}{
		{name: "malformed borrowed", asset: binancesdk.UserAsset{Asset: "BTC", Free: "0", Borrowed: "bad", Interest: "0", NetAsset: "0"}},
		{name: "negative interest", asset: binancesdk.UserAsset{Asset: "BTC", Free: "0", Borrowed: "0", Interest: "-0.1", NetAsset: "0"}},
		{name: "non-finite debt", asset: binancesdk.UserAsset{Asset: "BTC", Free: "0", Borrowed: "NaN", Interest: "0", NetAsset: "0"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, _, _, err := parseMarginUserAsset(tc.asset)
			if err == nil || !strings.Contains(err.Error(), "invalid") {
				t.Fatalf("parseMarginUserAsset error=%v, want invalid balance rejection", err)
			}
		})
	}
}

func TestSummarizeMarginAccountRejectsMalformedResponse(t *testing.T) {
	if _, _, _, err := summarizeMarginAccount(nil, "USDT", 100); err == nil {
		t.Fatal("nil margin account response must fail closed")
	}
	badQuote := &binancesdk.MarginAccount{TotalAssetOfBTC: "1", TotalNetAssetOfBTC: "1", UserAssets: []binancesdk.UserAsset{{Asset: "USDT", Free: "bad", Borrowed: "0", Interest: "0", NetAsset: "0"}}}
	if _, _, _, err := summarizeMarginAccount(badQuote, "USDT", 100); err == nil {
		t.Fatal("malformed interest must fail closed")
	}
	if _, _, _, err := summarizeMarginAccount(&binancesdk.MarginAccount{TotalAssetOfBTC: "NaN", TotalNetAssetOfBTC: "1"}, "USDT", 100); err == nil {
		t.Fatal("invalid BTC valuation data must fail closed")
	}
}

func TestBinanceSpotMarginAdapterUsesInterestAwareAccountEvidence(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/sapi/v1/margin/account":
			_, _ = w.Write([]byte(`{"totalAssetOfBtc":"2","totalNetAssetOfBtc":"1.5","userAssets":[{"asset":"BTC","free":"0.2","borrowed":"1","interest":"0.01","netAsset":"-0.81"},{"asset":"USDT","free":"50","borrowed":"0","interest":"0","netAsset":"50"},{"asset":"USDC","free":"700","borrowed":"0","interest":"0","netAsset":"700"}]}`))
		case "/api/v3/ticker/price":
			_, _ = w.Write([]byte(`{"symbol":"BTCUSDT","price":"100"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client := binancesdk.NewClient("test-key", "test-secret")
	client.BaseURL = server.URL
	adapter := &BinanceSpotMarginAdapter{BinanceSpotAdapter: &BinanceSpotAdapter{
		client: client, symbol: "BTCUSDT", baseAsset: "BTC", quoteAsset: "USDT",
	}}

	account, err := adapter.GetAccount(context.Background())
	if err != nil {
		t.Fatalf("GetAccount: %v", err)
	}
	if account.TotalWalletBalance != 200 || account.TotalMarginBalance != 150 || account.AvailableBalance != 50 || account.BalanceAsset != "USDT" {
		t.Fatalf("account balances must use BTC-valued net API equity and quote-only available funds: %+v", account)
	}
	positions, err := adapter.GetPositions(context.Background(), "BTCUSDT")
	if err != nil {
		t.Fatalf("GetPositions: %v", err)
	}
	if len(positions) != 1 || math.Abs(positions[0].Size+1.01) > 1e-12 {
		t.Fatalf("margin short position=%+v, want principal plus interest -1.01", positions)
	}
}

func TestBinanceSpotMarginAdapterReturnsAuthoritativeEmptyPositionsWhenDebtIsZero(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/sapi/v1/margin/account" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"totalAssetOfBtc":"0","totalLiabilityOfBtc":"0","totalNetAssetOfBtc":"0","userAssets":[]}`))
	}))
	defer server.Close()
	client := binancesdk.NewClient("test-key", "test-secret")
	client.BaseURL = server.URL
	adapter := &BinanceSpotMarginAdapter{BinanceSpotAdapter: &BinanceSpotAdapter{
		client: client, symbol: "BTCUSDT", baseAsset: "BTC", quoteAsset: "USDT",
	}}

	positions, err := adapter.GetPositions(context.Background(), "BTCUSDT")
	if err != nil {
		t.Fatalf("GetPositions: %v", err)
	}
	if positions == nil || len(positions) != 0 {
		t.Fatalf("zero-debt account positions=%+v, want authoritative non-nil empty snapshot", positions)
	}
}

func TestBinanceSpotMarginFlatnessVerificationCoversAllDebtsAndOrders(t *testing.T) {
	for _, tc := range []struct {
		name       string
		account    string
		isolated   string
		crossOrder string
		isoOrder   string
		crossOCO   string
		isoOCO     string
		wantErr    string
	}{
		{
			name:     "flat account",
			account:  `{"totalLiabilityOfBtc":"0","userAssets":[{"asset":"BTC","free":"0","borrowed":"0","interest":"0","netAsset":"0"},{"asset":"USDT","free":"10","borrowed":"0","interest":"0","netAsset":"10"}]}`,
			isolated: `{"totalLiabilityOfBtc":"0","assets":[]}`,
		},
		{
			name:     "liability in another asset",
			account:  `{"totalLiabilityOfBtc":"0.01","userAssets":[{"asset":"BNB","free":"0","borrowed":"0.1","interest":"0.01","netAsset":"-0.11"}]}`,
			isolated: `{"totalLiabilityOfBtc":"0","assets":[]}`,
			wantErr:  "total liability is not zero",
		},
		{
			name:     "asset debt despite zero summary",
			account:  `{"totalLiabilityOfBtc":"0","userAssets":[{"asset":"BNB","free":"0","borrowed":"0.1","interest":"0","netAsset":"-0.1"}]}`,
			isolated: `{"totalLiabilityOfBtc":"0","assets":[]}`,
			wantErr:  "debt remains for BNB",
		},
		{
			name:       "cross margin order",
			account:    `{"totalLiabilityOfBtc":"0","userAssets":[]}`,
			isolated:   `{"totalLiabilityOfBtc":"0","assets":[]}`,
			crossOrder: `[{"symbol":"ETHUSDT","orderId":1,"status":"NEW"}]`,
			wantErr:    "cross margin account for  has 1 active orders",
		},
		{
			name:     "cross margin OCO list",
			account:  `{"totalLiabilityOfBtc":"0","userAssets":[]}`,
			isolated: `{"totalLiabilityOfBtc":"0","assets":[]}`,
			crossOCO: `[{"orderListId":3,"listOrderStatus":"EXECUTING","orders":[]}]`,
			wantErr:  "cross margin account for  has 1 active OCO lists",
		},
		{
			name:     "isolated margin order",
			account:  `{"totalLiabilityOfBtc":"0","userAssets":[]}`,
			isolated: `{"totalLiabilityOfBtc":"0","assets":[{"symbol":"ETHUSDT","baseAsset":{"asset":"ETH","borrowed":"0","interest":"0"},"quoteAsset":{"asset":"USDT","borrowed":"0","interest":"0"}}]}`,
			isoOrder: `[{"symbol":"ETHUSDT","orderId":2,"status":"NEW"}]`,
			wantErr:  "isolated margin account for ETHUSDT has 1 active orders",
		},
		{
			name:     "isolated margin OCO list",
			account:  `{"totalLiabilityOfBtc":"0","userAssets":[]}`,
			isolated: `{"totalLiabilityOfBtc":"0","assets":[{"symbol":"ETHUSDT","baseAsset":{"asset":"ETH","borrowed":"0","interest":"0"},"quoteAsset":{"asset":"USDT","borrowed":"0","interest":"0"}}]}`,
			isoOCO:   `[{"orderListId":4,"listOrderStatus":"EXECUTING","orders":[]}]`,
			wantErr:  "isolated margin account for ETHUSDT has 1 active OCO lists",
		},
		{
			name:     "isolated liability in another symbol",
			account:  `{"totalLiabilityOfBtc":"0","userAssets":[]}`,
			isolated: `{"totalLiabilityOfBtc":"0.02","assets":[{"symbol":"ETHUSDT","baseAsset":{"asset":"ETH","borrowed":"1","interest":"0.01"},"quoteAsset":{"asset":"USDT","borrowed":"0","interest":"0"}}]}`,
			wantErr:  "isolated-margin total liability is not zero",
		},
		{
			name:     "missing total liability",
			account:  `{"userAssets":[]}`,
			isolated: `{"totalLiabilityOfBtc":"0","assets":[]}`,
			wantErr:  "total liability is missing",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.crossOrder == "" {
				tc.crossOrder = `[]`
			}
			if tc.isoOrder == "" {
				tc.isoOrder = `[]`
			}
			if tc.crossOCO == "" {
				tc.crossOCO = `[]`
			}
			if tc.isoOCO == "" {
				tc.isoOCO = `[]`
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/sapi/v1/margin/account":
					_, _ = w.Write([]byte(tc.account))
				case "/sapi/v1/margin/isolated/account":
					_, _ = w.Write([]byte(tc.isolated))
				case "/sapi/v1/margin/openOrders":
					if r.URL.Query().Get("isIsolated") == "TRUE" {
						if r.URL.Query().Get("symbol") != "ETHUSDT" {
							t.Errorf("isolated order query symbol=%q, want ETHUSDT", r.URL.Query().Get("symbol"))
						}
						_, _ = w.Write([]byte(tc.isoOrder))
					} else {
						_, _ = w.Write([]byte(tc.crossOrder))
					}
				case "/sapi/v1/margin/openOrderList":
					if r.Header.Get("X-MBX-APIKEY") != "test-key" || r.URL.Query().Get("signature") == "" {
						t.Errorf("margin OCO request missing API key header or signature")
					}
					if r.URL.Query().Get("isIsolated") == "TRUE" {
						_, _ = w.Write([]byte(tc.isoOCO))
					} else {
						_, _ = w.Write([]byte(tc.crossOCO))
					}
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			client := binancesdk.NewClient("test-key", "test-secret")
			client.BaseURL = server.URL
			adapter := &BinanceSpotMarginAdapter{BinanceSpotAdapter: &BinanceSpotAdapter{client: client}}
			err := adapter.VerifySpotMarginAccountFlat(context.Background())
			if tc.wantErr == "" && err != nil {
				t.Fatalf("VerifySpotMarginAccountFlat: %v", err)
			}
			if tc.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tc.wantErr)) {
				t.Fatalf("flatness error = %v, want substring %q", err, tc.wantErr)
			}
		})
	}
}
