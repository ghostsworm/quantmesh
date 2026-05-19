package exchange

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

func TestLivePublicExchangeMarketData(t *testing.T) {
	if os.Getenv("QUANTMESH_LIVE_EXCHANGE_TESTS") != "1" {
		t.Skip("set QUANTMESH_LIVE_EXCHANGE_TESTS=1 to call live exchange public APIs")
	}

	tests := []struct {
		name      string
		symbol    string
		priceOnly bool
	}{
		{name: "binance", symbol: "BTCUSDT"},
		{name: "bitget", symbol: "BTCUSDT"},
		{name: "bybit", symbol: "BTCUSDT"},
		{name: "gate", symbol: "BTCUSDT"},
		{name: "okx", symbol: "BTCUSDT"},
		{name: "huobi", symbol: "BTCUSDT"},
		{name: "kucoin", symbol: "BTCUSDT"},
		{name: "kraken", symbol: "BTCUSDT"},
		{name: "bitfinex", symbol: "BTCUSDT"},
		{name: "mexc", symbol: "BTCUSDT"},
		{name: "bingx", symbol: "BTCUSDT"},
		{name: "deribit", symbol: "BTCUSDT"},
		{name: "bitmex", symbol: "BTCUSDT"},
		{name: "phemex", symbol: "BTCUSDT"},
		{name: "woox", symbol: "BTCUSDT"},
		{name: "coinex", symbol: "BTCUSDT"},
		{name: "bitrue", symbol: "BTCUSDT"},
		{name: "xtcom", symbol: "BTCUSDT"},
		{name: "btcc", symbol: "BTCUSDT"},
		{name: "ascendex", symbol: "BTCUSDT"},
		{name: "poloniex", symbol: "BTCUSDT"},
		{name: "cryptocom", symbol: "BTCUSDT"},
		{name: "whitebit", symbol: "BTCUSDT", priceOnly: true},
		{name: "bitkub", symbol: "BTCUSDT", priceOnly: true},
		{name: "coinsph", symbol: "BTCUSDT", priceOnly: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
			defer cancel()

			ex, err := NewExchangeForPublicKlines(tt.name, tt.symbol)
			if err != nil {
				t.Fatalf("create public exchange: %v", err)
			}

			if tt.priceOnly {
				price, err := ex.GetLatestPrice(ctx, tt.symbol)
				if err != nil {
					t.Fatalf("get latest price: %v", err)
				}
				if price <= 0 {
					t.Fatalf("latest price must be positive, got %f", price)
				}
				return
			}

			klines, err := ex.GetHistoricalKlines(ctx, tt.symbol, "1m", 2)
			if err != nil {
				if strings.Contains(strings.ToLower(err.Error()), "not implemented") {
					t.Fatalf("historical klines not implemented: %v", err)
				}
				t.Fatalf("get historical klines: %v", err)
			}
			if len(klines) == 0 {
				t.Fatal("expected at least one kline")
			}
		})
	}
}
