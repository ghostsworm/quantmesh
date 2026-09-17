package storage

import (
	"encoding/json"
	"fmt"
	"time"

	"quantmesh/utils"
)

// SaveSystemMetrics 保存系统監控细粒度數據
func (s *SQLStorage) SaveSystemMetrics(metrics *SystemMetrics) error {
	// 轉换為UTC時间存儲
	timestamp := utils.ToUTC(metrics.Timestamp)
	var memoryPercent interface{}
	if metrics.MemoryPercent > 0 {
		memoryPercent = metrics.MemoryPercent
	}

	_, err := s.db.Exec(`
		INSERT INTO system_metrics
		(timestamp, cpu_percent, memory_mb, memory_percent, process_id)
		VALUES (?, ?, ?, ?, ?)
	`, timestamp, metrics.CPUPercent, metrics.MemoryMB, memoryPercent, metrics.ProcessID)
	return err
}

// SaveDailySystemMetrics 保存系统監控每日彙總數據
func (s *SQLStorage) SaveDailySystemMetrics(metrics *DailySystemMetrics) error {
	// 轉换為UTC時间存儲
	date := utils.ToUTC(metrics.Date)
	if s.dbType == "mysql" {
		_, err := s.db.Exec(`
			INSERT INTO daily_system_metrics
			(date, avg_cpu_percent, max_cpu_percent, min_cpu_percent,
			 avg_memory_mb, max_memory_mb, min_memory_mb, sample_count)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)
			ON DUPLICATE KEY UPDATE
				avg_cpu_percent = VALUES(avg_cpu_percent),
				max_cpu_percent = VALUES(max_cpu_percent),
				min_cpu_percent = VALUES(min_cpu_percent),
				avg_memory_mb = VALUES(avg_memory_mb),
				max_memory_mb = VALUES(max_memory_mb),
				min_memory_mb = VALUES(min_memory_mb),
				sample_count = VALUES(sample_count)
		`, date, metrics.AvgCPUPercent, metrics.MaxCPUPercent, metrics.MinCPUPercent,
			metrics.AvgMemoryMB, metrics.MaxMemoryMB, metrics.MinMemoryMB, metrics.SampleCount)
		return err
	}
	_, err := s.db.Exec(`
		INSERT OR REPLACE INTO daily_system_metrics
		(date, avg_cpu_percent, max_cpu_percent, min_cpu_percent,
		 avg_memory_mb, max_memory_mb, min_memory_mb, sample_count)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, date, metrics.AvgCPUPercent, metrics.MaxCPUPercent, metrics.MinCPUPercent,
		metrics.AvgMemoryMB, metrics.MaxMemoryMB, metrics.MinMemoryMB, metrics.SampleCount)
	return err
}

// SaveEvent 保存事件
func (s *SQLStorage) SaveEvent(eventType string, data map[string]interface{}) error {
	// 检查是否是系统監控事件
	if eventType == "system_metrics" {
		return s.saveSystemMetricsFromMap(data)
	}

	// 將 data 序列化為 JSON
	jsonData, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("序列化事件數據失败: %w", err)
	}

	_, err = s.db.Exec(`
		INSERT INTO events (event_type, data, created_at)
		VALUES (?, ?, ?)
	`, eventType, string(jsonData), utils.NowUTC())
	return err
}

// saveSystemMetricsFromMap 從 map 保存系统監控數據
func (s *SQLStorage) saveSystemMetricsFromMap(data map[string]interface{}) error {
	metrics := &SystemMetrics{}

	if timestamp, ok := data["timestamp"].(time.Time); ok {
		metrics.Timestamp = utils.ToUTC(timestamp)
	} else if timestampStr, ok := data["timestamp"].(string); ok {
		var err error
		parsedTime, err := time.Parse(time.RFC3339, timestampStr)
		if err != nil {
			metrics.Timestamp = utils.NowUTC()
		} else {
			metrics.Timestamp = utils.ToUTC(parsedTime)
		}
	} else {
		metrics.Timestamp = utils.NowUTC()
	}

	if cpuPercent, ok := data["cpu_percent"].(float64); ok {
		metrics.CPUPercent = cpuPercent
	}
	if memoryMB, ok := data["memory_mb"].(float64); ok {
		metrics.MemoryMB = memoryMB
	}
	if memoryPercent, ok := data["memory_percent"].(float64); ok {
		metrics.MemoryPercent = memoryPercent
	}
	if processID, ok := data["process_id"].(int); ok {
		metrics.ProcessID = processID
	} else if processID, ok := data["process_id"].(float64); ok {
		metrics.ProcessID = int(processID)
	}

	return s.SaveSystemMetrics(metrics)
}
