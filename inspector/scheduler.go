package inspector

import (
	"context"
	"time"

	"quantmesh/logger"
)

// SchedulerConfig 調度配置
type SchedulerConfig struct {
	RegularInterval time.Duration // 常規間隔，預設 1h
	QuietHoursStart int           // 靜默時段開始（0-23），如 23
	QuietHoursEnd   int           // 靜默時段結束（0-23），如 7
	QuietInterval   time.Duration // 靜默時段內發送間隔，如 4h
}

// DefaultSchedulerConfig 預設調度配置
func DefaultSchedulerConfig() SchedulerConfig {
	return SchedulerConfig{
		RegularInterval: time.Hour,
		QuietHoursStart: 23,
		QuietHoursEnd:   7,
		QuietInterval:   4 * time.Hour,
	}
}

// Scheduler 智子巡檢調度器：決定何時觸發定時報告
type Scheduler struct {
	cfg   SchedulerConfig
	ctx   context.Context
	cancel context.CancelFunc
}

// NewScheduler 創建調度器
func NewScheduler(cfg SchedulerConfig) *Scheduler {
	if cfg.RegularInterval <= 0 {
		cfg.RegularInterval = time.Hour
	}
	if cfg.QuietInterval <= 0 {
		cfg.QuietInterval = 4 * time.Hour
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Scheduler{cfg: cfg, ctx: ctx, cancel: cancel}
}

// Run 在獨立 goroutine 中按間隔調用 onTick；首次延遲到下一整點（或下一靜默間隔）
func (s *Scheduler) Run(onTick func()) {
	if onTick == nil {
		return
	}
	go func() {
		loc := time.Local
		ticker := time.NewTicker(s.intervalAt(time.Now().In(loc)))
		defer ticker.Stop()
		// 首次立即執行一次可選；這裡改為延遲到第一個 tick
		for {
			select {
			case <-s.ctx.Done():
				return
			case t := <-ticker.C:
				nextInterval := s.intervalAt(t.In(loc))
				ticker.Reset(nextInterval)
				onTick()
			}
		}
	}()
	logger.Info("智子巡檢調度已啟動（常規間隔 %s，靜默時段 %d:00-%d:00 間隔 %s）",
		s.cfg.RegularInterval, s.cfg.QuietHoursStart, s.cfg.QuietHoursEnd, s.cfg.QuietInterval)
}

// intervalAt 根據當前時刻返回下次觸發間隔
func (s *Scheduler) intervalAt(now time.Time) time.Duration {
	hour := now.Hour()
	inQuiet := s.inQuietHours(hour)
	if inQuiet {
		return s.cfg.QuietInterval
	}
	return s.cfg.RegularInterval
}

func (s *Scheduler) inQuietHours(hour int) bool {
	start, end := s.cfg.QuietHoursStart, s.cfg.QuietHoursEnd
	if start > end {
		// 例如 23-7：靜默為 hour>=23 或 hour<7
		return hour >= start || hour < end
	}
	return hour >= start && hour < end
}

// Stop 停止調度
func (s *Scheduler) Stop() {
	if s.cancel != nil {
		s.cancel()
	}
}
