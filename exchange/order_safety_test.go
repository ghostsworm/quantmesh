package exchange

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"quantmesh/exchange/income"
)

func TestUnsupportedOrderFillHistoryFailsClosed(t *testing.T) {
	tests := []struct {
		name string
		get  func(context.Context, string, int64) ([]*OrderFill, error)
	}{
		{name: "AscendEX", get: (&ascendexWrapper{}).GetOrderFills},
		{name: "Bitfinex", get: (&bitfinexWrapper{}).GetOrderFills},
		{name: "BitMEX", get: (&bitmexWrapper{}).GetOrderFills},
		{name: "Bitrue", get: (&bitrueWrapper{}).GetOrderFills},
		{name: "Bitkub", get: (&bitkubSpotWrapper{}).GetOrderFills},
		{name: "BTCC", get: (&btccWrapper{}).GetOrderFills},
		{name: "CoinSPH", get: (&coinsphSpotWrapper{}).GetOrderFills},
		{name: "Crypto.com", get: (&cryptocomWrapper{}).GetOrderFills},
		{name: "Deribit", get: (&deribitWrapper{}).GetOrderFills},
		{name: "Huobi", get: (&huobiWrapper{}).GetOrderFills},
		{name: "Kraken", get: (&krakenWrapper{}).GetOrderFills},
		{name: "KuCoin", get: (&kucoinWrapper{}).GetOrderFills},
		{name: "Phemex", get: (&phemexWrapper{}).GetOrderFills},
		{name: "Poloniex", get: (&poloniexWrapper{}).GetOrderFills},
		{name: "WOO X", get: (&wooxWrapper{}).GetOrderFills},
		{name: "XT.com", get: (&xtcomWrapper{}).GetOrderFills},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fills, err := tt.get(context.Background(), "BTCUSDT", 1)
			if !errors.Is(err, ErrNotImplemented) {
				t.Fatalf("GetOrderFills() error = %v, want ErrNotImplemented", err)
			}
			if fills != nil {
				t.Fatalf("GetOrderFills() fills = %v, want nil on unsupported history", fills)
			}
		})
	}
}

func TestUnsupportedIncomeHistoryFailsClosed(t *testing.T) {
	tests := []struct {
		name string
		get  func(context.Context, string, string, int64, int64) ([]*income.Income, error)
	}{
		{name: "AscendEX", get: (&ascendexWrapper{}).GetIncomeHistory},
		{name: "Binance spot", get: (&binanceSpotWrapper{}).GetIncomeHistory},
		{name: "Binance spot margin", get: (&binanceSpotMarginWrapper{}).GetIncomeHistory},
		{name: "Bitfinex", get: (&bitfinexWrapper{}).GetIncomeHistory},
		{name: "Bitget spot", get: (&bitgetSpotWrapper{}).GetIncomeHistory},
		{name: "BitMEX", get: (&bitmexWrapper{}).GetIncomeHistory},
		{name: "Bitrue", get: (&bitrueWrapper{}).GetIncomeHistory},
		{name: "Bitkub", get: (&bitkubSpotWrapper{}).GetIncomeHistory},
		{name: "BTCC", get: (&btccWrapper{}).GetIncomeHistory},
		{name: "Bybit spot", get: (&bybitSpotWrapper{}).GetIncomeHistory},
		{name: "BingX", get: (&bingxWrapper{}).GetIncomeHistory},
		{name: "CoinSPH", get: (&coinsphSpotWrapper{}).GetIncomeHistory},
		{name: "Crypto.com", get: (&cryptocomWrapper{}).GetIncomeHistory},
		{name: "Deribit", get: (&deribitWrapper{}).GetIncomeHistory},
		{name: "Gate spot", get: (&gateSpotWrapper{}).GetIncomeHistory},
		{name: "Huobi", get: (&huobiWrapper{}).GetIncomeHistory},
		{name: "KuCoin", get: (&kucoinWrapper{}).GetIncomeHistory},
		{name: "Kraken", get: (&krakenWrapper{}).GetIncomeHistory},
		{name: "MEXC", get: (&mexcWrapper{}).GetIncomeHistory},
		{name: "OKX", get: (&okxWrapper{}).GetIncomeHistory},
		{name: "OKX spot", get: (&okxSpotWrapper{}).GetIncomeHistory},
		{name: "Phemex", get: (&phemexWrapper{}).GetIncomeHistory},
		{name: "Poloniex", get: (&poloniexWrapper{}).GetIncomeHistory},
		{name: "WOO X", get: (&wooxWrapper{}).GetIncomeHistory},
		{name: "WhiteBIT", get: (&whitebitWrapper{}).GetIncomeHistory},
		{name: "XT.com", get: (&xtcomWrapper{}).GetIncomeHistory},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payments, err := tt.get(context.Background(), "BTCUSDT", "FUNDING_FEE", 1, 2)
			if !errors.Is(err, ErrNotImplemented) {
				t.Fatalf("GetIncomeHistory() error = %v, want ErrNotImplemented", err)
			}
			if payments != nil {
				t.Fatalf("GetIncomeHistory() payments = %v, want nil on unsupported history", payments)
			}
		})
	}
}

func TestOrderIDStringUsesDecimalFormat(t *testing.T) {
	got := orderIDString(123456789)
	if got != "123456789" {
		t.Fatalf("orderIDString() = %q, want decimal order ID", got)
	}
	if got == string(rune(123456789)) {
		t.Fatalf("orderIDString() used rune conversion instead of decimal formatting")
	}
}

func TestJoinOrderOpErrorsKeepsFailuresVisible(t *testing.T) {
	err := joinOrderOpErrors("batch cancel test orders", []error{
		nil,
		errors.New("first failure"),
		errors.New("second failure"),
	})
	if err == nil {
		t.Fatal("joinOrderOpErrors() returned nil for failed order operations")
	}
	text := err.Error()
	for _, want := range []string{"batch cancel test orders failed", "first failure", "second failure"} {
		if !strings.Contains(text, want) {
			t.Fatalf("joined error %q does not contain %q", text, want)
		}
	}
}

func TestWrappersDoNotUseRuneOrderIDConversion(t *testing.T) {
	files, err := filepath.Glob("wrapper_*.go")
	if err != nil {
		t.Fatalf("glob wrappers: %v", err)
	}
	for _, file := range files {
		content, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}
		if strings.Contains(string(content), "string(rune(orderID))") {
			t.Fatalf("%s still converts numeric order IDs through rune", file)
		}
	}
}
