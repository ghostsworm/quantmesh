package bybit

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSummarizeBybitSpotQuoteBalance(t *testing.T) {
	tests := []struct {
		name         string
		balances     []AssetCoinBalance
		asset        string
		wantWallet   float64
		wantTransfer float64
		wantErr      bool
	}{
		{
			name: "selects exact quote currency",
			balances: []AssetCoinBalance{
				{Coin: "BTC", WalletBalance: "2", TransferBalance: "1"},
				{Coin: "USDT", WalletBalance: "50", TransferBalance: "40"},
				{Coin: "USDC", WalletBalance: "70", TransferBalance: "60"},
			},
			asset: "usdt", wantWallet: 50, wantTransfer: 40,
		},
		{name: "missing asset is zero", balances: []AssetCoinBalance{{Coin: "BTC", WalletBalance: "2", TransferBalance: "1"}}, asset: "USDT"},
		{name: "rejects bad wallet amount", balances: []AssetCoinBalance{{Coin: "USDT", WalletBalance: "NaN", TransferBalance: "0"}}, asset: "USDT", wantErr: true},
		{name: "rejects transfer above wallet amount", balances: []AssetCoinBalance{{Coin: "USDT", WalletBalance: "4", TransferBalance: "5"}}, asset: "USDT", wantErr: true},
		{name: "rejects duplicate quote rows", balances: []AssetCoinBalance{{Coin: "USDT", WalletBalance: "1", TransferBalance: "1"}, {Coin: "usdt", WalletBalance: "2", TransferBalance: "2"}}, asset: "USDT", wantErr: true},
		{name: "requires asset", asset: " ", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wallet, transfer, err := summarizeBybitSpotQuoteBalance(tt.balances, tt.asset)
			if (err != nil) != tt.wantErr {
				t.Fatalf("error = %v, wantErr %v", err, tt.wantErr)
			}
			if err == nil && (wallet != tt.wantWallet || transfer != tt.wantTransfer) {
				t.Fatalf("balance = (%v, %v), want (%v, %v)", wallet, transfer, tt.wantWallet, tt.wantTransfer)
			}
		})
	}
}

func TestBybitGetAllCoinsBalanceUsesSpotAssetEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v5/asset/transfer/query-account-coins-balance" || r.URL.Query().Get("accountType") != "SPOT" {
			t.Errorf("unexpected request: %s?%s", r.URL.Path, r.URL.RawQuery)
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"retCode":0,"retMsg":"OK","result":{"accountType":"SPOT","balance":[{"coin":"USDT","walletBalance":"10","transferBalance":"8"}]}}`))
	}))
	defer server.Close()

	client := NewBybitClient("key", "secret", false)
	client.baseURL = server.URL
	balances, err := client.GetAllCoinsBalance(context.Background(), "SPOT")
	if err != nil {
		t.Fatalf("GetAllCoinsBalance() error = %v", err)
	}
	if len(balances) != 1 || balances[0].Coin != "USDT" || balances[0].WalletBalance != "10" || balances[0].TransferBalance != "8" {
		t.Fatalf("GetAllCoinsBalance() = %#v", balances)
	}
}
