package binance

import (
	"math"
	"testing"

	"github.com/adshao/go-binance/v2/futures"
)

func TestBinanceAdapterRoundingAndEstimateFinalOrderAmount(t *testing.T) {
	adapter := &BinanceAdapter{
		priceDecimals:    2,
		quantityDecimals: 3,
		tickSize:         0.1,
		stepSize:         0.001,
		baseAsset:        "BTC",
		quoteAsset:       "USDT",
	}

	if got := adapter.roundToTickSize(100.06, SideBuy); math.Abs(got-100.0) > 1e-9 {
		t.Fatalf("buy tick rounding = %f, want 100.0", got)
	}
	if got := adapter.roundToTickSize(100.01, SideSell); math.Abs(got-100.1) > 1e-9 {
		t.Fatalf("sell tick rounding = %f, want 100.1", got)
	}
	if got := adapter.roundToStepSize(0.0019); math.Abs(got-0.001) > 1e-9 {
		t.Fatalf("step rounding = %f, want 0.001", got)
	}
	if got := adapter.EstimateFinalOrderAmount("BTCUSDT", 50000, 0.001, true); got != 50 {
		t.Fatalf("reduce-only notional = %f, want 50", got)
	}
	if got := adapter.EstimateFinalOrderAmount("BTCUSDT", 50000, 0.001, false); got < 100 {
		t.Fatalf("non reduce-only notional = %f, want at least 100", got)
	}

	adapter.stepSize = 0
	if got := adapter.EstimateFinalOrderAmount("BTCUSDT", 50000, 0.000001, false); got < 100 {
		t.Fatalf("fallback quantity notional = %f, want at least 100", got)
	}
	if adapter.GetPriceDecimals() != 2 || adapter.GetQuantityDecimals() != 3 {
		t.Fatal("unexpected adapter precisions")
	}
	if adapter.GetBaseAsset() != "BTC" || adapter.GetQuoteAsset() != "USDT" {
		t.Fatal("unexpected base/quote assets")
	}
}

func TestBinanceAdapterKlinesValidationAndSpikeHandling(t *testing.T) {
	adapter := &BinanceAdapter{}
	klines := []*futures.Kline{
		{Open: "100", High: "105", Low: "95", Close: "102", Volume: "10", OpenTime: 1},
		{Open: "102", High: "250", Low: "100", Close: "103", Volume: "bad-volume", OpenTime: 2},
		{Open: "bad-open", High: "105", Low: "95", Close: "101", Volume: "10", OpenTime: 3},
		{Open: "100", High: "90", Low: "95", Close: "98", Volume: "10", OpenTime: 4},
		{Open: "100", High: "105", Low: "95", Close: "-1", Volume: "10", OpenTime: 5},
	}

	candles, err := adapter.klinesToCandles(klines, "BTCUSDT")
	if err != nil {
		t.Fatalf("klinesToCandles returned error: %v", err)
	}
	if len(candles) != 2 {
		t.Fatalf("valid candle count = %d, want 2: %#v", len(candles), candles)
	}
	if candles[1].Volume != 0 {
		t.Fatalf("bad volume should become zero, got %f", candles[1].Volume)
	}

	valid := &Candle{Open: 10, High: 12, Low: 9, Close: 11, Volume: 1}
	if err := adapter.validateCandle(valid); err != nil {
		t.Fatalf("valid candle returned error: %v", err)
	}
	for _, candle := range []*Candle{
		{Open: 0, High: 12, Low: 9, Close: 11, Volume: 1},
		{Open: 10, High: 8, Low: 9, Close: 11, Volume: 1},
		{Open: 10, High: 10, Low: 9, Close: 11, Volume: 1},
		{Open: 10, High: 12, Low: 11, Close: 10, Volume: 1},
		{Open: 10, High: 12, Low: 9, Close: 11, Volume: -1},
	} {
		if err := adapter.validateCandle(candle); err == nil {
			t.Fatalf("expected invalid candle error for %#v", candle)
		}
	}

	spikes := []*Candle{
		{Symbol: "BTCUSDT", Open: 100, High: 101, Low: 99, Close: 100, Timestamp: 1},
		{Symbol: "BTCUSDT", Open: 100, High: 150, Low: 50, Close: 140, Timestamp: 2},
	}
	if got := adapter.detectPriceSpikes(spikes, 0.2); len(got) != 2 {
		t.Fatalf("detectPriceSpikes length = %d, want 2", len(got))
	}

	clipped := adapter.clipPriceSpikes([]*Candle{
		{Symbol: "BTCUSDT", Open: 100, High: 1000, Low: 1, Close: 101, Timestamp: 1},
		{Symbol: "BTCUSDT", Open: 102, High: 103, Low: 101, Close: 102, Timestamp: 2},
	}, 0.05)
	if clipped[0].High >= 1000 || clipped[0].Low <= 1 {
		t.Fatalf("expected spike clipping, got %#v", clipped[0])
	}
	if got := adapter.clipPriceSpikes(clipped, 0); len(got) != len(clipped) || got[0] != clipped[0] {
		t.Fatal("zero band should return original slice")
	}
}
