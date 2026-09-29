package okx

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOKXAccountSelectsConfiguredSettlementCurrency(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v5/account/balance":
			_, _ = w.Write([]byte(`{"code":"0","data":[{"details":[{"ccy":"USDT","eq":"100","availBal":"90"},{"ccy":"BTC","eq":"2.5","availBal":"2"}]}]}`))
		case "/api/v5/account/positions":
			_, _ = w.Write([]byte(`{"code":"0","data":[]}`))
		default:
			t.Errorf("unexpected OKX endpoint: %s", r.URL.String())
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client := NewOKXClient("key", "secret", "passphrase", false)
	client.baseURL = server.URL
	client.httpClient = server.Client()

	adapter := &OKXAdapter{client: client, quoteAsset: "btc"}
	account, err := adapter.GetAccount(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if account.BalanceAsset != "BTC" || account.TotalWalletBalance != 2.5 || account.AvailableBalance != 2 {
		t.Fatalf("expected BTC settlement balance, got %+v", account)
	}

	adapter.quoteAsset = "ETH"
	account, err = adapter.GetAccount(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if account.BalanceAsset != "" || account.TotalWalletBalance != 0 {
		t.Fatalf("missing settlement detail must not borrow another currency: %+v", account)
	}
}
