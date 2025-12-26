package monitor

import (
	"context"
	"fmt"
	"sync"
	"time"

	"quantmesh/config"
	"quantmesh/logger"
	"quantmesh/notify"
	"quantmesh/storage"
)

// Watchdog 系统资源监控看门狗
type Watchdog struct {
	cfg            *config.Config
	storageService *storage.StorageService
	notifier       *notify.NotificationService
	sampleInterval time.Duration
	cleanupInterval time.Duration
	ctx            context.Context
	cancel         context.CancelFunc
	mu             sync.RWMutex

	// 通知冷却机制
	lastNotificationTime map[string]time.Time
	cooldownDuration     time.Duration

	// 历史数据缓存（用于变化率检查）
	historyCache []*SystemMetrics
	maxHistory   int
}

// NewWatchdog 创建看门狗实例
func NewWatchdog(cfg *config.Config, storageService *storage.StorageService, notifier *notify.NotificationService) *Watchdog {
	ctx, cancel := context.WithCancel(context.Background())

	sampleInterval := time.Duration(cfg.Watchdog.Sampling.Interval) * time.Second
	if sampleInterval <= 0 {
		sampleInterval = 120 * time.Second // 默认2分钟
	}

	cleanupInterval := 1 * time.Hour // 每小时清理一次
	cooldownDuration := time.Duration(cfg.Watchdog.Notifications.CooldownMinutes) * time.Minute
	if cooldownDuration <= 0 {
		cooldownDuration = 30 * time.Minute // 默认30分钟
	}

	// 历史缓存大小：根据时间窗口计算（变化率检查需要）
	windowMinutes := cfg.Watchdog.Notifications.RateThreshold.WindowMinutes
	if windowMinutes <= 0 {
		windowMinutes = 5 // 默认5分钟
	}
	maxHistory := (windowMinutes*60)/int(sampleInterval.Seconds()) + 10 // 多保留一些

	return &Watchdog{
		cfg:                  cfg,
		storageService:       storageService,
		notifier:             notifier,
		sampleInterval:       sampleInterval,
		cleanupInterval:      cleanupInterval,
		ctx:                  ctx,
		cancel:               cancel,
		lastNotificationTime: make(map[string]time.Time),
		cooldownDuration:     cooldownDuration,
		historyCache:         make([]*SystemMetrics, 0, maxHistory),
		maxHistory:           maxHistory,
	}
}

// Start 启动看门狗
func (w *Watchdog) Start(ctx context.Context) error {
	if !w.cfg.Watchdog.Enabled {
		logger.Info("ℹ️ 看门狗监控未启用")
		return nil
	}

	logger.Info("✅ 看门狗监控已启动 (采样间隔: %v)", w.sampleInterval)

	// 启动采样协程
	go w.samplingLoop(ctx)

	// 启动清理协程
	go w.cleanupLoop(ctx)

	// 启动每日汇总协程
	if w.cfg.Watchdog.Aggregation.Enabled {
		go w.aggregationLoop(ctx)
	}

	return nil
}

// Stop 停止看门狗
func (w *Watchdog) Stop() {
	if w.cancel != nil {
		w.cancel()
	}
	logger.Info("✅ 看门狗监控已停止")
}

// samplingLoop 采样循环
func (w *Watchdog) samplingLoop(ctx context.Context) {
	ticker := time.NewTicker(w.sampleInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-w.ctx.Done():
			return
		case <-ticker.C:
			metrics, err := w.collectMetrics()
			if err != nil {
				logger.Error("❌ 采集系统指标失败: %v", err)
				continue
			}

			// 保存到数据库
			if err := w.saveMetrics(metrics); err != nil {
				logger.Error("❌ 保存系统指标失败: %v", err)
			}

			// 更新历史缓存
			w.updateHistoryCache(metrics)

			// 检查阈值并发送通知
			if w.cfg.Watchdog.Notifications.Enabled {
				if err := w.checkThresholds(metrics); err != nil {
					logger.Warn("⚠️ 检查阈值失败: %v", err)
				}
			}
		}
	}
}

// collectMetrics 采集系统指标
func (w *Watchdog) collectMetrics() (*SystemMetrics, error) {
	return CollectSystemMetrics()
}

