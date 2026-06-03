package backtest

import (
	"context"
	"errors"
	"testing"
	"time"

	"quantmesh/exchange"
)

type fakeSmartParamsExchange struct {
	exchange.IExchange
	price   float64
	candles []*exchange.Candle
}

func (e *fakeSmartParamsExchange) GetLatestPrice(ctx context.Context, symbol string) (float64, error) {
	return e.price, nil
}

func (e *fakeSmartParamsExchange) GetHistoricalKlines(ctx context.Context, symbol string, interval string, limit int) ([]*exchange.Candle, error) {
	if len(e.candles) == 0 {
		return nil, errors.New("no candles")
	}
	return e.candles, nil
}

func TestSmartParamsRecommendationForAllStrategies(t *testing.T) {
	fake := &fakeSmartParamsExchange{price: 100, candles: smartParamCandles(30, 100, 1)}
	service := NewSmartParamsService(func(exchangeName, marketType string) (exchange.IExchange, error) {
		return fake, nil
	}, SmartParamsConfig{})

	strategies := []string{"grid", "momentum", "mean_reversion", "trend_following", "dca", "martingale"}
	for _, strategy := range strategies {
		t.Run(strategy, func(t *testing.T) {
			rec, err := service.GetRecommendation(context.Background(), "binance", "futures", "BTCUSDT", strategy, 10000)
			if err != nil {
				t.Fatalf("GetRecommendation failed: %v", err)
			}
			if rec.Strategy != strategy || rec.CurrentPrice != 100 {
				t.Fatalf("unexpected recommendation: %#v", rec)
			}
			if len(rec.Params) == 0 || rec.Reasoning == "" || rec.Confidence <= 0 {
				t.Fatalf("recommendation should include params/reasoning/confidence: %#v", rec)
			}
		})
	}
}

func TestSmartParamsCacheAndClear(t *testing.T) {
	calls := 0
	fake := &fakeSmartParamsExchange{price: 100, candles: smartParamCandles(30, 100, 1)}
	service := NewSmartParamsService(func(exchangeName, marketType string) (exchange.IExchange, error) {
		calls++
		return fake, nil
	}, SmartParamsConfig{})

	first, err := service.getCurrentPrice(context.Background(), "binance", "futures", "BTCUSDT")
	if err != nil {
		t.Fatalf("getCurrentPrice first failed: %v", err)
	}
	fake.price = 200
	second, err := service.getCurrentPrice(context.Background(), "binance", "futures", "BTCUSDT")
	if err != nil {
		t.Fatalf("getCurrentPrice second failed: %v", err)
	}
	if first != second || second.CurrentPrice != 100 {
		t.Fatalf("expected cached price, first=%#v second=%#v", first, second)
	}
	if calls != 1 {
		t.Fatalf("expected factory called once for cached price, got %d", calls)
	}

	service.ClearCache()
	third, err := service.getCurrentPrice(context.Background(), "binance", "futures", "BTCUSDT")
	if err != nil {
		t.Fatalf("getCurrentPrice third failed: %v", err)
	}
	if third.CurrentPrice != 200 {
		t.Fatalf("expected refreshed price 200, got %v", third.CurrentPrice)
	}
}

func TestSmartParamsEvictsExpiredCache(t *testing.T) {
	service := NewSmartParamsService(nil, SmartParamsConfig{
		PriceCacheTTL:      time.Millisecond,
		VolatilityCacheTTL: time.Millisecond,
	})
	service.priceCache["old"] = &PriceInfo{UpdatedAt: time.Now().Add(-time.Hour)}
	service.volatilityCache["old"] = &VolatilityInfo{UpdatedAt: time.Now().Add(-time.Hour)}

	service.evictExpiredCache()

	if len(service.priceCache) != 0 || len(service.volatilityCache) != 0 {
		t.Fatalf("expected expired caches to be evicted, price=%#v vol=%#v", service.priceCache, service.volatilityCache)
	}
}

