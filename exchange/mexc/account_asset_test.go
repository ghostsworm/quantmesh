package mexc

import (
	"context"
	"net/http"
	"testing"
)

func TestMEXCAccountScopesEquityToContractSettlementAsset(t *testing.T) {
	client, closeServer := newMockMEXCClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/private/account/assets" {
			t.Fatalf("unexpected MEXC account path %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"code":0,"success":true,"data":[{"currency":"BTC","availableBalance":2,"equity":2.5,"unrealized":0.2},{"currency":"USDT","availableBalance":100,"equity":120,"unrealized":5}]}`))
	})
	defer closeServer()
	adapter := &Adapter{client: client, symbol: "BTC_USD", settleAsset: "BTC"}
	account, err := adapter.GetAccount(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if account.BalanceAsset != "BTC" || account.TotalMarginBalance != 2.5 || account.AvailableBalance != 2 {
		t.Fatalf("expected BTC settlement account values, got %+v", account)
	}
	if balance, err := adapter.GetBalance(context.Background()); err != nil || balance != 2 {
		t.Fatalf("GetBalance() = %v, %v; want settlement available balance 2 BTC", balance, err)
	}

	adapter.settleAsset = "ETH"
	if account, err := adapter.GetAccount(context.Background()); err == nil || account != nil {
		t.Fatalf("missing settlement row must not borrow another currency: account=%+v err=%v", account, err)
	}
	adapter.settleAsset = ""
	if account, err := adapter.GetAccount(context.Background()); err == nil || account != nil {
		t.Fatalf("missing settlement metadata must fail closed: account=%+v err=%v", account, err)
	}
}
