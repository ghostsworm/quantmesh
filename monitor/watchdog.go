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
	"quantmesh/utils"
)

// Watchdog 系统资源監控看门狗
type Watchdog struct {
	cfg             *config.Config
	storageService  *storage.StorageService
	logStorage      *storage.LogStorage // 日志存儲（用於清理日志）
	notifier        *notify.NotificationService
	sampleInterval  time.Duration
	cleanupInterval time.Duration
	ctx             context.Context
	cancel          context.CancelFunc
	mu              sync.RWMutex

	// 通知冷却机制
	lastNotificationTime map[string]time.Time
	cooldownDuration     time.Duration

	// 历史數據缓存（用於变化率检查）
	historyCache []*SystemMetrics
	maxHistory   int

	// 上次執行 VACUUM 的時间（SQLite 主庫優化，避免每小時執行）
	lastVacuumTime time.Time
}

// NewWatchdog 創建看门狗實例
func NewWatchdog(cfg *config.Config, storageService *storage.StorageService, logStorage *storage.LogStorage, notifier *notify.NotificationService) *Watchdog {
	ctx, cancel := context.WithCancel(context.Background())

	sampleInterval := time.Duration(cfg.Watchdog.Sampling.Interval) * time.Second
	if sampleInterval <= 0 {
		sampleInterval = 120 * time.Second // 默认2分钟
	}

	cleanupInterval := 1 * time.Hour // 每小時清理一次
	cooldownDuration := time.Duration(cfg.Watchdog.Notifications.CooldownMinutes) * time.Minute
	if cooldownDuration <= 0 {
		cooldownDuration = 30 * time.Minute // 默认30分钟
	}

	// 历史缓存大小：根據時间窗口计算（变化率检查需要）
	windowMinutes := cfg.Watchdog.Notifications.RateThreshold.WindowMinutes
	if windowMinutes <= 0 {
		windowMinutes = 5 // 默认5分钟
	}
	maxHistory := (windowMinutes*60)/int(sampleInterval.Seconds()) + 10 // 多保留一些

	return &Watchdog{
		cfg:                  cfg,
		storageService:       storageService,
		logStorage:           logStorage,
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

// Start 啟动看门狗
func (w *Watchdog) Start(ctx context.Context) error {
	if !w.cfg.Watchdog.Enabled {
		logger.Info("ℹ️ 看门狗監控未啟用")
		return nil
	}

	logger.Info("✅ 看门狗監控已啟动 (采样间隔: %v)", w.sampleInterval)

	// 啟动采样协程
	go w.samplingLoop(ctx)

	// 啟动清理协程
	go w.cleanupLoop(ctx)

	// 啟动每日彙總协程
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
	logger.Info("✅ 看门狗監控已停止")
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
				logger.Error("❌ 采集系统指標失败: %v", err)
				continue
			}

			// 保存到數據库
			if err := w.saveMetrics(metrics); err != nil {
				logger.Error("❌ 保存系统指標失败: %v", err)
			}

			// 更新历史缓存
			w.updateHistoryCache(metrics)

			// 检查阈值並发送通知
			if w.cfg.Watchdog.Notifications.Enabled {
				if err := w.checkThresholds(metrics); err != nil {
					logger.Warn("⚠️ 检查阈值失败: %v", err)
				}
			}
		}
	}
}

// collectMetrics 采集系统指標
func (w *Watchdog) collectMetrics() (*SystemMetrics, error) {
	return CollectSystemMetrics()
}

// saveMetrics 保存指標到數據库
func (w *Watchdog) saveMetrics(metrics *SystemMetrics) error {
	if w.storageService == nil {
		return nil
	}

	// 使用存儲服務的Save方法保存
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

	// 保持缓存大小（使用 copy 避免底层數组容量泄漏）
	if len(w.historyCache) > w.maxHistory {
		// 计算需要保留的起始位置
		start := len(w.historyCache) - w.maxHistory
		// 創建新的切片並複制數據，释放舊數组的記憶體
		newCache := make([]*SystemMetrics, w.maxHistory)
		copy(newCache, w.historyCache[start:])
		w.historyCache = newCache
	}
}

// GetLatestMetrics 獲取最新的系统監控數據（從缓存中）
func (w *Watchdog) GetLatestMetrics() *SystemMetrics {
	w.mu.RLock()
	defer w.mu.RUnlock()

	if len(w.historyCache) == 0 {
		return nil
	}

	// 返回最新的數據（最后一個）
	return w.historyCache[len(w.historyCache)-1]
}