// saveMetrics 保存指标到数据库
func (w *Watchdog) saveMetrics(metrics *SystemMetrics) error {
	if w.storageService == nil {
		return nil
	}

	// 使用存储服务的Save方法保存
	data := map[string]interface{}{
		"timestamp":      metrics.Timestamp,
		"cpu_percent":    metrics.CPUPercent,
		"memory_mb":      metrics.MemoryMB,
		"memory_percent": metrics.MemoryPercent,
		"process_id":     metrics.ProcessID,
	}

	w.storageService.Save("system_metrics", data)
	return nil
}

// updateHistoryCache 更新历史缓存
func (w *Watchdog) updateHistoryCache(metrics *SystemMetrics) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.historyCache = append(w.historyCache, metrics)

	// 保持缓存大小
	if len(w.historyCache) > w.maxHistory {
		w.historyCache = w.historyCache[len(w.historyCache)-w.maxHistory:]
	}
}

// checkThresholds 检查阈值并发送通知
func (w *Watchdog) checkThresholds(current *SystemMetrics) error {
	checker := NewThresholdChecker(w.cfg)

	// 检查固定阈值
	if w.cfg.Watchdog.Notifications.FixedThreshold.Enabled {
		if checker.CheckFixedThreshold(current) {
			if w.shouldNotify("fixed_cpu") {
				w.sendNotification("fixed_threshold", current, fmt.Sprintf(
					"CPU占用超过阈值: %.2f%% (阈值: %.2f%%)",
					current.CPUPercent, w.cfg.Watchdog.Notifications.FixedThreshold.CPUPercent,
				))
				w.updateNotificationTime("fixed_cpu")
			}
		}

		// 检查内存阈值（如果配置）
		if w.cfg.Watchdog.Notifications.FixedThreshold.MemoryMB > 0 {
			if current.MemoryMB >= float64(w.cfg.Watchdog.Notifications.FixedThreshold.MemoryMB) {
				if w.shouldNotify("fixed_memory") {
					w.sendNotification("fixed_threshold", current, fmt.Sprintf(
						"内存占用超过阈值: %.2f MB (阈值: %.2f MB)",
						current.MemoryMB, float64(w.cfg.Watchdog.Notifications.FixedThreshold.MemoryMB),
					))
					w.updateNotificationTime("fixed_memory")
				}
			}
		}
	}

	// 检查变化率阈值
	if w.cfg.Watchdog.Notifications.RateThreshold.Enabled {
		w.mu.RLock()
		history := make([]*SystemMetrics, len(w.historyCache))
		copy(history, w.historyCache)
		w.mu.RUnlock()

		if checker.CheckRateThreshold(
			current,
			history,
			w.cfg.Watchdog.Notifications.RateThreshold.WindowMinutes,
			w.cfg.Watchdog.Notifications.RateThreshold.CPUIncrease,
		) {
			if w.shouldNotify("rate_cpu") {
				oldest := findOldestInWindow(history, current.Timestamp, w.cfg.Watchdog.Notifications.RateThreshold.WindowMinutes)
				change := current.CPUPercent
				if oldest != nil {
					change = current.CPUPercent - oldest.CPUPercent
				}
				w.sendNotification("rate_threshold", current, fmt.Sprintf(
					"CPU占用在%d分钟内上涨%.2f%% (从%.2f%%到%.2f%%)",
					w.cfg.Watchdog.Notifications.RateThreshold.WindowMinutes,
					change, oldest.CPUPercent, current.CPUPercent,
				))
				w.updateNotificationTime("rate_cpu")
			}
		}

		// 检查内存变化率（如果配置）
		if w.cfg.Watchdog.Notifications.RateThreshold.MemoryIncreaseMB > 0 {
			if checker.CheckMemoryRateThreshold(
				current,
				history,
				w.cfg.Watchdog.Notifications.RateThreshold.WindowMinutes,
				float64(w.cfg.Watchdog.Notifications.RateThreshold.MemoryIncreaseMB),
			) {
				if w.shouldNotify("rate_memory") {
					oldest := findOldestInWindow(history, current.Timestamp, w.cfg.Watchdog.Notifications.RateThreshold.WindowMinutes)
					change := current.MemoryMB
					if oldest != nil {
						change = current.MemoryMB - oldest.MemoryMB
					}
					w.sendNotification("rate_threshold", current, fmt.Sprintf(
						"内存占用在%d分钟内上涨%.2f MB (从%.2f MB到%.2f MB)",
						w.cfg.Watchdog.Notifications.RateThreshold.WindowMinutes,
						change, oldest.MemoryMB, current.MemoryMB,
					))
					w.updateNotificationTime("rate_memory")
				}
			}
		}
	}

	return nil
}

