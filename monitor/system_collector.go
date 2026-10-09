package monitor

import (
	"fmt"
	"math"
	"os"
	"runtime"
	"time"

	"github.com/shirou/gopsutil/v3/mem"
	"github.com/shirou/gopsutil/v3/process"
)

// SystemMetrics 系统監控指標
type SystemMetrics struct {
	Timestamp     time.Time `json:"timestamp"`
	CPUPercent    float64   `json:"cpu_percent"` // process lifetime-average share of system logical CPU capacity, 0–100
	MemoryMB      float64   `json:"memory_mb"`
	MemoryPercent float64   `json:"memory_percent"` // 系统記憶體占用百分比
	ProcessID     int       `json:"process_id"`
	// Same-read process CPU units used by existing watchdog thresholds. Never
	// infer this value from historical mixed-source percentages.
	processCPUPercent *float64
}

// CollectSystemMetrics 采集系统资源指標
func CollectSystemMetrics() (*SystemMetrics, error) {
	pid := os.Getpid()
	p, err := process.NewProcess(int32(pid))
	if err != nil {
		return nil, fmt.Errorf("獲取進程失败: %w", err)
	}

	// gopsutil CPUPercent is lifetime-average process CPU time / wall time:
	// multiple logical CPUs can each contribute 100%. Normalize to capacity,
	// keep genuine zero, and never substitute a different (host) source.
	cpuSample, err := readProcessCPUSample(p.CPUPercent, runtime.NumCPU())
	if err != nil {
		return nil, err
	}

	// 采集記憶體占用（RSS - Resident Set Size，實際物理記憶體）
	memInfo, err := p.MemoryInfo()
	if err != nil {
		return nil, fmt.Errorf("獲取記憶體信息失败: %w", err)
	}

	memoryMB := float64(memInfo.RSS) / 1024 / 1024

	// 獲取系统總記憶體，计算記憶體占用百分比
	memStat, err := mem.VirtualMemory()
	if err != nil {
		// 如果獲取失败，記憶體百分比設為0
		memStat = nil
	}

	var memoryPercent float64
	if memStat != nil && memStat.Total > 0 {
		memoryPercent = (float64(memInfo.RSS) / float64(memStat.Total)) * 100
	}

	return &SystemMetrics{
		Timestamp:         time.Now(),
		CPUPercent:        cpuSample.CPUPercent,
		MemoryMB:          memoryMB,
		MemoryPercent:     memoryPercent,
		ProcessID:         pid,
		processCPUPercent: cpuSample.processCPUPercent,
	}, nil
}

func readProcessCPUCapacityPercent(read func() (float64, error), logicalCPUs int) (float64, error) {
	if read == nil || logicalCPUs < 1 {
		return 0, fmt.Errorf("process CPU capacity requires a reader and positive logical CPU count")
	}
	percent, err := read()
	if err != nil {
		return 0, fmt.Errorf("read process lifetime-average CPU usage: %w", err)
	}
	if math.IsNaN(percent) || math.IsInf(percent, 0) || percent < 0 {
		return 0, fmt.Errorf("process CPU usage is not a finite nonnegative percentage")
	}
	capacityPercent := percent / float64(logicalCPUs)
	if capacityPercent > 100 {
		return 0, fmt.Errorf("process CPU usage exceeds logical CPU capacity: %.6f%% across %d CPUs", percent, logicalCPUs)
	}
	return capacityPercent, nil
}

// GetGoRuntimeStats 獲取Go运行時统计信息（用於調試）
func GetGoRuntimeStats() map[string]interface{} {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	return map[string]interface{}{
		"goroutines":      runtime.NumGoroutine(),
		"alloc_mb":        float64(m.Alloc) / 1024 / 1024,
		"total_alloc_mb":  float64(m.TotalAlloc) / 1024 / 1024,
		"sys_mb":          float64(m.Sys) / 1024 / 1024,
		"num_gc":          m.NumGC,
		"gc_cpu_fraction": m.GCCPUFraction,
	}
}
