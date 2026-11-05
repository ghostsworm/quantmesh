package replay

import (
	"context"
	"fmt"
	"math"

	"quantmesh/backtest"
	"quantmesh/exchange"
	"quantmesh/strategy/regime"
)

// validateTaskWindow 不裁切半根 K 線：即使只保留 end 之前的合成 tick，
// 該 tick 的價格和成交量仍可能已使用 end 之後的 OHLCV。
func validateTaskWindow(ctx context.Context, task *backtest.BacktestTask, candles []*exchange.Candle) (int64, error) {
	if task.Interval == "" || task.StartTime.IsZero() || task.EndTime.IsZero() || !task.EndTime.After(task.StartTime) {
		return 0, fmt.Errorf("explicit interval and non-empty [start, end) window are required")
	}
	d, err := regime.ParseKlineInterval(task.Interval)
	if err != nil {
		return 0, fmt.Errorf("task interval %q: %w", task.Interval, err)
	}
	intervalMs := d.Milliseconds()
	start, end := task.StartTime.UnixMilli(), task.EndTime.UnixMilli()
	for i, c := range candles {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		if c == nil {
			return 0, fmt.Errorf("nil candle at index %d", i)
		}
		if c.Timestamp%intervalMs != 0 {
			return 0, fmt.Errorf("candle at %d is not UTC-aligned to interval %s", c.Timestamp, task.Interval)
		}
		if c.Timestamp < start || c.Timestamp > math.MaxInt64-intervalMs || c.Timestamp+intervalMs > end {
			return 0, fmt.Errorf("candle at %d is not complete inside [%d, %d) with interval %s", c.Timestamp, start, end, task.Interval)
		}
		if i > 0 && c.Timestamp-candles[i-1].Timestamp != intervalMs {
			return 0, fmt.Errorf("candle cadence at index %d does not match interval %s", i, task.Interval)
		}
	}
	return intervalMs, nil
}

func validateTaskTicks(ctx context.Context, task *backtest.BacktestTask, ticks []Tick) error {
	start, end := task.StartTime.UnixMilli(), task.EndTime.UnixMilli()
	for i, tick := range ticks {
		if err := ctx.Err(); err != nil {
			return err
		}
		if tick.Timestamp < start || tick.Timestamp >= end {
			return fmt.Errorf("tick %d at %d lies outside [%d, %d)", i, tick.Timestamp, start, end)
		}
	}
	return nil
}