// checkThresholds 检查阈值並发送通知
func (w *Watchdog) checkThresholds(current *SystemMetrics) error {
	checker := NewThresholdChecker(w.cfg)

	// 检查固定阈值
	if w.cfg.Watchdog.Notifications.FixedThreshold.Enabled {
		if checker.CheckFixedThreshold(current) {
			if w.shouldNotify("fixed_cpu") {
				w.sendNotification("fixed_threshold", current, fmt.Sprintf(
					"CPU占用超過阈值: %.2f%% (阈值: %.2f%%)",
					current.CPUPercent, w.cfg.Watchdog.Notifications.FixedThreshold.CPUPercent,
				))
				w.updateNotificationTime("fixed_cpu")
			}
		}

		// 检查記憶體阈值（如果配置）
		if w.cfg.Watchdog.Notifications.FixedThreshold.MemoryMB > 0 {
			if current.MemoryMB >= float64(w.cfg.Watchdog.Notifications.FixedThreshold.MemoryMB) {
				if w.shouldNotify("fixed_memory") {
					w.sendNotification("fixed_threshold", current, fmt.Sprintf(
						"記憶體占用超過阈值: %.2f MB (阈值: %.2f MB)",
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
					"CPU占用在%d分钟内上涨%.2f%% (從%.2f%%到%.2f%%)",
					w.cfg.Watchdog.Notifications.RateThreshold.WindowMinutes,
					change, oldest.CPUPercent, current.CPUPercent,
				))
				w.updateNotificationTime("rate_cpu")
			}
		}

		// 检查記憶體变化率（如果配置）
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
						"記憶體占用在%d分钟内上涨%.2f MB (從%.2f MB到%.2f MB)",
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

// shouldNotify 检查是否应該发送通知（冷却机制）
func (w *Watchdog) shouldNotify(key string) bool {
	w.mu.RLock()
	defer w.mu.RUnlock()

	lastTime, exists := w.lastNotificationTime[key]
	if !exists {
		return true
	}

	return time.Since(lastTime) >= w.cooldownDuration
}

// updateNotificationTime 更新通知時间
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
	// 注意：这里需要創建事件，但notify服務可能需要适配
	logger.Warn("🚨 [系统監控告警] %s: %s", alertType, message)
	logger.Info("📊 當前系统状態: CPU=%.2f%%, 記憶體=%.2f MB", metrics.CPUPercent, metrics.MemoryMB)

	// TODO: 集成到事件系统，通過事件總線发送通知
	// 目前先記錄日志，后续可以通過事件系统发送
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
				logger.Error("❌ 清理過期數據失败: %v", err)
			}
		}
	}
}

// cleanup 清理過期數據
func (w *Watchdog) cleanup() error {
	if w.storageService == nil {
		return nil
	}

	storage := w.storageService.GetStorage()
	if storage == nil {
		return nil
	}

	// 清理细粒度數據（超過保留天數）
	detailRetentionDays := w.cfg.Watchdog.Retention.DetailDays
	if detailRetentionDays > 0 {
		cutoffTime := time.Now().Add(-time.Duration(detailRetentionDays) * 24 * time.Hour)
		if err := storage.CleanupSystemMetrics(cutoffTime); err != nil {
			logger.Warn("⚠️ 清理细粒度數據失败: %v", err)
		} else {
			logger.Debug("🧹 清理细粒度數據（早於 %s）", cutoffTime.Format("2006-01-02 15:04:05"))
		}
	}

	// 清理彙總數據（超過保留天數）
	dailyRetentionDays := w.cfg.Watchdog.Retention.DailyDays
	if dailyRetentionDays > 0 {
		cutoffDate := time.Now().Add(-time.Duration(dailyRetentionDays) * 24 * time.Hour)
		cutoffDate = time.Date(cutoffDate.Year(), cutoffDate.Month(), cutoffDate.Day(), 0, 0, 0, 0, cutoffDate.Location())
		if err := storage.CleanupDailySystemMetrics(cutoffDate); err != nil {
			logger.Warn("⚠️ 清理彙總數據失败: %v", err)
		} else {
			logger.Debug("🧹 清理彙總數據（早於 %s）", cutoffDate.Format("2006-01-02"))
		}
	}

	// 清理日志（超過保留天數）
	if w.logStorage != nil {
		logRetentionDays := w.cfg.System.LogRetentionDays
		if logRetentionDays > 0 {
			if err := w.logStorage.CleanOldLogs(logRetentionDays); err != nil {
				logger.Warn("⚠️ 清理過期日志失败: %v", err)
			} else {
				logger.Debug("🧹 清理過期日志（保留最近 %d 天）", logRetentionDays)
			}
		}
	}

	// SQLite 主庫定期 VACUUM（每 24 小時一次，回收 DELETE 后的空間）
	vacuumInterval := 24 * time.Hour
	if time.Since(w.lastVacuumTime) >= vacuumInterval {
		if err := storage.Vacuum(); err != nil {
			logger.Warn("⚠️ SQLite VACUUM 失败: %v", err)
		} else {
			w.lastVacuumTime = time.Now()
			logger.Info("🧹 SQLite 主庫 VACUUM 完成")
		}
	}

	return nil
}

