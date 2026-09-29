package gate

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGateAccountUsesResponseCurrencyAsBalanceAsset(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/futures/btc/accounts":
			_, _ = w.Write([]byte(`{"currency":"btc","total":"2","available":"1","unrealised_pnl":"0.1"}`))
		case "/futures/btc/positions/BTC_USD":
			_, _ = w.Write([]byte(`{}`))
		default:
			t.Errorf("unexpected endpoint %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client := NewClient("api-key", "secret", false)
	client.baseURL = server.URL
	client.httpClient = server.Client()
	adapter := &GateAdapter{client: client, settle: "btc", gateSymbol: "BTC_USD"}
	account, err := adapter.GetAccount(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if account.BalanceAsset != "BTC" || account.TotalMarginBalance != 2.1 {
		t.Fatalf("account response currency not preserved: %+v", account)
	}
}
