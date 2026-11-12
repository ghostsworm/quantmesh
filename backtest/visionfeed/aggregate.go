package visionfeed

import (
	"fmt"

	"quantmesh/exchange"
)

// AggregateExact folds verified 1m candles into the requested interval.
// Every output bucket must contain exactly its full set of sub-candles, in
// order and without gaps. Nothing is dropped and nothing is filled in.
func AggregateExact(minutes []*exchange.Candle, intervalMs, firstMs, endMs int64) ([]*exchange.Candle, error) {
	if intervalMs <= 0 || intervalMs%MinuteMs != 0 {
		return nil, fmt.Errorf("visionfeed: aggregate: interval %d is not a positive whole number of minutes", intervalMs)
	}
	if firstMs%intervalMs != 0 || endMs%intervalMs != 0 || endMs <= firstMs {
		return nil, fmt.Errorf("visionfeed: aggregate: window [%d,%d) is not aligned to %d", firstMs, endMs, intervalMs)
	}
	wanted := (endMs - firstMs) / MinuteMs
	if int64(len(minutes)) != wanted {
		return nil, fmt.Errorf("visionfeed: aggregate: got %d minute candles, want %d", len(minutes), wanted)
	}
	perBucket := intervalMs / MinuteMs
	out := make([]*exchange.Candle, 0, (endMs-firstMs)/intervalMs)
	for bucket := firstMs; bucket < endMs; bucket += intervalMs {
		base := (bucket - firstMs) / MinuteMs
		first := minutes[base]
		if first == nil || first.Timestamp != bucket {
			return nil, fmt.Errorf("visionfeed: aggregate: bucket %d does not start on its first minute", bucket)
		}
		high, low := first.High, first.Low
		volume := 0.0
		var closePrice float64
		for offset := int64(0); offset < perBucket; offset++ {
			candle := minutes[base+offset]
			want := bucket + offset*MinuteMs
			if candle == nil || candle.Timestamp != want {
				return nil, fmt.Errorf("visionfeed: aggregate: bucket %d missing sub-candle at %d", bucket, want)
			}
			if candle.High > high {
				high = candle.High
			}
			if candle.Low < low {
				low = candle.Low
			}
			volume += candle.Volume
			closePrice = candle.Close
		}
		out = append(out, &exchange.Candle{
			Symbol: first.Symbol, Timestamp: bucket, IsClosed: true,
			Open: first.Open, High: high, Low: low, Close: closePrice, Volume: volume,
		})
	}
	return out, nil
}
