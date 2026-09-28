package main

import (
	"context"
	"fmt"
	"math"
	"sort"
	"time"

	"quantmesh/exchange"
	"quantmesh/indicators"
	"quantmesh/strategy"
)

func runtimeVolatilityHistoryLoader(ex exchange.IExchange, symbol string) strategy.VolatilityHistoryLoader {
	return func(ctx context.Context, required int, asOf time.Time) ([]indicators.PricePoint, error) {
		if required < 1 || required >= 1000 {
			return nil, fmt.Errorf("hourly history request exceeds supported single-page evidence")
		}
		candles, err := ex.GetHistoricalKlines(ctx, symbol, "1h", required+1)
		if err != nil {
			return nil, err
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		end := asOf.UTC().Truncate(time.Hour)
		points := make([]indicators.PricePoint, 0, len(candles))
		for _, c := range candles {
			if c == nil || c.Symbol != symbol {
				return nil, fmt.Errorf("hourly history has missing or foreign symbol")
			}
			start := time.UnixMilli(c.Timestamp).UTC()
			if !start.Equal(start.Truncate(time.Hour)) || start.After(end) {
				return nil, fmt.Errorf("invalid hourly candle timestamp")
			}
			if start.Equal(end) {
				continue
			} // current, unclosed bar: never use future data
			if !c.IsClosed {
				return nil, fmt.Errorf("past hourly bar is not closed")
			}
			if err := c.Validate(); err != nil {
				return nil, fmt.Errorf("invalid hourly OHLC: %w", err)
			}
			for _, v := range []float64{c.Open, c.High, c.Low, c.Close, c.Volume} {
				if math.IsNaN(v) || math.IsInf(v, 0) {
					return nil, fmt.Errorf("non-finite hourly evidence")
				}
			}
			points = append(points, indicators.PricePoint{Timestamp: start.Add(time.Hour), Price: c.Close, High: c.High, Low: c.Low, Volume: c.Volume})
		}
		sort.Slice(points, func(i, j int) bool { return points[i].Timestamp.Before(points[j].Timestamp) })
		// Detector performs contiguous-window, freshness and immutable overlap
		// checks before publication; the adapter cannot conceal missing hours.
		return points, nil
	}
}
