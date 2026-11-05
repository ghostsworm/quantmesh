package replay

import (
	"context"
	"errors"
	"testing"
	"time"

	"quantmesh/backtest"
	"quantmesh/exchange"
	"quantmesh/position"
)

func boundedTask(interval string, start time.Time, duration time.Duration) *backtest.BacktestTask {
	task := replayFeatureTask("bounded", nil)
	task.Interval, task.StartTime, task.EndTime = interval, start, start.Add(duration)
	return task
}

func boundedCandle(start time.Time) *exchange.Candle {
	return &exchange.Candle{Timestamp: start.UnixMilli(), Open: 2000, High: 2021, Low: 1979, Close: 2005, Volume: 50}
}

func TestRunGridTaskContextCompleteCandleBoundary(t *testing.T) {
	start := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name   string
		mutate func(*backtest.BacktestTask, *[]*exchange.Candle)
	}{
		{"missing interval", func(task *backtest.BacktestTask, _ *[]*exchange.Candle) { task.Interval = "" }},
		{"missing start", func(task *backtest.BacktestTask, _ *[]*exchange.Candle) { task.StartTime = time.Time{} }},
		{"empty window", func(task *backtest.BacktestTask, _ *[]*exchange.Candle) { task.EndTime = task.StartTime }},
		{"partial last candle", func(task *backtest.BacktestTask, _ *[]*exchange.Candle) { task.EndTime = start.Add(30 * time.Minute) }},
		{"candle at exclusive end", func(_ *backtest.BacktestTask, candles *[]*exchange.Candle) {
			*candles = append(*candles, boundedCandle(start.Add(time.Hour)))
		}},
		{"warmup mixed into trading", func(_ *backtest.BacktestTask, candles *[]*exchange.Candle) {
			*candles = append([]*exchange.Candle{boundedCandle(start.Add(-time.Hour))}, *candles...)
		}},
		{"nil candle", func(_ *backtest.BacktestTask, candles *[]*exchange.Candle) { *candles = append(*candles, nil) }},
		{"offset candle", func(task *backtest.BacktestTask, candles *[]*exchange.Candle) {
			task.EndTime = start.Add(2 * time.Hour)
			(*candles)[0].Timestamp += time.Minute.Milliseconds()
		}},
		{"wrong cadence", func(task *backtest.BacktestTask, candles *[]*exchange.Candle) {
			task.EndTime = start.Add(3 * time.Hour)
			*candles = append(*candles, boundedCandle(start.Add(2*time.Hour)))
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			task := boundedTask("1h", start, time.Hour)
			candles := []*exchange.Candle{boundedCandle(start)}
			tc.mutate(task, &candles)
			result, extra, err := RunGridTaskContext(context.Background(), task, candles)
			if err == nil || result != nil || extra != nil {
				t.Fatalf("invalid window returned result=%v extra=%v error=%v", result, extra, err)
			}
		})
	}

	// 單根 1h K 線不能回退為推斷不到周期時的 1m；最後點為 00:45。
	task := boundedTask("1h", start, time.Hour)
	result, extra, err := RunGridTaskContext(context.Background(), task, []*exchange.Candle{boundedCandle(start)})
	if err != nil {
		t.Fatal(err)
	}
	metrics := extra.(Metrics)
	wantEnd := start.Add(45 * time.Minute)
	if !result.EndTime.Equal(wantEnd) || metrics.EndTime != wantEnd.UnixMilli() || metrics.TicksProcessed != 4 {
		t.Fatalf("single 1h candle interval lost: end=%s ticks=%d", result.EndTime, metrics.TicksProcessed)
	}
	if metrics.EndTime >= task.EndTime.UnixMilli() {
		t.Fatal("replay crossed exclusive end")
	}
	// 同一單根粗周期輸入也不能被誤認為 1m regime 的預熱來源。
	task.Params["regime_filter"] = map[string]interface{}{"enabled": true, "kline_interval": "1m"}
	if _, _, err := RunGridTaskContext(context.Background(), task, []*exchange.Candle{boundedCandle(start)}); err == nil {
		t.Fatal("single 1h candle must not feed a 1m regime detector")
	}
}

func TestReplayContextCancellationNeverReturnsPartialSuccess(t *testing.T) {
	for _, stage := range []string{"before run", "setup", "first tick", "last tick"} {
		t.Run(stage, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			cfg := testConfig(MatchingConfig{})
			ticks := ticksFromPrices(touchAndCrossPrices, 10)
			if stage == "before run" {
				cancel()
			}
			cfg.Setup = func(*position.SuperPositionManager, position.Clock) error {
				if stage == "setup" {
					cancel()
				}
				return nil
			}
			cfg.OnTick = func(now time.Time, _ float64) {
				if stage == "first tick" || stage == "last tick" && now.UnixMilli() == ticks[len(ticks)-1].Timestamp {
					cancel()
				}
			}
			engine := NewEngine(cfg)
			result, err := engine.RunContext(ctx, ticks)
			if !errors.Is(err, context.Canceled) || result != nil {
				t.Fatalf("cancellation must discard partial result, result=%v error=%v", result, err)
			}
			if (stage == "setup" || stage == "first tick") && engine.adjustCalls != 0 {
				t.Fatal("strategy adjusted after cancellation")
			}
		})
	}
	start := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, extra, err := RunGridTaskContext(ctx, boundedTask("1h", start, time.Hour), []*exchange.Candle{boundedCandle(start)})
	if !errors.Is(err, context.Canceled) || result != nil || extra != nil {
		t.Fatalf("task cancellation returned result=%v extra=%v error=%v", result, extra, err)
	}
	deadline, stop := context.WithDeadline(context.Background(), time.Unix(0, 0))
	defer stop()
	if result, err := NewEngine(testConfig(MatchingConfig{})).RunContext(deadline, ticksFromPrices(touchAndCrossPrices, 10)); result != nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expired deadline returned result=%v error=%v", result, err)
	}
}

func TestValidateTaskTicksExclusiveBoundary(t *testing.T) {
	start := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	task := boundedTask("1m", start, time.Minute)
	for _, ts := range []int64{start.Add(-time.Millisecond).UnixMilli(), task.EndTime.UnixMilli()} {
		if err := validateTaskTicks(context.Background(), task, []Tick{{Timestamp: ts, Price: 2000}}); err == nil {
			t.Fatalf("accepted tick outside half-open window: %d", ts)
		}
	}
}
