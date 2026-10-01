package binance

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	binancesdk "github.com/adshao/go-binance/v2"
)

const spotFeeMinuteMillis = int64(time.Minute / time.Millisecond)

func (b *BinanceSpotAdapter) historicalFeeAssetQuoteRate(ctx context.Context, asset string, tradeTime int64) (float64, error) {
	return b.historicalAssetQuoteRate(ctx, asset, b.quoteAsset, tradeTime)
}

func (b *BinanceSpotAdapter) historicalAssetQuoteRate(ctx context.Context, asset, quote string, tradeTime int64) (float64, error) {
	rates, err := b.historicalAssetQuoteRates(ctx, asset, quote, []int64{tradeTime})
	if err != nil {
		return 0, err
	}
	minute := tradeTime / spotFeeMinuteMillis * spotFeeMinuteMillis
	if rate, exists := rates[minute]; exists {
		return rate, nil
	}
	return 0, fmt.Errorf("historical 1m candle does not cover the requested quote minute")
}

// historicalAssetQuoteRates fetches each unique UTC minute once and limits each
// request window to Binance's 1000-candle maximum, even for sparse 90-day pages.
func (b *BinanceSpotAdapter) historicalAssetQuoteRates(ctx context.Context, asset, quote string, tradeTimes []int64) (map[int64]float64, error) {
	rates := make(map[int64]float64)
	if err := ctx.Err(); err != nil {
		return rates, err
	}
	asset, quote = strings.ToUpper(strings.TrimSpace(asset)), strings.ToUpper(strings.TrimSpace(quote))
	if asset == "" || quote == "" {
		return nil, fmt.Errorf("historical quote requires an asset and quote currency")
	}
	minuteSet := make(map[int64]struct{}, len(tradeTimes))
	minuteTradeTimes := make(map[int64]int64, len(tradeTimes))
	for _, tradeTime := range tradeTimes {
		if tradeTime <= 0 {
			continue
		}
		minute := tradeTime / spotFeeMinuteMillis * spotFeeMinuteMillis
		minuteSet[minute] = struct{}{}
		if _, exists := minuteTradeTimes[minute]; !exists {
			minuteTradeTimes[minute] = tradeTime
		}
	}
	minutes := make([]int64, 0, len(minuteSet))
	for minute := range minuteSet {
		minutes = append(minutes, minute)
	}
	sort.Slice(minutes, func(i, j int) bool { return minutes[i] < minutes[j] })
	if asset == quote {
		for _, minute := range minutes {
			rates[minute] = 1
		}
		return rates, nil
	}
	if b.feeHistoricalRateFetcher != nil {
		for _, minute := range minutes {
			if err := ctx.Err(); err != nil {
				return rates, err
			}
			rate, err := b.feeHistoricalRateFetcher(ctx, asset, quote, minuteTradeTimes[minute])
			if err == nil && isFinitePositive(rate) {
				rates[minute] = rate
			}
		}
		return rates, nil
	}
	for offset := 0; offset < len(minutes); {
		end := offset + 1
		for end < len(minutes) && minutes[end]-minutes[offset] <= 999*spotFeeMinuteMillis {
			end++
		}
		window := minutes[offset:end]
		prices, _ := b.fetchSpotFeeMinutePrices(ctx, asset+quote, window)
		for minute, price := range prices {
			if isFinitePositive(price) {
				rates[minute] = price
			}
		}
		missing := make([]int64, 0, len(window)-len(prices))
		for _, minute := range window {
			if _, exists := rates[minute]; !exists {
				missing = append(missing, minute)
			}
		}
		if len(missing) > 0 {
			inversePrices, _ := b.fetchSpotFeeMinutePrices(ctx, quote+asset, missing)
			for minute, price := range inversePrices {
				if isFinitePositive(price) {
					rates[minute] = 1 / price
				}
			}
		}
		if err := ctx.Err(); err != nil {
			return rates, err
		}
		offset = end
	}
	return rates, nil
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
