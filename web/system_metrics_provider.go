package web

import (
	"fmt"
	"time"

	"quantmesh/monitor"
	"quantmesh/storage"
	"quantmesh/utils"
)

// SystemMetricsProviderImpl 系统監控數據提供者實現
type SystemMetricsProviderImpl struct {
	storageService *storage.StorageService
	watchdog       *monitor.Watchdog
}

// NewSystemMetricsProvider 創建系统監控數據提供者
func NewSystemMetricsProvider(storageService *storage.StorageService, watchdog *monitor.Watchdog) *SystemMetricsProviderImpl {
	return &SystemMetricsProviderImpl{
		storageService: storageService,
		watchdog:       watchdog,
	}
}

// GetCurrentMetrics 獲取當前系统状態
func (p *SystemMetricsProviderImpl) GetCurrentMetrics() (*SystemMetricsResponse, error) {
	// 优先從watchdog獲取最新數據（從缓存中）
	if p.watchdog != nil {
		latest := p.watchdog.GetLatestMetrics()
		if latest != nil {
			return &SystemMetricsResponse{
				Timestamp:     utils.ToUTC8(latest.Timestamp),
				CPUPercent:    latest.CPUPercent,
				MemoryMB:      latest.MemoryMB,
				MemoryPercent: latest.MemoryPercent,
				ProcessID:     latest.ProcessID,
			}, nil
		}
	}

	// 如果watchdog没有數據，實時采集一次
	metrics, err := monitor.CollectSystemMetrics()
	if err == nil && metrics != nil {
		return &SystemMetricsResponse{
			Timestamp:     utils.ToUTC8(metrics.Timestamp),
			CPUPercent:    metrics.CPUPercent,
			MemoryMB:      metrics.MemoryMB,
			MemoryPercent: metrics.MemoryPercent,
			ProcessID:     metrics.ProcessID,
		}, nil
	}

	// 如果實時采集失败，尝試從數據库獲取最新數據
	if p.storageService != nil {
		storage := p.storageService.GetStorage()
		if storage != nil {
			latest, err := storage.GetLatestSystemMetrics()
			if err == nil && latest != nil {
				return &SystemMetricsResponse{
					Timestamp:     utils.ToUTC8(latest.Timestamp),
					CPUPercent:    latest.CPUPercent,
					MemoryMB:      latest.MemoryMB,
					MemoryPercent: latest.MemoryPercent,
					ProcessID:     latest.ProcessID,
				}, nil
			}
		}
	}

	// 所有方法都失败，返回默认值（但这种情况应該很少发生）
	return &SystemMetricsResponse{
		Timestamp:     utils.ToUTC8(time.Now()),
		CPUPercent:    0,
		MemoryMB:      0,
		MemoryPercent: 0,
		ProcessID:     0,
	}, nil
}

// GetMetrics 獲取系统監控數據
func (p *SystemMetricsProviderImpl) GetMetrics(startTime, endTime time.Time, granularity string) ([]*SystemMetricsResponse, error) {
	var storageMetrics []*storage.SystemMetrics

	if p.storageService != nil {
		storageImpl := p.storageService.GetStorage()
		if storageImpl != nil {
			// 限制查詢時间範圍，防止返回過多數據導致記憶體问题
			maxDuration := 7 * 24 * time.Hour // 最多查詢7天
			actualDuration := endTime.Sub(startTime)
			if actualDuration > maxDuration {
				startTime = endTime.Add(-maxDuration)
			}

			metrics, err := storageImpl.QuerySystemMetrics(startTime, endTime)
			if err != nil {
				return nil, fmt.Errorf("查詢監控數據失败: %w", err)
			}

			// 限制返回的數據量，防止記憶體占用過大
			maxDataPoints := 10000 // 最多返回1万条數據
			if len(metrics) > maxDataPoints {
				step := len(metrics) / maxDataPoints
				sampledMetrics := make([]*storage.SystemMetrics, 0, maxDataPoints)
				for i := 0; i < len(metrics); i += step {
					if i < len(metrics) {
						sampledMetrics = append(sampledMetrics, metrics[i])
					}
				}
				lastIdx := len(metrics) - 1
				if len(sampledMetrics) > 0 && sampledMetrics[len(sampledMetrics)-1] != metrics[lastIdx] {
					sampledMetrics = append(sampledMetrics, metrics[lastIdx])
				}
				storageMetrics = sampledMetrics
			} else {
				storageMetrics = metrics
			}
		}
	}

	// 若無歷史數據，用當前指標作為單點回退，避免圖表空白
	if len(storageMetrics) == 0 {
		current, err := p.GetCurrentMetrics()
		if err == nil && current != nil {
			return []*SystemMetricsResponse{current}, nil
		}
		return []*SystemMetricsResponse{}, nil
	}

	metrics := make([]*SystemMetricsResponse, len(storageMetrics))
	for i, sm := range storageMetrics {
		metrics[i] = &SystemMetricsResponse{
			Timestamp:     utils.ToUTC8(sm.Timestamp),
			CPUPercent:    sm.CPUPercent,
			MemoryMB:      sm.MemoryMB,
			MemoryPercent: sm.MemoryPercent,
			ProcessID:     sm.ProcessID,
		}
	}

	return metrics, nil
}

// GetDailyMetrics 獲取每日彙總數據
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
		return nil, fmt.Errorf("查詢每日彙總數據失败: %w", err)
	}

	metrics := make([]*DailySystemMetricsResponse, len(dailyMetrics))
	for i, dm := range dailyMetrics {
		metrics[i] = &DailySystemMetricsResponse{
			Date:          utils.ToUTC8(dm.Date),
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
