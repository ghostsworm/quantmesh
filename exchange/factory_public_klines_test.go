package exchange

import (
	"context"
	"os"
	"strings"
	"testing"
)

func TestNewExchangeForPublicKlines_Binance(t *testing.T) {
	ex, err := NewExchangeForPublicKlines("binance", "BTCUSDT")
	if err != nil {
		t.Fatalf("NewExchangeForPublicKlines failed: %v", err)
	}
	if ex == nil {
		t.Fatal("expected non-nil exchange")
	}
	if !strings.EqualFold(ex.GetName(), "binance") {
		t.Errorf("expected name binance, got %s", ex.GetName())
	}
	// 拉取真實 K 線需要外網，與 public_exchange_live_test.go 用同一個開關門控。
	// 原先靠比對錯誤字串來跳過，但幣安返回的是 `<APIError> rsp={"error":403}`，
	// 不含 "Forbidden" 字樣，於是漏判成真失敗——單元測試不該依賴外部 API 可達性。
	if os.Getenv("QUANTMESH_LIVE_EXCHANGE_TESTS") != "1" {
		t.Skip("set QUANTMESH_LIVE_EXCHANGE_TESTS=1 to call live exchange public APIs")
	}

	ctx := context.Background()
	klines, err := ex.GetHistoricalKlines(ctx, "BTCUSDT", "1m", 10)
	if err != nil {
		t.Fatalf("GetHistoricalKlines failed: %v", err)
	}
	if len(klines) > 10 {
		t.Errorf("expected at most 10 klines, got %d", len(klines))
	}
}

func TestNewExchangeForPublicKlines_Unsupported(t *testing.T) {
	_, err := NewExchangeForPublicKlines("unsupported_exchange", "BTCUSDT")
	if err == nil {
		t.Fatal("expected error for unsupported exchange")
	}
}

func TestNewExchangeForPublicKlines_CaseInsensitive(t *testing.T) {
	ex, err := NewExchangeForPublicKlines("BINANCE", "ethusdt")
	if err != nil {
		t.Fatalf("NewExchangeForPublicKlines(BINANCE) failed: %v", err)
	}
	if ex == nil {
		t.Fatal("expected non-nil exchange")
	}
	if !strings.EqualFold(ex.GetName(), "binance") {
		t.Errorf("expected name binance, got %s", ex.GetName())
	}
}
