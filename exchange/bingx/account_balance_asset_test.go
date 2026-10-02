package bingx

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetAccountSelectsSettlementAssetAndUsesEquity(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/openApi/swap/v3/user/balance" {
			t.Fatalf("unexpected API path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"msg":"","data":[{"asset":"USDC","balance":"30","equity":"32","availableMargin":"28"},{"asset":"USDT","balance":"100","equity":"107","unrealizedProfit":"7","availableMargin":"91"}]}`))
	}))
	defer server.Close()

	client := NewBingXClient("api-key", "secret-key", false)
	client.baseURL = server.URL
	client.httpClient = server.Client()
	adapter := &Adapter{client: client, symbol: "BTC-USDT", quoteAsset: "USDT"}

	account, err := adapter.GetAccount(context.Background())
	if err != nil {
		t.Fatalf("GetAccount: %v", err)
	}
	if account.BalanceAsset != "USDT" || account.TotalWalletBalance != 100 || account.TotalMarginBalance != 107 || account.AvailableBalance != 91 {
		t.Fatalf("unexpected USDT account snapshot: %+v", account)
	}

	balance, err := adapter.GetBalance(context.Background())
	if err != nil {
		t.Fatalf("GetBalance: %v", err)
	}
	if balance != 91 {
		t.Fatalf("GetBalance() = %v, want USDT availableMargin 91", balance)
	}
}

func TestGetAccountFailsWhenSettlementAssetIsMissing(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"data":[{"asset":"USDC","balance":"30","equity":"32","availableMargin":"28"}]}`))
	}))
	defer server.Close()

	client := NewBingXClient("api-key", "secret-key", false)
	client.baseURL = server.URL
	client.httpClient = server.Client()
	adapter := &Adapter{client: client, quoteAsset: "USDT"}
	if _, err := adapter.GetAccount(context.Background()); err == nil {
		t.Fatal("GetAccount succeeded without the configured settlement asset")
	}
}
