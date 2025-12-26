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

// SystemMetrics 系统监控指标
type SystemMetrics struct {
	Timestamp     time.Time `json:"timestamp"`
	CPUPercent    float64   `json:"cpu_percent"`
	MemoryMB      float64   `json:"memory_mb"`
	MemoryPercent float64   `json:"memory_percent"` // 系统内存占用百分比
	ProcessID     int       `json:"process_id"`
}

// CollectSystemMetrics 采集系统资源指标
func CollectSystemMetrics() (*SystemMetrics, error) {
	pid := os.Getpid()
	p, err := process.NewProcess(int32(pid))
	if err != nil {
		return nil, fmt.Errorf("获取进程失败: %w", err)
	}

	// 采集CPU占用率
	cpuPercent, err := p.CPUPercent()
	if err != nil {
		// 如果获取失败，尝试使用系统CPU使用率
		cpuPercent, err = getSystemCPUPercent()
		if err != nil {
			return nil, fmt.Errorf("获取CPU占用率失败: %w", err)
		}
	}

	// 采集内存占用（RSS - Resident Set Size，实际物理内存）
	memInfo, err := p.MemoryInfo()
	if err != nil {
		return nil, fmt.Errorf("获取内存信息失败: %w", err)
	}

	memoryMB := float64(memInfo.RSS) / 1024 / 1024

	// 获取系统总内存，计算内存占用百分比
	memStat, err := mem.VirtualMemory()
	if err != nil {
		// 如果获取失败，内存百分比设为0
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

// getSystemCPUPercent 获取系统CPU使用率（备用方法）
func getSystemCPUPercent() (float64, error) {
	percentages, err := cpu.Percent(time.Second, false)
	if err != nil {
		return 0, err
	}

	if len(percentages) == 0 {
		return 0, fmt.Errorf("无法获取CPU使用率")
	}

	return percentages[0], nil
}

// GetGoRuntimeStats 获取Go运行时统计信息（用于调试）
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