// shouldNotify 检查是否应该发送通知（冷却机制）
func (w *Watchdog) shouldNotify(key string) bool {
	w.mu.RLock()
	defer w.mu.RUnlock()

	lastTime, exists := w.lastNotificationTime[key]
	if !exists {
		return true
	}

	return time.Since(lastTime) >= w.cooldownDuration
}

// updateNotificationTime 更新通知时间
func (w *Watchdog) updateNotificationTime(key string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.lastNotificationTime[key] = time.Now()
}

// sendNotification 发送通知
func (w *Watchdog) sendNotification(alertType string, metrics *SystemMetrics, message string) {
	if w.notifier == nil {
		return
	}

	// 使用事件系统发送通知
	// 注意：这里需要创建事件，但notify服务可能需要适配
	logger.Warn("🚨 [系统监控告警] %s: %s", alertType, message)
	logger.Info("📊 当前系统状态: CPU=%.2f%%, 内存=%.2f MB", metrics.CPUPercent, metrics.MemoryMB)

	// TODO: 集成到事件系统，通过事件总线发送通知
	// 目前先记录日志，后续可以通过事件系统发送
}

// cleanupLoop 清理循环
func (w *Watchdog) cleanupLoop(ctx context.Context) {
	ticker := time.NewTicker(w.cleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-w.ctx.Done():
			return
		case <-ticker.C:
			if err := w.cleanup(); err != nil {
				logger.Error("❌ 清理过期数据失败: %v", err)
			}
		}
	}
}

// cleanup 清理过期数据
func (w *Watchdog) cleanup() error {
	if w.storageService == nil {
		return nil
	}

	storage := w.storageService.GetStorage()
	if storage == nil {
		return nil
	}

	// 清理细粒度数据（超过保留天数）
	detailRetentionDays := w.cfg.Watchdog.Retention.DetailDays
	if detailRetentionDays > 0 {
		cutoffTime := time.Now().Add(-time.Duration(detailRetentionDays) * 24 * time.Hour)
		if err := storage.CleanupSystemMetrics(cutoffTime); err != nil {
			logger.Warn("⚠️ 清理细粒度数据失败: %v", err)
		} else {
			logger.Debug("🧹 清理细粒度数据（早于 %s）", cutoffTime.Format("2006-01-02 15:04:05"))
		}
	}

	// 清理汇总数据（超过保留天数）
	dailyRetentionDays := w.cfg.Watchdog.Retention.DailyDays
	if dailyRetentionDays > 0 {
		cutoffDate := time.Now().Add(-time.Duration(dailyRetentionDays) * 24 * time.Hour)
		cutoffDate = time.Date(cutoffDate.Year(), cutoffDate.Month(), cutoffDate.Day(), 0, 0, 0, 0, cutoffDate.Location())
		if err := storage.CleanupDailySystemMetrics(cutoffDate); err != nil {
			logger.Warn("⚠️ 清理汇总数据失败: %v", err)
		} else {
			logger.Debug("🧹 清理汇总数据（早于 %s）", cutoffDate.Format("2006-01-02"))
		}
	}

	return nil
}

// aggregationLoop 每日汇总循环
func (w *Watchdog) aggregationLoop(ctx context.Context) {
	// 计算下次汇总时间（默认凌晨）
	schedule := w.cfg.Watchdog.Aggregation.Schedule
	if schedule == "" {
		schedule = "00:00"
	}

	// 解析时间
	hour, min := parseSchedule(schedule)
	now := time.Now()
	nextRun := time.Date(now.Year(), now.Month(), now.Day(), hour, min, 0, 0, now.Location())
	if nextRun.Before(now) {
		nextRun = nextRun.Add(24 * time.Hour)
	}

	// 等待到汇总时间
	waitDuration := time.Until(nextRun)
	logger.Info("⏰ 下次每日汇总时间: %s (等待 %v)", nextRun.Format("2006-01-02 15:04:05"), waitDuration)

	timer := time.NewTimer(waitDuration)
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-w.ctx.Done():
			return
		case <-timer.C:
			// 执行汇总（汇总昨天的数据）
			yesterday := time.Now().Add(-24 * time.Hour)
			if err := w.aggregateDaily(yesterday); err != nil {
				logger.Error("❌ 每日汇总失败: %v", err)
			}

			// 设置下次汇总时间（24小时后）
			timer.Reset(24 * time.Hour)
		}
	}
}