// aggregationLoop 每日彙總循环
func (w *Watchdog) aggregationLoop(ctx context.Context) {
	// 计算下次彙總時间（默认凌晨）
	schedule := w.cfg.Watchdog.Aggregation.Schedule
	if schedule == "" {
		schedule = "00:00"
	}

	// 解析時间（按配置時區）
	hour, min := parseSchedule(schedule)
	now := utils.NowConfiguredTimezone()
	nextRun := time.Date(now.Year(), now.Month(), now.Day(), hour, min, 0, 0, utils.GlobalLocation)
	if nextRun.Before(now) {
		nextRun = nextRun.Add(24 * time.Hour)
	}

	// 等待到彙總時间
	waitDuration := time.Until(nextRun)
	logger.Info("⏰ 下次每日彙總時间: %s (等待 %v)", nextRun.Format("2006-01-02 15:04:05"), waitDuration)

	timer := time.NewTimer(waitDuration)
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-w.ctx.Done():
			return
		case <-timer.C:
			// 執行彙總（彙總昨天的數據，按配置時區）
			yesterday := utils.NowConfiguredTimezone().Add(-24 * time.Hour)
			if err := w.aggregateDaily(yesterday); err != nil {
				logger.Error("❌ 每日彙總失败: %v", err)
			}

			// 設置下次彙總時间（24小時后）
			timer.Reset(24 * time.Hour)
		}
	}
}

// aggregateDaily 每日彙總
func (w *Watchdog) aggregateDaily(date time.Time) error {
	if w.storageService == nil {
		return nil
	}

	logger.Info("📊 开始每日彙總: %s", date.Format("2006-01-02"))

	// 计算日期範圍（當天的开始和結束）
	startTime := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, date.Location())
	endTime := startTime.Add(24 * time.Hour)

	// 從數據库查詢當天的所有细粒度數據
	metrics, err := w.queryMetricsByTimeRange(startTime, endTime)
	if err != nil {
		return fmt.Errorf("查詢監控數據失败: %w", err)
	}

	if len(metrics) == 0 {
		logger.Warn("⚠️ 當日無監控數據，跳過彙總")
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

	// 保存到數據库（通過StorageService）
	if w.storageService != nil {
		storage := w.storageService.GetStorage()
		if storage != nil {
			if err := storage.SaveDailySystemMetrics(dailyMetrics); err != nil {
				return fmt.Errorf("保存每日彙總失败: %w", err)
			}
		}
	}

	logger.Info("✅ 每日彙總完成: CPU平均=%.2f%%, 記憶體平均=%.2f MB, 样本數=%d",
		dailyMetrics.AvgCPUPercent, dailyMetrics.AvgMemoryMB, dailyMetrics.SampleCount)

	return nil
}

// queryMetricsByTimeRange 查詢時间範圍内的監控數據
func (w *Watchdog) queryMetricsByTimeRange(startTime, endTime time.Time) ([]*SystemMetrics, error) {
	if w.storageService == nil {
		return []*SystemMetrics{}, nil
	}

	storage := w.storageService.GetStorage()
	if storage == nil {
		return []*SystemMetrics{}, nil
	}

	// 轉换為storage包的SystemMetrics
	storageMetrics, err := storage.QuerySystemMetrics(startTime, endTime)
	if err != nil {
		return nil, err
	}

	// 轉换為monitor包的SystemMetrics
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

// findOldestInWindow 在時间窗口内找到最舊的數據点
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

// parseSchedule 解析時间調度（格式：HH:MM）
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
