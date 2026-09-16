package strategy

import (
	"math"
	"sync"
	"testing"
	"time"

	"quantmesh/config"
)

const (
	trendDeadlockTimeout   = 10 * time.Second
	trendConcurrentWorkers = 4
	trendConcurrentIters   = 2000
	emaTolerance           = 1e-9
)

// TestTrendDetector_DetectTrendConcurrentWithAddPrice 验证 E3：DetectTrend 与 addPrice 并发不死锁。
func TestTrendDetector_DetectTrendConcurrentWithAddPrice(t *testing.T) {
	for _, method := range []string{"ma", "ema"} {
		t.Run(method, func(t *testing.T) {
			cfg := &config.Config{}
			cfg.Trading.SmartPosition.TrendDetection.ShortPeriod = 3
			cfg.Trading.SmartPosition.TrendDetection.LongPeriod = 5
			cfg.Trading.SmartPosition.TrendDetection.Method = method
			td := NewTrendDetector(cfg, nil)

			done := make(chan struct{})
			go func() {
				defer close(done)
				var wg sync.WaitGroup
				for w := 0; w < trendConcurrentWorkers; w++ {
					wg.Add(2)
					go func(seed int) {
						defer wg.Done()
						for i := 0; i < trendConcurrentIters; i++ {
							td.addPrice(100 + float64((seed+i)%7))
						}
					}(w)
					go func() {
						defer wg.Done()
						for i := 0; i < trendConcurrentIters; i++ {
							_ = td.DetectTrend()
							_ = td.GetCurrentTrend()
						}
					}()
				}
				wg.Wait()
			}()

			select {
			case <-done:
			case <-time.After(trendDeadlockTimeout):
				t.Fatalf("DetectTrend/addPrice 并发执行超过 %v，疑似死锁", trendDeadlockTimeout)
			}
		})
	}
}

func TestExponentialMovingAverage(t *testing.T) {
	tests := []struct {
		name   string
		prices []float64
		period int
		want   float64
	}{
		{name: "数据不足", prices: []float64{1, 2}, period: 3, want: 0},
		{name: "非法周期", prices: []float64{1, 2, 3}, period: 0, want: 0},
		{name: "恰好一个周期等于SMA", prices: []float64{1, 2, 3}, period: 3, want: 2},
		// k=0.5, seed=2; 4 -> 3; 5 -> 4
		{name: "周期3迭代两步", prices: []float64{1, 2, 3, 4, 5}, period: 3, want: 4},
		// k=2/3, seed=15; 30 -> 25; 0 -> 25/3
		{name: "周期2含回落", prices: []float64{10, 20, 30, 0}, period: 2, want: 25.0 / 3.0},
		{name: "常数序列", prices: []float64{7, 7, 7, 7, 7, 7}, period: 4, want: 7},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := exponentialMovingAverage(tt.prices, tt.period)
			if math.Abs(got-tt.want) > emaTolerance {
				t.Fatalf("exponentialMovingAverage(%v, %d) = %v, want %v", tt.prices, tt.period, got, tt.want)
			}
		})
	}
}

// TestExponentialMovingAverage_DiffersFromSMA 验证 S6：EMA 不再退化为 SMA。
func TestExponentialMovingAverage_DiffersFromSMA(t *testing.T) {
	// 注意：线性序列上 EMA 稳态恰好等于 SMA，因此使用非线性序列。
	prices := []float64{10, 10, 10, 10, 10, 10, 10, 40}
	const period = 3
	sma := simpleMovingAverage(prices, period)
	ema := exponentialMovingAverage(prices, period)
	if math.Abs(sma-20) > emaTolerance {
		t.Fatalf("SMA = %v, want 20", sma)
	}
	// seed=10, k=0.5：最后一步 40*0.5 + 10*0.5 = 25
	if math.Abs(ema-25) > emaTolerance {
		t.Fatalf("EMA = %v, want 25", ema)
	}
	if math.Abs(ema-sma) < emaTolerance {
		t.Fatalf("EMA 与 SMA 相同 (%v)，EMA 仍是 SMA", ema)
	}
}
