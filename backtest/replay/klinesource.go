package replay

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"quantmesh/exchange"
	"quantmesh/strategy/regime"
)

// AggregateCandles 把升序的 1m（或任意更小周期）K 線聚合為 intervalMs 周期，按 UTC 紀元對齊。
// 聚合桶內缺失的子 K 線不補齊（缺口只會讓該根 K 線的高低點偏窄，不會引入未來數據）。
func AggregateCandles(candles []*exchange.Candle, intervalMs int64) ([]*exchange.Candle, error) {
	if intervalMs <= 0 {
		return nil, fmt.Errorf("aggregate candles: interval must be positive, got %d", intervalMs)
	}
	var out []*exchange.Candle
	var cur *exchange.Candle
	prevTs := int64(-1)
	for _, c := range candles {
		if c == nil {
			continue
		}
		if c.Timestamp <= prevTs {
			return nil, fmt.Errorf("aggregate candles: input not strictly ascending at ts=%d (prev %d)", c.Timestamp, prevTs)
		}
		prevTs = c.Timestamp
		open := c.Timestamp - c.Timestamp%intervalMs
		if cur == nil || cur.Timestamp != open {
			cur = &exchange.Candle{Symbol: c.Symbol, Timestamp: open, Open: c.Open, High: c.High, Low: c.Low, Close: c.Close, Volume: c.Volume, IsClosed: true}
			out = append(out, cur)
			continue
		}
		if c.High > cur.High {
			cur.High = c.High
		}
		if c.Low < cur.Low {
			cur.Low = c.Low
		}
		cur.Close = c.Close
		cur.Volume += c.Volume
	}
	return out, nil
}

// ClosedKlineSource 內存 K 線源（實現 regime.KlineSource），嚴格無未來函數：
// 只返回在 now() 時刻已經收盤（openTime + interval <= now）的 K 線，最多 limit 根（取最近的）。
// now 由回放的模擬時鐘提供。
type ClosedKlineSource struct {
	mu         sync.RWMutex
	symbol     string
	interval   string
	intervalMs int64
	bars       []*exchange.Candle // 升序
	now        func() time.Time
}

// NewClosedKlineSource 用已聚合好的 interval 周期 K 線創建數據源
func NewClosedKlineSource(symbol, interval string, bars []*exchange.Candle, now func() time.Time) (*ClosedKlineSource, error) {
	d, err := regime.ParseKlineInterval(interval)
	if err != nil {
		return nil, fmt.Errorf("closed kline source %s: %w", symbol, err)
	}
	if now == nil {
		return nil, fmt.Errorf("closed kline source %s: now func is nil", symbol)
	}
	sorted := make([]*exchange.Candle, 0, len(bars))
	for _, b := range bars {
		if b != nil {
			sorted = append(sorted, b)
		}
	}
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Timestamp < sorted[j].Timestamp })
	return &ClosedKlineSource{symbol: symbol, interval: interval, intervalMs: d.Milliseconds(), bars: sorted, now: now}, nil
}

var _ regime.KlineSource = (*ClosedKlineSource)(nil)

// GetHistoricalKlines 返回 now() 前已收盤的最近 limit 根 K 線（副本，調用方可修改）
func (s *ClosedKlineSource) GetHistoricalKlines(ctx context.Context, symbol string, interval string, limit int) ([]*exchange.Candle, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if symbol != s.symbol {
		return nil, fmt.Errorf("closed kline source: symbol %q not served (have %q)", symbol, s.symbol)
	}
	if interval != s.interval {
		return nil, fmt.Errorf("closed kline source %s: interval %q not served (have %q)", symbol, interval, s.interval)
	}
	if limit <= 0 {
		return nil, nil
	}
	cutoff := s.now().UnixMilli()
	s.mu.RLock()
	defer s.mu.RUnlock()
	// 第一根「未收盤」K 線的下標：openTime + interval > cutoff
	end := sort.Search(len(s.bars), func(i int) bool { return s.bars[i].Timestamp+s.intervalMs > cutoff })
	start := end - limit
	if start < 0 {
		start = 0
	}
	out := make([]*exchange.Candle, 0, end-start)
	for _, b := range s.bars[start:end] {
		cp := *b
		cp.IsClosed = true
		out = append(out, &cp)
	}
	return out, nil
}