// aggregateDaily 每日汇总
func (w *Watchdog) aggregateDaily(date time.Time) error {
	if w.storageService == nil {
		return nil
	}

	logger.Info("📊 开始每日汇总: %s", date.Format("2006-01-02"))

	// 计算日期范围（当天的开始和结束）
	startTime := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, date.Location())
	endTime := startTime.Add(24 * time.Hour)

	// 从数据库查询当天的所有细粒度数据
	metrics, err := w.queryMetricsByTimeRange(startTime, endTime)
	if err != nil {
		return fmt.Errorf("查询监控数据失败: %w", err)
	}

	if len(metrics) == 0 {
		logger.Warn("⚠️ 当日无监控数据，跳过汇总")
		return nil
	}

	// 计算统计值
	var sumCPU, sumMemory float64
	var maxCPU, maxMemory float64
	var minCPU, minMemory float64 = 100, 1e10

	for i, m := range metrics {
		sumCPU += m.CPUPercent
		sumMemory += m.MemoryMB

		if i == 0 {
			maxCPU = m.CPUPercent
			minCPU = m.CPUPercent
			maxMemory = m.MemoryMB
			minMemory = m.MemoryMB
		} else {
			if m.CPUPercent > maxCPU {
				maxCPU = m.CPUPercent
			}
			if m.CPUPercent < minCPU {
				minCPU = m.CPUPercent
			}
			if m.MemoryMB > maxMemory {
				maxMemory = m.MemoryMB
			}
			if m.MemoryMB < minMemory {
				minMemory = m.MemoryMB
			}
		}
	}

	count := float64(len(metrics))
	dailyMetrics := &storage.DailySystemMetrics{
		Date:          startTime,
		AvgCPUPercent: sumCPU / count,
		MaxCPUPercent: maxCPU,
		MinCPUPercent: minCPU,
		AvgMemoryMB:   sumMemory / count,
		MaxMemoryMB:   maxMemory,
		MinMemoryMB:   minMemory,
		SampleCount:   len(metrics),
		CreatedAt:     time.Now(),
	}

	// 保存到数据库（通过StorageService）
	if w.storageService != nil {
		storage := w.storageService.GetStorage()
		if storage != nil {
			if err := storage.SaveDailySystemMetrics(dailyMetrics); err != nil {
				return fmt.Errorf("保存每日汇总失败: %w", err)
			}
		}
	}

	logger.Info("✅ 每日汇总完成: CPU平均=%.2f%%, 内存平均=%.2f MB, 样本数=%d",
		dailyMetrics.AvgCPUPercent, dailyMetrics.AvgMemoryMB, dailyMetrics.SampleCount)

	return nil
}

// queryMetricsByTimeRange 查询时间范围内的监控数据
func (w *Watchdog) queryMetricsByTimeRange(startTime, endTime time.Time) ([]*SystemMetrics, error) {
	if w.storageService == nil {
		return []*SystemMetrics{}, nil
	}

	storage := w.storageService.GetStorage()
	if storage == nil {
		return []*SystemMetrics{}, nil
	}

	// 转换为storage包的SystemMetrics
	storageMetrics, err := storage.QuerySystemMetrics(startTime, endTime)
	if err != nil {
		return nil, err
	}

	// 转换为monitor包的SystemMetrics
	metrics := make([]*SystemMetrics, len(storageMetrics))
	for i, sm := range storageMetrics {
		metrics[i] = &SystemMetrics{
			Timestamp:     sm.Timestamp,
			CPUPercent:    sm.CPUPercent,
			MemoryMB:      sm.MemoryMB,
			MemoryPercent: sm.MemoryPercent,
			ProcessID:     sm.ProcessID,
		}
	}

	return metrics, nil
}

// findOldestInWindow 在时间窗口内找到最旧的数据点
func findOldestInWindow(history []*SystemMetrics, currentTime time.Time, windowMinutes int) *SystemMetrics {
	windowStart := currentTime.Add(-time.Duration(windowMinutes) * time.Minute)

	var oldest *SystemMetrics
	for _, m := range history {
		if m.Timestamp.After(windowStart) && m.Timestamp.Before(currentTime) {
			if oldest == nil || m.Timestamp.Before(oldest.Timestamp) {
				oldest = m
			}
		}
	}

	return oldest
}

// parseSchedule 解析时间调度（格式：HH:MM）
func parseSchedule(schedule string) (int, int) {
	var hour, min int
	fmt.Sscanf(schedule, "%d:%d", &hour, &min)
	if hour < 0 || hour > 23 {
		hour = 0
	}
	if min < 0 || min > 59 {
		min = 0
	}
	return hour, min
}

