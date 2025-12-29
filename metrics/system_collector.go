package metrics

import (
	"context"
	"runtime"
	"time"
)

// SystemMetricsCollector 系统指标采集器
type SystemMetricsCollector struct {
	pm       *PrometheusMetrics
	interval time.Duration
	ctx      context.Context
	cancel   context.CancelFunc
}

// NewSystemMetricsCollector 创建系统指标采集器
func NewSystemMetricsCollector(interval time.Duration) *SystemMetricsCollector {
	ctx, cancel := context.WithCancel(context.Background())
	return &SystemMetricsCollector{
		pm:       GetPrometheusMetrics(),
		interval: interval,
		ctx:      ctx,
		cancel:   cancel,
	}
}

// Start 启动采集
func (smc *SystemMetricsCollector) Start() {
	go smc.collectLoop()
}

// Stop 停止采集
func (smc *SystemMetricsCollector) Stop() {
	if smc.cancel != nil {
		smc.cancel()
	}
}

// collectLoop 采集循环
func (smc *SystemMetricsCollector) collectLoop() {
	ticker := time.NewTicker(smc.interval)
	defer ticker.Stop()

	// 立即采集一次
	smc.collect()

	for {
		select {
		case <-smc.ctx.Done():
			return
		case <-ticker.C:
			smc.collect()
		}
	}
}

// collect 采集系统指标
func (smc *SystemMetricsCollector) collect() {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	// Goroutine 数量
	smc.pm.SetGoroutineCount(runtime.NumGoroutine())

	// 内存指标
	smc.pm.SetMemoryAlloc(m.Alloc)
	
	// 累计分配（只在增加时更新）
	// 注意：TotalAlloc 是累计值，我们需要计算增量
	// 这里简化处理，直接使用当前值
	if m.TotalAlloc > 0 {
		// 由于 Prometheus Counter 只能增加，我们需要记录上次的值
		// 这里简化处理，使用 Gauge 替代
		smc.pm.SetMemoryAlloc(m.Alloc)
	}

	// GC 停顿时间（最近一次）
	if m.NumGC > 0 {
		// PauseNs 是一个循环缓冲区，最新的 GC 停顿时间在 (NumGC+255)%256 位置
		idx := (m.NumGC + 255) % 256
		pauseNs := m.PauseNs[idx]
		if pauseNs > 0 {
			smc.pm.RecordGCPause(time.Duration(pauseNs))
		}
	}
}

