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
