package binance

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	binancesdk "github.com/adshao/go-binance/v2"
)

const spotFeeMinuteMillis = int64(time.Minute / time.Millisecond)

func (b *BinanceSpotAdapter) historicalFeeAssetQuoteRate(ctx context.Context, asset string, tradeTime int64) (float64, error) {
	if tradeTime <= 0 {
		return 0, fmt.Errorf("trade timestamp is missing")
	}
	if b.feeHistoricalRateFetcher != nil {
		return b.feeHistoricalRateFetcher(ctx, asset, b.quoteAsset, tradeTime)
	}
	minute := tradeTime / spotFeeMinuteMillis * spotFeeMinuteMillis
	prices, ok := b.fetchSpotFeeMinutePrices(ctx, strings.ToUpper(asset)+strings.ToUpper(b.quoteAsset), []int64{minute})
	if ok {
		return prices[minute], nil
	}
	prices, ok = b.fetchSpotFeeMinutePrices(ctx, strings.ToUpper(b.quoteAsset)+strings.ToUpper(asset), []int64{minute})
	if ok && isFinitePositive(prices[minute]) {
		return 1 / prices[minute], nil
	}
	return 0, fmt.Errorf("historical 1m candle does not cover the trade minute")
}

// convertThirdAssetFeesAtHistoricalMinute values non-base/non-quote spot fees
// using the close of the UTC minute containing the execution. Missing markets,
// failed requests, or absent candles remain explicitly unconverted.
func (b *BinanceSpotAdapter) convertThirdAssetFeesAtHistoricalMinute(ctx context.Context, trades []*UserTrade) {
	byAsset := make(map[string][]*UserTrade)
	for _, trade := range trades {
		if trade == nil {
			continue
		}
		asset := strings.ToUpper(strings.TrimSpace(trade.CommissionAsset))
		if trade.Commission == 0 || asset == "" || asset == strings.ToUpper(b.baseAsset) || asset == strings.ToUpper(b.quoteAsset) {
			continue
		}
		byAsset[asset] = append(byAsset[asset], trade)
	}
	for asset, rows := range byAsset {
		if err := ctx.Err(); err != nil {
			return
		}
		minuteSet := make(map[int64]struct{}, len(rows))
		for _, row := range rows {
			minuteSet[row.Time.UnixMilli()/spotFeeMinuteMillis*spotFeeMinuteMillis] = struct{}{}
		}
		minutes := make([]int64, 0, len(minuteSet))
		for minute := range minuteSet {
			minutes = append(minutes, minute)
		}
		// Small page size and sparse fee-asset requests are expected; query disjoint
		// runs separately so a long history cannot silently exceed Binance limits.
		prices, ok := b.fetchSpotFeeMinutePrices(ctx, asset+strings.ToUpper(b.quoteAsset), minutes)
		inverse := false
		if !ok {
			prices, ok = b.fetchSpotFeeMinutePrices(ctx, strings.ToUpper(b.quoteAsset)+asset, minutes)
			inverse = ok
		}
		if !ok {
			continue
		}
		for _, row := range rows {
			minute := row.Time.UnixMilli() / spotFeeMinuteMillis * spotFeeMinuteMillis
			price, exists := prices[minute]
			if !exists || !isFinitePositive(price) {
				continue
			}
			if inverse {
				price = 1 / price
			}
			converted := row.Commission * price
			if math.IsNaN(converted) || math.IsInf(converted, 0) {
				continue
			}
			row.CommissionQuote, row.CommissionQuoteRate, row.CommissionQuoteKnown = converted, price, true
		}
	}
}

func (b *BinanceSpotAdapter) fetchSpotFeeMinutePrices(ctx context.Context, pair string, minutes []int64) (map[int64]float64, bool) {
	if b == nil || b.client == nil || len(minutes) == 0 {
		return nil, false
	}
	minuteSet := make(map[int64]struct{}, len(minutes))
	for _, minute := range minutes {
		minuteSet[minute] = struct{}{}
	}
	min, max := minutes[0], minutes[0]
	for _, minute := range minutes[1:] {
		if minute < min {
			min = minute
		}
		if minute > max {
			max = minute
		}
	}
	prices := make(map[int64]float64, len(minutes))
	requestCount := 0
	for start := min; start <= max; {
		requestCount++
		if requestCount > 12 {
			return prices, false
		}
		end := start + 999*spotFeeMinuteMillis
		if end > max {
			end = max
		}
		var candles []*binancesdk.Kline
		err := b.withRateLimit(ctx, func() error {
			var fetchErr error
			candles, fetchErr = b.client.NewKlinesService().Symbol(pair).Interval("1m").StartTime(start).EndTime(end + spotFeeMinuteMillis - 1).Limit(1000).Do(ctx)
			return fetchErr
		})
		if err != nil {
			return nil, false
		}
		for _, candle := range candles {
			if candle == nil || candle.OpenTime < start || candle.OpenTime > end {
				continue
			}
			if _, required := minuteSet[candle.OpenTime]; !required {
				continue
			}
			closePrice, parseErr := strconv.ParseFloat(candle.Close, 64)
			if parseErr == nil && isFinitePositive(closePrice) {
				prices[candle.OpenTime] = closePrice
			}
		}
		if end == max {
			break
		}
		start = end + spotFeeMinuteMillis
	}
	if len(prices) != len(minuteSet) {
		return prices, false
	}
	return prices, true
}
