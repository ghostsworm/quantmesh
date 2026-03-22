package exchange

import (
	"context"
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
	// 可選：嘗試拉取 K 線（需網絡，CI 可能失敗則跳過）
	ctx := context.Background()
	klines, err := ex.GetHistoricalKlines(ctx, "BTCUSDT", "1m", 10)
	if err != nil {
		if strings.Contains(err.Error(), "connection") || strings.Contains(err.Error(), "timeout") {
			t.Skip("network unavailable, skipping klines fetch")
		}
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
