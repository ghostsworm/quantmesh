package monitor

import (
	"time"

	"quantmesh/config"
)

// ThresholdChecker 阈值检查器
type ThresholdChecker struct {
	cfg *config.Config
}

// NewThresholdChecker 创建阈值检查器
func NewThresholdChecker(cfg *config.Config) *ThresholdChecker {
	return &ThresholdChecker{
		cfg: cfg,
	}
}

// CheckFixedThreshold 检查固定阈值
func (tc *ThresholdChecker) CheckFixedThreshold(metrics *SystemMetrics) bool {
	if !tc.cfg.Watchdog.Notifications.FixedThreshold.Enabled {
		return false
	}

	// 检查CPU阈值
	if metrics.CPUPercent >= tc.cfg.Watchdog.Notifications.FixedThreshold.CPUPercent {
		return true
	}

	// 检查内存阈值（如果配置）
	if tc.cfg.Watchdog.Notifications.FixedThreshold.MemoryMB > 0 {
		if metrics.MemoryMB >= tc.cfg.Watchdog.Notifications.FixedThreshold.MemoryMB {
			return true
		}
	}

	return false
}

// CheckRateThreshold 检查变化率阈值（CPU）
func (tc *ThresholdChecker) CheckRateThreshold(
	current *SystemMetrics,
	history []*SystemMetrics,
	windowMinutes int,
	thresholdPercent float64,
) bool {
	if !tc.cfg.Watchdog.Notifications.RateThreshold.Enabled {
		return false
	}

	if thresholdPercent <= 0 {
		return false
	}

	// 找到时间窗口内的最旧数据点
	windowStart := current.Timestamp.Add(-time.Duration(windowMinutes) * time.Minute)
	var oldest *SystemMetrics

	for _, m := range history {
		if m.Timestamp.After(windowStart) && m.Timestamp.Before(current.Timestamp) {
			if oldest == nil || m.Timestamp.Before(oldest.Timestamp) {
				oldest = m
			}
		}
	}

	if oldest == nil {
		return false
	}

	// 计算变化率
	change := current.CPUPercent - oldest.CPUPercent
	return change >= thresholdPercent
}

// CheckMemoryRateThreshold 检查内存变化率阈值
func (tc *ThresholdChecker) CheckMemoryRateThreshold(
	current *SystemMetrics,
	history []*SystemMetrics,
	windowMinutes int,
	thresholdMB float64,
) bool {
	if !tc.cfg.Watchdog.Notifications.RateThreshold.Enabled {
		return false
	}

	if thresholdMB <= 0 {
		return false
	}

	// 找到时间窗口内的最旧数据点
	windowStart := current.Timestamp.Add(-time.Duration(windowMinutes) * time.Minute)
	var oldest *SystemMetrics

	for _, m := range history {
		if m.Timestamp.After(windowStart) && m.Timestamp.Before(current.Timestamp) {
			if oldest == nil || m.Timestamp.Before(oldest.Timestamp) {
				oldest = m
			}
		}
	}

	if oldest == nil {
		return false
	}

	// 计算变化量（MB）
	change := current.MemoryMB - oldest.MemoryMB
	return change >= thresholdMB
}

