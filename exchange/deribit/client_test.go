package deribit

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSendRequestUsesSingleJSONRPCEndpoint(t *testing.T) {
	var sawPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":[]}`))
	}))
	defer srv.Close()

	base := strings.TrimSuffix(srv.URL, "")
	c := &DeribitClient{
		baseURL:    base,
		httpClient: http.DefaultClient,
	}
	_, err := c.sendRequest(context.Background(), "public/get_instruments", map[string]interface{}{"currency": "BTC"})
	if err != nil {
		t.Fatal(err)
	}
	if sawPath != "/api/v2" {
		t.Fatalf("JSON-RPC 應 POST 至 /api/v2，實際路徑 %q", sawPath)
	}
}

func TestNewDeribitClient(t *testing.T) {
	apiKey := "test_api_key"
	secretKey := "test_secret_key"

	client := NewDeribitClient(apiKey, secretKey, false)
	if client == nil {
		t.Fatal("創建客戶端失败")
	}
	if client.apiKey != apiKey {
		t.Errorf("API Key 設置錯误")
	}
	if client.secretKey != secretKey {
		t.Errorf("Secret Key 設置錯误")
	}
}

func TestNewAdapter(t *testing.T) {
	config := map[string]string{
		"api_key":    "test_api_key",
		"secret_key": "test_secret_key",
		"testnet":    "false",
	}

	adapter, err := NewAdapter(config, "BTCUSDT")
	if err != nil {
		t.Fatalf("創建适配器失败: %v", err)
	}

	if adapter == nil {
		t.Fatal("适配器不能為 nil")
	}

	if adapter.GetName() != "Deribit" {
		t.Errorf("交易所名称錯误: 期望 Deribit, 得到 %s", adapter.GetName())
	}
}

func TestAdapterBasicMethods(t *testing.T) {
	config := map[string]string{
		"api_key":    "test_api_key",
		"secret_key": "test_secret_key",
		"testnet":    "false",
	}

	adapter, err := NewAdapter(config, "BTCUSDT")
	if err != nil {
		t.Fatalf("創建适配器失败: %v", err)
	}

	if adapter.GetPriceDecimals() <= 0 {
		t.Error("價格精度应該大於 0")
	}

	if adapter.GetQuantityDecimals() < 0 {
		t.Error("數量精度不應為負")
	}

	if adapter.GetBaseAsset() == "" {
		t.Error("基础资產不能為空")
	}

	if adapter.GetQuoteAsset() == "" {
		t.Error("报價资產不能為空")
	}
}
