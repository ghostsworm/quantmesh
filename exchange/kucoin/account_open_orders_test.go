package kucoin

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAdapterAccountOpenOrdersReadsAllSymbolsAndStopOrders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("KC-API-KEY") == "" || r.Header.Get("KC-API-SIGN") == "" || r.Header.Get("KC-API-TIMESTAMP") == "" {
			t.Errorf("missing KuCoin authentication headers: %v", r.Header)
		}
		if r.URL.Query().Has("symbol") || r.URL.Query().Get("status") != "active" || r.URL.Query().Get("pageSize") != "50" {
			t.Errorf("account order query unexpectedly scoped: %s", r.URL.RawQuery)
		}
		page := r.URL.Query().Get("currentPage")
		var body string
		switch r.URL.Path {
		case "/api/v1/orders":
			switch page {
			case "1":
				body = `{"code":"200000","data":{"currentPage":1,"pageSize":1,"totalNum":2,"totalPage":2,"items":[{"id":"101","symbol":"BTCUSDTM","side":"buy","size":3,"filledSize":0,"status":"open","isActive":true}]}}`
			case "2":
				body = `{"code":"200000","data":{"currentPage":2,"pageSize":1,"totalNum":2,"totalPage":2,"items":[{"id":"102","symbol":"ETHUSDTM","side":"sell","size":4,"filledSize":1,"status":"open","isActive":true}]}}`
			default:
				t.Errorf("unexpected active order page: %s", page)
			}
		case "/api/v1/stopOrders":
			body = `{"code":"200000","data":{"currentPage":1,"pageSize":50,"totalNum":1,"totalPage":1,"items":[{"id":"201","symbol":"SOLUSDTM","side":"buy","size":2,"filledSize":0,"status":"open","isActive":true,"stop":"loss"}]}}`
		default:
			t.Errorf("unexpected endpoint %s", r.URL.Path)
		}
		_, _ = fmt.Fprint(w, body)
	}))
	defer server.Close()

	client := NewKuCoinClient("api-key", "secret-key", "passphrase")
	client.baseURL = server.URL
	client.httpClient = server.Client()
	adapter := &Adapter{client: client}
	orders, err := adapter.GetAccountOpenOrders(context.Background())
	if err != nil {
		t.Fatalf("GetAccountOpenOrders(): %v", err)
	}
	if len(orders) != 3 {
		t.Fatalf("account snapshot returned %d orders, want 3: %+v", len(orders), orders)
	}
	gotSymbols := map[string]bool{}
	for _, order := range orders {
		gotSymbols[order.Symbol] = true
	}
	for _, symbol := range []string{"BTCUSDTM", "ETHUSDTM", "SOLUSDTM"} {
		if !gotSymbols[symbol] {
			t.Errorf("account snapshot missing %s", symbol)
		}
	}
}

func TestAdapterAccountOpenOrdersFailsClosed(t *testing.T) {
	tests := []struct {
		name       string
		activePage string
		stopPage   string
	}{
		{
			name:       "null active order list",
			activePage: `{"code":"200000","data":{"currentPage":1,"pageSize":50,"totalNum":0,"totalPage":0,"items":null}}`,
		},
		{
			name:       "pagination count changed",
			activePage: `{"code":"200000","data":{"currentPage":1,"pageSize":1,"totalNum":2,"totalPage":2,"items":[{"id":"101","symbol":"BTCUSDTM","side":"buy","size":1,"status":"open","isActive":true}]}}`,
		},
		{
			name:       "duplicate order across endpoints",
			activePage: `{"code":"200000","data":{"currentPage":1,"pageSize":50,"totalNum":1,"totalPage":1,"items":[{"id":"101","symbol":"BTCUSDTM","side":"buy","size":1,"status":"open","isActive":true}]}}`,
			stopPage:   `{"code":"200000","data":{"currentPage":1,"pageSize":50,"totalNum":1,"totalPage":1,"items":[{"id":"101","symbol":"BTCUSDTM","side":"buy","size":1,"status":"open","isActive":true}]}}`,
		},
		{
			name:       "invalid stop order row",
			activePage: `{"code":"200000","data":{"currentPage":1,"pageSize":50,"totalNum":0,"totalPage":0,"items":[]}}`,
			stopPage:   `{"code":"200000","data":{"currentPage":1,"pageSize":50,"totalNum":1,"totalPage":1,"items":[{"id":"202","symbol":"BTCUSDTM","side":"other","size":1,"status":"open","isActive":true}]}}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body := tt.activePage
				if r.URL.Path == "/api/v1/stopOrders" {
					body = tt.stopPage
					if body == "" {
						body = `{"code":"200000","data":{"currentPage":1,"pageSize":50,"totalNum":0,"totalPage":0,"items":[]}}`
					}
				} else if r.URL.Query().Get("currentPage") == "2" {
					body = `{"code":"200000","data":{"currentPage":2,"pageSize":1,"totalNum":3,"totalPage":2,"items":[{"id":"102","symbol":"ETHUSDTM","side":"sell","size":1,"status":"open","isActive":true}]}}`
				}
				_, _ = fmt.Fprint(w, body)
			}))
			defer server.Close()
			client := NewKuCoinClient("api-key", "secret-key", "passphrase")
			client.baseURL = server.URL
			client.httpClient = server.Client()
			if _, err := (&Adapter{client: client}).GetAccountOpenOrders(context.Background()); err == nil {
				t.Fatal("GetAccountOpenOrders() accepted incomplete or invalid evidence")
			}
		})
	}
}
