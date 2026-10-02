package bitget

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

func TestGetAccountFuturesOpenOrdersCoversProductsStatusesAndPages(t *testing.T) {
	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Path != "/api/v2/mix/order/orders-pending" || r.URL.Query().Get("symbol") != "" {
			t.Fatalf("unexpected account-wide request: %s", r.URL.String())
		}
		productType := r.URL.Query().Get("productType")
		status := r.URL.Query().Get("status")
		if r.URL.Query().Get("limit") != "100" || (status != "live" && status != "partially_filled") {
			t.Fatalf("unexpected query: %s", r.URL.RawQuery)
		}
		orders := []bitgetPendingOrder{}
		endID := ""
		if productType == "USDT-FUTURES" && status == "live" && r.URL.Query().Get("idLessThan") == "" {
			for i := 0; i < 100; i++ {
				orders = append(orders, bitgetPendingOrder{OrderID: strconv.Itoa(1000 + i), Symbol: "BTCUSDT"})
			}
			endID = "1099"
		} else if productType == "USDT-FUTURES" && status == "live" {
			if r.URL.Query().Get("idLessThan") != "1099" {
				t.Fatalf("second page cursor=%q", r.URL.Query().Get("idLessThan"))
			}
			orders = append(orders, bitgetPendingOrder{OrderID: "900", Symbol: "ETHUSDT"})
		} else if productType == "USDC-FUTURES" && status == "partially_filled" {
			orders = append(orders, bitgetPendingOrder{OrderID: "901", Symbol: "BTCUSDC"})
		}
		data, err := json.Marshal(map[string]any{"entrustedList": orders, "endId": endID})
		if err != nil {
			t.Fatal(err)
		}
		_, _ = fmt.Fprintf(w, `{"code":"00000","data":%s}`, data)
	}))
	defer server.Close()
	client := NewClient("key", "secret", "pass", false)
	client.baseURL = server.URL
	client.httpClient = server.Client()

	orders, err := client.GetAccountFuturesOpenOrders(context.Background())
	if err != nil {
		t.Fatalf("GetAccountFuturesOpenOrders() error = %v", err)
	}
	if len(orders) != 102 || requests != 7 {
		t.Fatalf("orders=%d requests=%d, want orders=102 requests=7", len(orders), requests)
	}
}

func TestGetAccountSpotOpenOrdersCoversAllSymbolsNormalTPSLAndPages(t *testing.T) {
	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Path != "/api/v2/spot/trade/unfilled-orders" || r.URL.Query().Get("symbol") != "" || r.URL.Query().Get("limit") != "100" {
			t.Fatalf("unexpected account-wide request: %s", r.URL.String())
		}
		orderType := r.URL.Query().Get("tpslType")
		orders := []bitgetSpotOpenOrder{}
		if orderType == "normal" && r.URL.Query().Get("idLessThan") == "" {
			for i := 0; i < 100; i++ {
				orders = append(orders, bitgetSpotOpenOrder{OrderID: strconv.Itoa(2000 + i), Symbol: "BTCUSDT"})
			}
		} else if orderType == "normal" {
			if r.URL.Query().Get("idLessThan") != "2099" {
				t.Fatalf("second page cursor=%q", r.URL.Query().Get("idLessThan"))
			}
			orders = append(orders, bitgetSpotOpenOrder{OrderID: "1999", Symbol: "ETHUSDT"})
		} else if orderType == "tpsl" {
			orders = append(orders, bitgetSpotOpenOrder{OrderID: "1998", Symbol: "BTCUSDT"})
		} else {
			t.Fatalf("unexpected tpslType=%q", orderType)
		}
		data, err := json.Marshal(orders)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = fmt.Fprintf(w, `{"code":"00000","data":%s}`, data)
	}))
	defer server.Close()
	client := NewClient("key", "secret", "pass", false)
	client.baseURL = server.URL
	client.httpClient = server.Client()

	orders, err := client.GetAccountSpotOpenOrders(context.Background())
	if err != nil {
		t.Fatalf("GetAccountSpotOpenOrders() error = %v", err)
	}
	if len(orders) != 102 || requests != 3 {
		t.Fatalf("orders=%d requests=%d, want orders=102 requests=3", len(orders), requests)
	}
}

func TestGetAccountFuturesOpenOrdersRejectsFullPageWithoutCursor(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		orders := make([]bitgetPendingOrder, bitgetAccountOpenOrdersPageSize)
		for i := range orders {
			orders[i] = bitgetPendingOrder{OrderID: strconv.Itoa(i + 1), Symbol: "BTCUSDT"}
		}
		data, err := json.Marshal(map[string]any{"entrustedList": orders})
		if err != nil {
			t.Fatal(err)
		}
		_, _ = fmt.Fprintf(w, `{"code":"00000","data":%s}`, data)
	}))
	defer server.Close()
	client := NewClient("key", "secret", "pass", false)
	client.baseURL = server.URL
	client.httpClient = server.Client()

	if _, err := client.GetAccountFuturesOpenOrders(context.Background()); err == nil {
		t.Fatal("expected full page without endId to fail closed")
	}
}
