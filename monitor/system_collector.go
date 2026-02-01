package monitor

import (
	"fmt"
	"os"
	"runtime"
	"time"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/mem"
	"github.com/shirou/gopsutil/v3/process"
)

// SystemMetrics 系统監控指標
type SystemMetrics struct {
	Timestamp     time.Time `json:"timestamp"`
	CPUPercent    float64   `json:"cpu_percent"`
	MemoryMB      float64   `json:"memory_mb"`
	MemoryPercent float64   `json:"memory_percent"` // 系统記憶體占用百分比
	ProcessID     int       `json:"process_id"`
}

// CollectSystemMetrics 采集系统资源指標
func CollectSystemMetrics() (*SystemMetrics, error) {
	pid := os.Getpid()
	p, err := process.NewProcess(int32(pid))
	if err != nil {
		return nil, fmt.Errorf("獲取進程失败: %w", err)
	}

	// 采集CPU占用率
	// 注意：CPUPercent() 第一次調用可能返回0，因為它需要時间间隔来计算
	// 如果返回0或錯误，使用系统CPU使用率作為备用
	cpuPercent, err := p.CPUPercent()
	if err != nil || cpuPercent == 0 {
		// 如果獲取失败或返回0，尝試使用系统CPU使用率
		systemCPU, err2 := getSystemCPUPercent()
		if err2 == nil && systemCPU > 0 {
			cpuPercent = systemCPU
		} else if err != nil {
			// 如果進程CPU獲取失败，且系统CPU也失败，返回錯误
			return nil, fmt.Errorf("獲取CPU占用率失败: %w", err)
		}
		// 如果進程CPU返回0但系统CPU也失败，继续使用0（可能是進程刚啟动）
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
		Timestamp:     time.Now(),
		CPUPercent:    cpuPercent,
		MemoryMB:      memoryMB,
		MemoryPercent: memoryPercent,
		ProcessID:     pid,
	}, nil
}

// getSystemCPUPercent 獲取系统CPU使用率（备用方法）
func getSystemCPUPercent() (float64, error) {
	percentages, err := cpu.Percent(time.Second, false)
	if err != nil {
		return 0, err
	}

	if len(percentages) == 0 {
		return 0, fmt.Errorf("無法獲取CPU使用率")
	}

	return percentages[0], nil
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
