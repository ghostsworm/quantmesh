package woox

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPlaceOrderWithOptionsTransmitsReduceOnly(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request OrderRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode venue request: %v", err)
		}
		if !request.ReduceOnly {
			t.Fatal("venue request lost reduce_only=true")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"data":{"order_id":1,"symbol":"BTC_USDT","side":"SELL","order_type":"LIMIT","order_price":100,"order_quantity":1,"status":"NEW"}}`))
	}))
	defer server.Close()

	client := NewWOOXClient("api-key", "secret-key", false)
	client.baseURL = server.URL
	client.httpClient = server.Client()
	adapter := &Adapter{client: client, symbol: "BTC_USDT"}
	if _, err := adapter.PlaceOrderWithOptions(context.Background(), SideSell, 100, 1, "client-1", true); err != nil {
		t.Fatalf("PlaceOrderWithOptions: %v", err)
	}
}

func TestAccountUsesDocumentedCamelCaseFieldsAndFreeCollateral(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v3/accountinfo" {
			t.Fatalf("unexpected API path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"data":{"applicationId":"app-1","account":"main","totalCollateral":100,"freeCollateral":31,"totalAccountValue":250,"totalVaultValue":120,"totalStakingValue":5}}`))
	}))
	defer server.Close()

	client := NewWOOXClient("api-key", "secret-key", false)
	client.baseURL = server.URL
	client.httpClient = server.Client()
	adapter := &Adapter{client: client, symbol: "BTC_USDT"}

	account, err := adapter.GetAccount(context.Background())
	if err != nil {
		t.Fatalf("GetAccount: %v", err)
	}
	if account.TotalWalletBalance != 250 || account.TotalMarginBalance != 100 || account.AvailableBalance != 31 {
		t.Fatalf("unexpected account balances: %+v", account)
	}

	balance, err := adapter.GetBalance(context.Background())
	if err != nil {
		t.Fatalf("GetBalance: %v", err)
	}
	if balance != 31 {
		t.Fatalf("GetBalance() = %v, want freeCollateral 31", balance)
	}
}