func TestSmartParamsMultipleRecommendationsSortedAndUnsupported(t *testing.T) {
	fake := &fakeSmartParamsExchange{price: 100, candles: smartParamCandles(30, 100, 2)}
	service := NewSmartParamsService(func(exchangeName, marketType string) (exchange.IExchange, error) {
		return fake, nil
	}, SmartParamsConfig{})

	recs, err := service.GetMultipleRecommendations(context.Background(), "binance", "futures", "BTCUSDT", []string{"martingale", "grid", "unknown"}, 20000)
	if err != nil {
		t.Fatalf("GetMultipleRecommendations failed: %v", err)
	}
	if len(recs) != 2 {
		t.Fatalf("expected unsupported strategy to be skipped, got %d", len(recs))
	}
	if recs[0].Confidence < recs[1].Confidence {
		t.Fatalf("recommendations should be sorted by confidence: %#v", recs)
	}

	if _, err := service.GetRecommendation(context.Background(), "binance", "futures", "BTCUSDT", "unknown", 10000); err == nil {
		t.Fatal("expected unsupported strategy to fail")
	}
}

func TestSmartParamsVolatilityHelpers(t *testing.T) {
	service := NewSmartParamsService(nil, SmartParamsConfig{})

	if got := service.calculateStdDev([]float64{1, 2, 3}); got <= 0 {
		t.Fatalf("stddev should be positive, got %v", got)
	}
	if got := service.calculateStdDev(nil); got != 0 {
		t.Fatalf("empty stddev = %v, want 0", got)
	}

	levelTests := map[float64]string{
		90: "極高",
		70: "高",
		50: "中等",
		30: "較低",
		10: "低",
	}
	for input, want := range levelTests {
		if got := service.getVolatilityLevel(input); got != want {
			t.Fatalf("getVolatilityLevel(%v) = %q, want %q", input, got, want)
		}
	}

	defaultVol := service.getDefaultVolatility("BTCUSDT")
	if defaultVol.Symbol != "BTCUSDT" || defaultVol.Volatility7d <= 0 || defaultVol.TrendDirection != "sideways" {
		t.Fatalf("unexpected default volatility: %#v", defaultVol)
	}
}

func TestSmartParamsVolatilityInfoFallbackFactory(t *testing.T) {
	futures := &fakeSmartParamsExchange{price: 100, candles: smartParamCandles(30, 100, -1)}
	service := NewSmartParamsService(func(exchangeName, marketType string) (exchange.IExchange, error) {
		if marketType == "spot" {
			return nil, errors.New("spot unavailable")
		}
		return futures, nil
	}, SmartParamsConfig{})

	vol, err := service.getVolatilityInfo(context.Background(), "binance", "BTCUSDT")
	if err != nil {
		t.Fatalf("getVolatilityInfo failed: %v", err)
	}
	if vol.TrendDirection != "down" {
		t.Fatalf("TrendDirection = %q, want down", vol.TrendDirection)
	}
	if vol.Volatility7d <= 0 || vol.AverageRange <= 0 {
		t.Fatalf("expected positive volatility metrics: %#v", vol)
	}

	cached, err := service.getVolatilityInfo(context.Background(), "binance", "BTCUSDT")
	if err != nil {
		t.Fatalf("cached getVolatilityInfo failed: %v", err)
	}
	if cached != vol {
		t.Fatal("expected cached volatility object")
	}
}

func smartParamCandles(count int, start float64, drift float64) []*exchange.Candle {
	candles := make([]*exchange.Candle, 0, count)
	price := start
	for i := 0; i < count; i++ {
		next := price + drift
		high := maxFloat(price, next) + 2
		low := minFloat(price, next) - 2
		candles = append(candles, &exchange.Candle{
			Symbol:    "BTCUSDT",
			Open:      price,
			High:      high,
			Low:       low,
			Close:     next,
			Volume:    1000,
			Timestamp: int64(i) * int64(time.Hour/time.Millisecond),
			IsClosed:  true,
		})
		price = next
	}
	return candles
}

func maxFloat(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

func minFloat(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
