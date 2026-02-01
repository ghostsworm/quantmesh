package monitor

import (
	"context"
	"runtime"
	"sync"
	"time"

	"quantmesh/config"
	"quantmesh/logger"
)

// MemoryManager 内存管理器
type MemoryManager struct {
	cfg             *config.Config
	ctx             context.Context
	cancel          context.CancelFunc
	mu              sync.RWMutex
	lastGCStats     runtime.MemStats
	gcInterval      time.Duration
	cleanupInterval time.Duration
}

// NewMemoryManager 创建内存管理器
func NewMemoryManager(cfg *config.Config, ctx context.Context) *MemoryManager {
	ctx, cancel := context.WithCancel(ctx)

	gcInterval := 5 * time.Minute      // 每5分钟触发一次 GC
	cleanupInterval := 30 * time.Minute // 每30分钟执行一次清理

	return &MemoryManager{
		cfg:             cfg,
		ctx:             ctx,
		cancel:          cancel,
		gcInterval:      gcInterval,
		cleanupInterval: cleanupInterval,
	}
}

// Start 启动内存管理
func (mm *MemoryManager) Start() {
	logger.Info("✅ 内存管理器已启动 (GC间隔: %v, 清理间隔: %v)", mm.gcInterval, mm.cleanupInterval)

	// 启动定期 GC
	go mm.gcLoop()

	// 启动内存监控
	go mm.monitorLoop()
}

// Stop 停止内存管理
func (mm *MemoryManager) Stop() {
	if mm.cancel != nil {
		mm.cancel()
	}
	logger.Info("✅ 内存管理器已停止")
}

// gcLoop 定期触发 GC
func (mm *MemoryManager) gcLoop() {
	ticker := time.NewTicker(mm.gcInterval)
	defer ticker.Stop()

	// 立即执行一次
	mm.forceGC()

	for {
		select {
		case <-mm.ctx.Done():
			return
		case <-ticker.C:
			mm.forceGC()
		}
	}
}

// forceGC 强制触发 GC
func (mm *MemoryManager) forceGC() {
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)

	// 触发 GC
	runtime.GC()

	// 等待 GC 完成
	time.Sleep(100 * time.Millisecond)
	runtime.ReadMemStats(&after)

	// 计算释放的内存
	freedMB := float64(before.Alloc-after.Alloc) / 1024 / 1024
	allocMB := float64(after.Alloc) / 1024 / 1024
	sysMB := float64(after.Sys) / 1024 / 1024
	
	// 计算 GC 停顿时间
	var totalPause time.Duration
	pauseCount := 0
	for i := 0; i < 256 && i < int(after.NumGC); i++ {
		idx := (uint64(after.NumGC) + uint64(255-i)) % 256
		if after.PauseNs[idx] > 0 {
			totalPause += time.Duration(after.PauseNs[idx])
			pauseCount++
		}
	}
	avgPause := time.Duration(0)
	if pauseCount > 0 {
		avgPause = totalPause / time.Duration(pauseCount)
	}

	logger.Debug("🧹 [内存管理] GC完成: 释放=%.2f MB, 当前分配=%.2f MB, 系统内存=%.2f MB, Goroutines=%d, GC次数=%d, 平均停顿=%v",
		freedMB, allocMB, sysMB, runtime.NumGoroutine(), after.NumGC, avgPause)

	mm.mu.Lock()
	mm.lastGCStats = after
	mm.mu.Unlock()
}

// monitorLoop 内存监控循环
func (mm *MemoryManager) monitorLoop() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-mm.ctx.Done():
			return
		case <-ticker.C:
			mm.checkMemoryUsage()
		}
	}
}

// checkMemoryUsage 检查内存使用情况
func (mm *MemoryManager) checkMemoryUsage() {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	allocMB := float64(m.Alloc) / 1024 / 1024
	sysMB := float64(m.Sys) / 1024 / 1024
	numGoroutines := runtime.NumGoroutine()

	// 检查内存使用是否过高
	highMemoryThreshold := 500.0 // 500 MB
	if allocMB > highMemoryThreshold {
		logger.Warn("⚠️ [内存监控] 内存使用较高: %.2f MB (分配), %.2f MB (系统), Goroutines=%d",
			allocMB, sysMB, numGoroutines)
	}

	// 检查 Goroutine 数量是否过多
	highGoroutineThreshold := 100
	if numGoroutines > highGoroutineThreshold {
		logger.Warn("⚠️ [内存监控] Goroutine 数量较多: %d", numGoroutines)
	}

	// 如果内存使用持续增长，触发 GC
	mm.mu.RLock()
	lastAlloc := mm.lastGCStats.Alloc
	mm.mu.RUnlock()

	if lastAlloc > 0 && m.Alloc > lastAlloc*2 {
		logger.Warn("⚠️ [内存监控] 检测到内存持续增长，触发强制 GC")
		mm.forceGC()
	}
}

// GetMemoryStats 获取内存统计信息
func (mm *MemoryManager) GetMemoryStats() map[string]interface{} {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	return map[string]interface{}{
		"alloc_mb":       float64(m.Alloc) / 1024 / 1024,
		"sys_mb":         float64(m.Sys) / 1024 / 1024,
		"num_gc":         m.NumGC,
		"goroutines":    runtime.NumGoroutine(),
		"heap_alloc_mb":  float64(m.HeapAlloc) / 1024 / 1024,
		"heap_sys_mb":    float64(m.HeapSys) / 1024 / 1024,
		"heap_idle_mb":   float64(m.HeapIdle) / 1024 / 1024,
		"heap_inuse_mb":  float64(m.HeapInuse) / 1024 / 1024,
		"next_gc_mb":     float64(m.NextGC) / 1024 / 1024,
		"gc_cpu_fraction": m.GCCPUFraction,
	}
}
