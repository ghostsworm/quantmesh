package main

import (
	"context"
	"math"
	"testing"
	"time"

	"quantmesh/exchange"
	"quantmesh/indicators"
)

type hourlyEvidenceExchange struct {
	exchange.IExchange
	candles          []*exchange.Candle
	symbol, interval string
	count            int
}

func (e *hourlyEvidenceExchange) GetHistoricalKlines(ctx context.Context, symbol, interval string, count int) ([]*exchange.Candle, error) {
	e.symbol, e.interval, e.count = symbol, interval, count
	return e.candles, nil
}

func sampleHourlyCandles(end time.Time) []*exchange.Candle {
	out := make([]*exchange.Candle, 7)
	for i := range out {
		out[i] = &exchange.Candle{Symbol: "BTCUSDT", Timestamp: end.Add(time.Duration(-i) * time.Hour).UnixMilli(), Open: 100, High: 110, Low: 90, Close: 105, Volume: 1, IsClosed: true}
	}
	return out // descending; includes unclosed current bar, despite IsClosed=true
}

func TestRuntimeHourlyHistoryLoaderValidatesClosedWindow(t *testing.T) {
	end := time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC)
	ex := &hourlyEvidenceExchange{candles: sampleHourlyCandles(end)}
	points, err := runtimeVolatilityHistoryLoader(ex, "BTCUSDT")(context.Background(), 6, end.Add(20*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if ex.symbol != "BTCUSDT" || ex.interval != "1h" || ex.count != 7 || len(points) != 6 || !points[5].Timestamp.Equal(end) {
		t.Fatalf("invalid request/window: %+v", ex)
	}
	cfg := indicators.DefaultVolatilityRegimeConfig()
	cfg.ShortPeriod, cfg.MediumPeriod, cfg.LongPeriod, cfg.PriceRangePeriod = 3, 3, 3, 3
	if err := indicators.NewVolatilityRegimeDetector(cfg).ReplaceHourlyHistory(points, end); err != nil {
		t.Fatal(err)
	}
}

func TestRuntimeHourlyHistoryRejectsForeignFutureAndInvalidBars(t *testing.T) {
	end := time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC)
	for _, scenario := range []string{"nil", "foreign", "future", "seconds", "nan", "unclosed", "invalid_open"} {
		t.Run(scenario, func(t *testing.T) {
			ex := &hourlyEvidenceExchange{candles: sampleHourlyCandles(end)}
			switch scenario {
			case "nil":
				ex.candles[2] = nil
			case "foreign":
				ex.candles[2].Symbol = "ETHUSDT"
			case "future":
				ex.candles[2].Timestamp = end.Add(time.Hour).UnixMilli()
			case "seconds":
				ex.candles[2].Timestamp = end.Unix()
			case "nan":
				ex.candles[2].Volume = math.NaN()
			case "unclosed":
				ex.candles[2].IsClosed = false
			case "invalid_open":
				ex.candles[2].Open = math.Inf(1)
			}
			if _, err := runtimeVolatilityHistoryLoader(ex, "BTCUSDT")(context.Background(), 6, end); err == nil {
				t.Fatal("invalid history accepted")
			}
		})
	}
}
