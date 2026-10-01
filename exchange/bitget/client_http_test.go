package bitget

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newMockBitgetClient(t *testing.T, handler http.HandlerFunc) (*Client, func()) {
	t.Helper()
	server := httptest.NewServer(handler)
	client := NewClient("api-key", "secret-key", "passphrase", false)
	client.baseURL = server.URL
	client.httpClient = server.Client()
	return client, server.Close
}

func TestBitgetDoRequestAndWalletTransfer(t *testing.T) {
	client, closeServer := newMockBitgetClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("ACCESS-KEY") != "api-key" || r.Header.Get("ACCESS-SIGN") == "" ||
			r.Header.Get("ACCESS-TIMESTAMP") == "" || r.Header.Get("ACCESS-PASSPHRASE") != "passphrase" {
			t.Fatalf("signed headers missing")
		}
		if r.Header.Get("X-CHANNEL-API-CODE") != "3xh1b" {
			t.Fatalf("channel header = %q", r.Header.Get("X-CHANNEL-API-CODE"))
		}
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body["fromType"] != "spot" || body["toType"] != "usdt_futures" || body["coin"] != "USDT" || body["amount"] != "10" {
			t.Fatalf("unexpected body: %#v", body)
		}
		_, _ = w.Write([]byte(`{"code":"00000","msg":"success","data":{"transferId":"tx-1"},"requestTime":1}`))
	})
	defer closeServer()

	transferID, err := client.WalletTransferV2(context.Background(), "spot", "usdt_futures", "USDT", "10")
	if err != nil || transferID != "tx-1" {
		t.Fatalf("WalletTransferV2() = %q, %v", transferID, err)
	}
}

func TestBitgetInternalTransferRoundsDownToSupportedPrecision(t *testing.T) {
	client, closeServer := newMockBitgetClient(t, func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode transfer body: %v", err)
		}
		if got := body["amount"]; got != "1.00000000" {
			t.Fatalf("transfer amount rounded up or formatted incorrectly: %q", got)
		}
		_, _ = w.Write([]byte(`{"code":"00000","msg":"success","data":{"transferId":"tx-round-down"}}`))
	})
	defer closeServer()
	adapter := &BitgetAdapter{client: client, quoteAsset: "USDT"}
	if _, err := adapter.InternalTransfer(context.Background(), "UMFUTURE", "SPOT", "USDT", 1.000000009); err != nil {
		t.Fatal(err)
	}
}

func TestFormatBitgetTransferAmountRejectsInvalidOrRoundedToZeroValues(t *testing.T) {
	for _, amount := range []float64{0, -1, 0.000000009} {
		if formatted, err := formatBitgetTransferAmount(amount); err == nil {
			t.Errorf("formatBitgetTransferAmount(%v)=%q, want error", amount, formatted)
		}
	}
	for amount, want := range map[float64]string{1.234567899: "1.23456789", 10: "10.00000000"} {
		got, err := formatBitgetTransferAmount(amount)
		if err != nil || got != want {
			t.Errorf("formatBitgetTransferAmount(%v)=(%q,%v), want %q", amount, got, err, want)
		}
	}
}

func TestBitgetDoRequestEdgeResponses(t *testing.T) {
	t.Run("api error", func(t *testing.T) {
		client, closeServer := newMockBitgetClient(t, func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"code":"40001","msg":"bad key","data":null}`))
		})
		defer closeServer()
		if _, err := client.DoRequest(context.Background(), http.MethodGet, "/api/v2/demo", nil); err == nil || !strings.Contains(err.Error(), "code=40001") {
			t.Fatalf("expected API error, got %v", err)
		}
	})

	t.Run("invalid json", func(t *testing.T) {
		client, closeServer := newMockBitgetClient(t, func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`not-json`))
		})
		defer closeServer()
		if _, err := client.DoRequest(context.Background(), http.MethodGet, "/api/v2/demo", nil); err == nil || !strings.Contains(err.Error(), "解析响应失败") {
			t.Fatalf("expected parse error, got %v", err)
		}
	})

	t.Run("empty transfer data", func(t *testing.T) {
		client, closeServer := newMockBitgetClient(t, func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"code":"00000","msg":"success"}`))
		})
		defer closeServer()
		transferID, err := client.WalletTransferV2(context.Background(), "spot", "usdt_futures", "USDT", "10")
		if err != nil || transferID != "" {
			t.Fatalf("empty transfer data = %q, %v", transferID, err)
		}
	})

	t.Run("malformed transfer data", func(t *testing.T) {
		client, closeServer := newMockBitgetClient(t, func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"code":"00000","msg":"success","data":"bad"}`))
		})
		defer closeServer()
		if _, err := client.WalletTransferV2(context.Background(), "spot", "usdt_futures", "USDT", "10"); err == nil || !strings.Contains(err.Error(), "解析劃轉 data") {
			t.Fatalf("expected transfer data parse error, got %v", err)
		}
	})
}

func TestBitgetOrderLookupByClientOrderID(t *testing.T) {
	t.Run("futures", func(t *testing.T) {
		client, closeServer := newMockBitgetClient(t, func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/api/v2/mix/order/detail" || r.URL.Query().Get("clientOid") != "close-1" || r.URL.Query().Get("productType") != "USDT-FUTURES" {
				t.Fatalf("unexpected request: %s", r.URL.String())
			}
			_, _ = w.Write([]byte(`{"code":"00000","msg":"success","data":{"symbol":"BTCUSDT","size":"2","orderId":"7","clientOid":"close-1","filledQty":"2","price":"100","side":"sell","status":"full-fill","priceAvg":"99","uTime":"1000"}}`))
		})
		defer closeServer()
		adapter := &BitgetAdapter{client: client, symbol: "BTCUSDT", productType: "USDT-FUTURES"}
		order, err := adapter.GetOrderByClientOrderID(context.Background(), "BTCUSDT", "close-1")
		if err != nil || order.OrderID != 7 || order.Status != "FILLED" || order.ExecutedQty != 2 {
			t.Fatalf("order=%+v error=%v", order, err)
		}
	})

	t.Run("spot duplicate is rejected", func(t *testing.T) {
		client, closeServer := newMockBitgetClient(t, func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/api/v2/spot/trade/orderInfo" || r.URL.Query().Get("clientOid") != "close-1" {
				t.Fatalf("unexpected request: %s", r.URL.String())
			}
			_, _ = w.Write([]byte(`{"code":"00000","msg":"success","data":[{"symbol":"BTCUSDT","orderId":"7","clientOid":"close-1"},{"symbol":"BTCUSDT","orderId":"8","clientOid":"close-1"}]}`))
		})
		defer closeServer()
		adapter := &BitgetSpotAdapter{client: client, symbol: "BTCUSDT"}
		if _, err := adapter.GetOrderByClientOrderID(context.Background(), "BTCUSDT", "close-1"); err == nil {
			t.Fatal("expected duplicate client IDs to fail closed")
		}
	})
}
