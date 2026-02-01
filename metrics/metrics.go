package metrics

import (
	"sync"
	"time"
)

// Metrics 性能指標
type Metrics struct {
	OrderExecutionTime time.Duration
	OrderSuccessRate   float64
	AveragePnL         float64
	WinRate            float64
	MaxDrawdown        float64
	TotalVolume        float64
	LastUpdate         time.Time
	mu                 sync.RWMutex
}

// MetricsCollector 指標收集器
type MetricsCollector struct {
	metrics *Metrics
}

// NewMetricsCollector 創建指標收集器
func NewMetricsCollector() *MetricsCollector {
	return &MetricsCollector{
		metrics: &Metrics{
			LastUpdate: time.Now(),
		},
	}
}

// RecordOrderExecution 記錄訂單執行時间
func (mc *MetricsCollector) RecordOrderExecution(duration time.Duration) {
	mc.metrics.mu.Lock()
	defer mc.metrics.mu.Unlock()
	mc.metrics.OrderExecutionTime = duration
	mc.metrics.LastUpdate = time.Now()
}

// RecordOrderResult 記錄订單結果
func (mc *MetricsCollector) RecordOrderResult(success bool) {
	mc.metrics.mu.Lock()
	defer mc.metrics.mu.Unlock()
	// TODO: 實現成功率计算
	mc.metrics.LastUpdate = time.Now()
}

// RecordPnL 記錄盈亏
func (mc *MetricsCollector) RecordPnL(pnl float64) {
	mc.metrics.mu.Lock()
	defer mc.metrics.mu.Unlock()
	mc.metrics.AveragePnL = pnl
	mc.metrics.LastUpdate = time.Now()
}

// GetMetrics 獲取指標
func (mc *MetricsCollector) GetMetrics() *Metrics {
	mc.metrics.mu.RLock()
	defer mc.metrics.mu.RUnlock()
	return mc.metrics
}
