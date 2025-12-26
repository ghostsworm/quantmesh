package web

import (
	"fmt"
	"time"

	"quantmesh/monitor"
	"quantmesh/storage"
)

// SystemMetricsProviderImpl 系统监控数据提供者实现
type SystemMetricsProviderImpl struct {
	storageService *storage.StorageService
	watchdog       *monitor.Watchdog
}

// NewSystemMetricsProvider 创建系统监控数据提供者
func NewSystemMetricsProvider(storageService *storage.StorageService, watchdog *monitor.Watchdog) *SystemMetricsProviderImpl {
	return &SystemMetricsProviderImpl{
		storageService: storageService,
		watchdog:       watchdog,
	}
}

// GetCurrentMetrics 获取当前系统状态
func (p *SystemMetricsProviderImpl) GetCurrentMetrics() (*SystemMetricsResponse, error) {
	// 尝试从watchdog获取最新数据
	if p.watchdog != nil {
		// TODO: 在watchdog中添加GetLatestMetrics方法
		// 暂时从数据库获取最新数据
	}

	if p.storageService == nil {
		return &SystemMetricsResponse{
			Timestamp:     time.Now(),
			CPUPercent:    0,
			MemoryMB:      0,
			MemoryPercent: 0,
			ProcessID:     0,
		}, nil
	}

	storage := p.storageService.GetStorage()
	if storage == nil {
		return &SystemMetricsResponse{
			Timestamp:     time.Now(),
			CPUPercent:    0,
			MemoryMB:      0,
			MemoryPercent: 0,
			ProcessID:     0,
		}, nil
	}

	latest, err := storage.GetLatestSystemMetrics()
	if err != nil {
		return &SystemMetricsResponse{
			Timestamp:     time.Now(),
			CPUPercent:    0,
			MemoryMB:      0,
			MemoryPercent: 0,
			ProcessID:     0,
		}, nil
	}

	if latest == nil {
		return &SystemMetricsResponse{
			Timestamp:     time.Now(),
			CPUPercent:    0,
			MemoryMB:      0,
			MemoryPercent: 0,
			ProcessID:     0,
		}, nil
	}

	return &SystemMetricsResponse{
		Timestamp:     latest.Timestamp,
		CPUPercent:    latest.CPUPercent,
		MemoryMB:      latest.MemoryMB,
		MemoryPercent: latest.MemoryPercent,
		ProcessID:     latest.ProcessID,
	}, nil
}

// GetMetrics 获取系统监控数据
func (p *SystemMetricsProviderImpl) GetMetrics(startTime, endTime time.Time, granularity string) ([]*SystemMetricsResponse, error) {
	if p.storageService == nil {
		return []*SystemMetricsResponse{}, nil
	}

	storage := p.storageService.GetStorage()
	if storage == nil {
		return []*SystemMetricsResponse{}, nil
	}

	storageMetrics, err := storage.QuerySystemMetrics(startTime, endTime)
	if err != nil {
		return nil, fmt.Errorf("查询监控数据失败: %w", err)
	}

	metrics := make([]*SystemMetricsResponse, len(storageMetrics))
	for i, sm := range storageMetrics {
		metrics[i] = &SystemMetricsResponse{
			Timestamp:     sm.Timestamp,
			CPUPercent:    sm.CPUPercent,
			MemoryMB:      sm.MemoryMB,
			MemoryPercent: sm.MemoryPercent,
			ProcessID:     sm.ProcessID,
		}
	}

	return metrics, nil
}

// GetDailyMetrics 获取每日汇总数据
func (p *SystemMetricsProviderImpl) GetDailyMetrics(days int) ([]*DailySystemMetricsResponse, error) {
	if p.storageService == nil {
		return []*DailySystemMetricsResponse{}, nil
	}

	storage := p.storageService.GetStorage()
	if storage == nil {
		return []*DailySystemMetricsResponse{}, nil
	}

	dailyMetrics, err := storage.QueryDailySystemMetrics(days)
	if err != nil {
		return nil, fmt.Errorf("查询每日汇总数据失败: %w", err)
	}

	metrics := make([]*DailySystemMetricsResponse, len(dailyMetrics))
	for i, dm := range dailyMetrics {
		metrics[i] = &DailySystemMetricsResponse{
			Date:          dm.Date,
			AvgCPUPercent: dm.AvgCPUPercent,
			MaxCPUPercent: dm.MaxCPUPercent,
			MinCPUPercent: dm.MinCPUPercent,
			AvgMemoryMB:   dm.AvgMemoryMB,
			MaxMemoryMB:   dm.MaxMemoryMB,
			MinMemoryMB:   dm.MinMemoryMB,
			SampleCount:   dm.SampleCount,
		}
	}

	return metrics, nil
}

